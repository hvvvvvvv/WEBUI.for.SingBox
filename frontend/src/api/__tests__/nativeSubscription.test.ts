import { Code, ConnectError } from '@connectrpc/connect'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { NativeSubscription } from '../nativeSubscription'

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
    const first = stream.first()
    expect(open).toHaveBeenCalledTimes(1)
    pending.resolve(7)
    await expect(first).resolves.toBe(7)
    expect(handler).toHaveBeenCalledWith(7)
    unregister()
    expect(signals[0]?.aborted).toBe(true)
  })

  it('counts duplicate registrations independently', () => {
    let signal!: AbortSignal
    const stream = new NativeSubscription<number>(async function* (value) {
      signal = value
      await new Promise(() => {})
      yield 1
    })
    stream.setInstance(new AbortController().signal)
    const handler = vi.fn()
    const first = stream.subscribe(handler)
    const second = stream.subscribe(handler)
    first()
    expect(signal.aborted).toBe(false)
    second()
    expect(signal.aborted).toBe(true)
  })

  it('discards delayed values from an old process even if its iterator ignores cancellation', async () => {
    const old = deferred<number>()
    const fresh = deferred<number>()
    const open = vi
      .fn()
      .mockReturnValueOnce(
        (async function* () {
          yield await old.promise
        })(),
      )
      .mockReturnValueOnce(
        (async function* () {
          yield await fresh.promise
        })(),
      )
    const stream = new NativeSubscription<number>(open)
    const handler = vi.fn()
    stream.subscribe(handler)
    stream.setInstance(new AbortController().signal)
    stream.setInstance(new AbortController().signal)
    old.resolve(1)
    fresh.resolve(2)
    await flush()
    expect(handler.mock.calls).toEqual([[2]])
    stream.setInstance()
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
    for (const [delay, calls] of [
      [1000, 2],
      [2000, 3],
      [5000, 4],
      [10000, 5],
    ]) {
      await vi.advanceTimersByTimeAsync(delay!)
      expect(open).toHaveBeenCalledTimes(calls!)
    }
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
