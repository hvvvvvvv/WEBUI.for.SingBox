import { Code, ConnectError } from '@connectrpc/connect'

const reconnectDelays = [1000, 2000, 5000, 10000]

/** One server stream shared by its consumers. Replacing the instance revokes old results. */
export class NativeSubscription<T> {
  private handlers = new Map<number, (value: T) => void>()
  private nextHandler = 0
  private instance?: AbortSignal
  private controller?: AbortController
  private reconnectTimer?: ReturnType<typeof setTimeout>
  private generation = 0
  private failures = 0
  private latest?: T
  private terminalError?: unknown
  private pending = new Set<(error: unknown) => void>()

  constructor(
    private readonly open: (signal: AbortSignal) => AsyncIterable<T>,
    private readonly onError: (error: unknown) => void = (error) =>
      console.error('Native API:', error),
  ) {}

  setInstance(signal?: AbortSignal) {
    this.rejectPending(new ConnectError('Kernel instance changed', Code.Canceled))
    this.stop()
    this.instance = signal
    this.latest = undefined
    this.terminalError = undefined
    this.failures = 0
    this.start()
  }

  subscribe(handler: (value: T) => void, replay = true) {
    const id = ++this.nextHandler
    this.handlers.set(id, handler)
    if (replay && this.latest !== undefined) handler(this.latest)
    this.start()
    let removed = false
    return () => {
      if (removed) return
      removed = true
      this.handlers.delete(id)
      if (this.handlers.size === 0) this.stop()
    }
  }

  publish(value: T) {
    this.latest = value
    for (const handler of this.handlers.values()) handler(value)
  }

  first(): Promise<T> {
    return this.waitFor(() => true)
  }

  waitFor(predicate: (value: T) => boolean): Promise<T> {
    if (this.terminalError) return Promise.reject(this.terminalError)
    return new Promise((resolve, reject) => {
      let unregister = () => {}
      const fail = (error: unknown) => {
        clearTimeout(timer)
        unregister()
        this.pending.delete(fail)
        reject(error)
      }
      const timer = setTimeout(() => {
        fail(
          new ConnectError('Native API did not provide an initial snapshot', Code.DeadlineExceeded),
        )
      }, 10000)
      this.pending.add(fail)
      const receive = (value: T) => {
        if (!predicate(value)) return
        clearTimeout(timer)
        unregister()
        this.pending.delete(fail)
        resolve(value)
      }
      unregister = this.subscribe(receive, false)
      if (this.latest !== undefined) receive(this.latest)
    })
  }

  private rejectPending(error: unknown) {
    for (const fail of this.pending) fail(error)
  }

  private stop() {
    this.generation++
    this.controller?.abort()
    this.controller = undefined
    this.latest = undefined
    clearTimeout(this.reconnectTimer)
    this.reconnectTimer = undefined
  }

  private start() {
    if (
      !this.instance ||
      this.instance.aborted ||
      this.controller ||
      this.reconnectTimer ||
      this.handlers.size === 0 ||
      this.terminalError
    )
      return
    const generation = ++this.generation
    const controller = new AbortController()
    const instance = this.instance
    this.controller = controller
    const abort = () => {
      this.rejectPending(new ConnectError('Kernel instance changed', Code.Canceled))
      controller.abort()
    }
    instance.addEventListener('abort', abort, { once: true })
    void (async () => {
      try {
        for await (const value of this.open(controller.signal)) {
          if (generation !== this.generation || instance.aborted || controller.signal.aborted)
            return
          this.failures = 0
          this.latest = value
          for (const handler of this.handlers.values()) {
            try {
              handler(value)
            } catch (error) {
              this.onError(error)
            }
          }
        }
      } catch (error) {
        if (generation !== this.generation || controller.signal.aborted || instance.aborted) return
        const code = ConnectError.from(error).code
        if (
          [
            Code.Unauthenticated,
            Code.PermissionDenied,
            Code.Unimplemented,
            Code.InvalidArgument,
            Code.FailedPrecondition,
            Code.NotFound,
          ].includes(code)
        ) {
          this.terminalError = error
          this.rejectPending(error)
          this.onError(error)
          return
        }
        this.onError(error)
      } finally {
        instance.removeEventListener('abort', abort)
        if (generation === this.generation) this.controller = undefined
        if (
          generation === this.generation &&
          !controller.signal.aborted &&
          !instance.aborted &&
          this.handlers.size > 0 &&
          !this.terminalError
        ) {
          const delay = reconnectDelays[Math.min(this.failures++, reconnectDelays.length - 1)]!
          this.reconnectTimer = setTimeout(() => {
            this.reconnectTimer = undefined
            this.start()
          }, delay)
        }
      }
    })()
  }
}
