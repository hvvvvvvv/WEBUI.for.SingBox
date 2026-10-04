import { Code, ConnectError } from '@connectrpc/connect'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { NativeSubscription } from '@/api/nativeSubscription'

const flush = async () => {
  for (let i = 0; i < 10; i++) await Promise.resolve()
}
const deferred = <T>() => {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((r) => {
    resolve = r
  })
  return { promise, resolve }
}

afterEach(() => vi.useRealTimers())

describe('shared native subscriptions', () => {
  it('shares one stream, waits only for the first value, and cancels the last consumer', async () => {
    const pending = deferred<number>()
    const signals: AbortSignal[] = []
    const open = vi.fn(async function* (signal: AbortSignal) {
      signals.push(signal)
      yield await pending.promise
      await new Promise(() => {})
    })
    const stream = new NativeSubscription(open)
    stream.setInstance(new AbortController().signal)
    const handler = vi.fn()
    const unregister = stream.subscribe(handler)
    const unregisterDuplicate = stream.subscribe(handler)
    const first = stream.first()
    expect(open).toHaveBeenCalledTimes(1)
    pending.resolve(7)
    await expect(first).resolves.toBe(7)
    expect(handler).toHaveBeenCalledWith(7)
    unregister()
    expect(signals[0]?.aborted).toBe(false)
    unregisterDuplicate()
    expect(signals[0]?.aborted).toBe(true)
  })

  it('backs off recoverable failures and stops retrying capability errors', async () => {
    vi.useFakeTimers()
    const open = vi.fn(async function* () {
      throw new ConnectError('offline', Code.Unavailable)
      yield 1
    })
    const stream = new NativeSubscription<number>(open, vi.fn())
    stream.subscribe(vi.fn())
    stream.setInstance(new AbortController().signal)
    await flush()
    expect(open).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(1000)
    expect(open).toHaveBeenCalledTimes(2)
    stream.setInstance()
    const unsupported = new NativeSubscription<number>(async function* () {
      throw new ConnectError('unsupported', Code.Unimplemented)
      yield 1
    }, vi.fn())
    unsupported.setInstance(new AbortController().signal)
    await expect(unsupported.first()).rejects.toMatchObject({ code: Code.Unimplemented })
    await vi.advanceTimersByTimeAsync(30000)
    await expect(unsupported.first()).rejects.toMatchObject({ code: Code.Unimplemented })
    unsupported.setInstance()
  })

  it('cancels initial waiters when the instance stops', async () => {
    const stream = new NativeSubscription<number>(async function* () {
      yield await new Promise<number>(() => {})
    })
    stream.setInstance(new AbortController().signal)
    const pending = stream.first()
    stream.setInstance()
    await expect(pending).rejects.toMatchObject({ code: Code.Canceled })
  })
})
