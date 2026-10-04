import { create } from '@bufbuild/protobuf'
import { effectScope, ref, shallowRef, type EffectScope } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import {
  GroupItemSchema,
  GroupSchema,
  OutboundListSchema,
  type OutboundList,
} from '@gen/native/daemon/started_service_pb'
import type { Group, GroupItem } from '@/types/kernel'

const mocks = vi.hoisted(() => ({
  urlTest: vi.fn(),
  onOutbounds: vi.fn(),
  getNativeApiGeneration: vi.fn(),
}))
vi.mock('@/api/kernel', () => mocks)

import { useNativeUrlTest } from '@/hooks/useNativeUrlTest'

const callbacks = new Set<(value: OutboundList) => void>()
const scopes: EffectScope[] = []
const unregister = vi.fn()

const item = (tag: string, time = 0n, delay = 0, type = 'shadowsocks') =>
  create(GroupItemSchema, { tag, type, urlTestTime: time, urlTestDelay: delay })

const group = (tag: string, items: GroupItem[], selected = items[0]?.tag || '') =>
  create(GroupSchema, { tag, items, selected, type: 'selector', selectable: true })

const deferred = <T>() => {
  let resolve!: (value: T) => void
  let reject!: (reason: unknown) => void
  const promise = new Promise<T>((accept, fail) => {
    resolve = accept
    reject = fail
  })
  return { promise, resolve, reject }
}

const flush = async () => {
  for (let i = 0; i < 5; i++) await Promise.resolve()
}

const setup = (initial: { groups?: Group[]; outbounds?: GroupItem[] } = {}) => {
  const scope = effectScope()
  scopes.push(scope)
  const groups = shallowRef(initial.groups || [])
  const outbounds = shallowRef(initial.outbounds || [item('node', 10n, 100)])
  const pid = ref(1)
  const running = ref(true)
  const onTimeout = vi.fn()
  const test = scope.run(() =>
    useNativeUrlTest({
      groups: () => groups.value,
      outbounds: () => outbounds.value,
      pid: () => pid.value,
      running: () => running.value,
      onTimeout,
    }),
  )!
  const emit = (...values: GroupItem[]) => {
    outbounds.value = values
    const snapshot = create(OutboundListSchema, { outbounds: values })
    for (const callback of callbacks) callback(snapshot)
  }
  return { ...test, scope, groups, outbounds, pid, running, onTimeout, emit }
}

beforeEach(() => {
  vi.useFakeTimers()
  vi.resetAllMocks()
  callbacks.clear()
  mocks.urlTest.mockResolvedValue({})
  mocks.getNativeApiGeneration.mockReturnValue(1)
  mocks.onOutbounds.mockImplementation((callback: (value: OutboundList) => void) => {
    callbacks.add(callback)
    return () => {
      callbacks.delete(callback)
      unregister()
    }
  })
})

afterEach(() => {
  for (const scope of scopes.splice(0)) scope.stop()
  vi.useRealTimers()
})

describe('native latency test feedback', () => {
  it('keeps loading after the RPC accepts, and ignores unchanged cached history', async () => {
    const test = setup()
    await expect(test.start('node')).resolves.toBe(true)
    expect(mocks.urlTest).toHaveBeenCalledWith('node')
    expect(test.isLoading('node')).toBe(true)
    test.emit(item('node', 10n, 100))
    expect(test.isLoading('node')).toBe(true)
    await vi.advanceTimersByTimeAsync(9_999)
    expect(test.onTimeout).not.toHaveBeenCalled()
    await vi.advanceTimersByTimeAsync(1)
    expect(test.isLoading('node')).toBe(false)
    expect(test.onTimeout).toHaveBeenCalledExactlyOnceWith({
      tag: 'node',
      isGroup: false,
      remainingCount: 1,
    })
  })

  it('uses the outbound snapshot baseline rather than older group member history', async () => {
    const test = setup({
      groups: [group('group', [item('node', 9n, 90)])],
      outbounds: [item('node', 10n, 100)],
    })
    await test.start('group')
    test.emit(item('node', 10n, 100))
    expect(test.isLoading('group')).toBe(true)
    test.emit(item('node', 11n, 100))
    expect(test.isLoading('group')).toBe(false)
  })

  it('holds an early result until the RPC is accepted, then completes without timing out', async () => {
    const request = deferred<object>()
    mocks.urlTest.mockReturnValue(request.promise)
    const test = setup()
    const started = test.start('node')
    test.emit(item('node', 11n, 90))
    expect(test.isLoading('node')).toBe(true)
    request.resolve({})
    await expect(started).resolves.toBe(true)
    expect(test.isLoading('node')).toBe(false)
    await vi.advanceTimersByTimeAsync(10_000)
    expect(test.onTimeout).not.toHaveBeenCalled()
  })

  it('tests all nested members, deduplicates leaves, and stops group loading only when all finish', async () => {
    const first = item('first', 10n, 100)
    const second = item('second', 10n, 200)
    const third = item('third', 10n, 300)
    const outer = item('outer', 10n, 100, 'selector')
    const nested = item('nested', 10n, 200, 'selector')
    const test = setup({
      groups: [group('outer', [first, nested]), group('nested', [second, third, outer], 'second')],
      outbounds: [first, second, third, outer, nested],
    })
    await expect(test.start('outer')).resolves.toBe(true)
    for (const tag of ['outer', 'nested', 'first', 'second', 'third']) {
      expect(test.isLoading(tag), tag).toBe(true)
    }
    test.emit(item('first', 11n, 90), second, third, item('outer', 11n, 90, 'selector'), nested)
    expect(test.isLoading('first')).toBe(false)
    expect(test.isLoading('outer')).toBe(true)
    expect(test.isLoading('nested')).toBe(true)
    await expect(test.start('first')).resolves.toBe(false)
    expect(mocks.urlTest).toHaveBeenCalledTimes(1)
    test.emit(item('first', 11n, 90), item('second', 11n, 190), third, outer, nested)
    expect(test.isLoading('second')).toBe(false)
    expect(test.isLoading('nested')).toBe(true)
    test.emit(item('first', 11n, 90), item('second', 11n, 190), item('third', 11n, 290))
    for (const tag of ['outer', 'nested', 'first', 'second', 'third']) {
      expect(test.isLoading(tag), tag).toBe(false)
    }
    await expect(test.start('first')).resolves.toBe(true)
  })

  it('does not use a selector alias timestamp as completion evidence for its leaves', async () => {
    const first = item('first', 10n, 100)
    const second = item('second', 10n, 200)
    const nested = item('nested', 10n, 100, 'selector')
    const test = setup({
      groups: [group('outer', [nested]), group('nested', [first, second], 'first')],
      outbounds: [first, second, nested],
    })
    await test.start('outer')
    test.emit(first, second, item('nested', 11n, 80, 'selector'))
    expect(test.isLoading('outer')).toBe(true)
    expect(test.isLoading('nested')).toBe(true)
    expect(test.isLoading('first')).toBe(true)
    expect(test.isLoading('second')).toBe(true)
    await vi.advanceTimersByTimeAsync(10_000)
    expect(test.onTimeout).toHaveBeenCalledExactlyOnceWith({
      tag: 'outer',
      isGroup: true,
      remainingCount: 2,
    })
  })

  it('keeps the original batch membership when the displayed groups change', async () => {
    const first = item('first', 10n, 100)
    const second = item('second', 10n, 200)
    const test = setup({ groups: [group('group', [first, second])], outbounds: [first, second] })
    await test.start('group')
    test.groups.value = [group('group', [first])]
    test.emit(item('first', 11n, 90), second)
    expect(test.isLoading('group')).toBe(true)
    expect(test.isLoading('second')).toBe(true)
    await vi.advanceTimersByTimeAsync(10_000)
    expect(test.onTimeout).toHaveBeenCalledExactlyOnceWith({
      tag: 'group',
      isGroup: true,
      remainingCount: 1,
    })
  })

  it('cancels immediately when the process changes and ignores a late submission error', async () => {
    const request = deferred<object>()
    mocks.urlTest.mockReturnValueOnce(request.promise)
    const test = setup()
    const started = test.start('node')
    test.pid.value = 2
    expect(test.isLoading('node')).toBe(false)
    request.reject(new Error('old process request failed'))
    await expect(started).resolves.toBe(false)
    await vi.advanceTimersByTimeAsync(10_000)
    expect(test.onTimeout).not.toHaveBeenCalled()
  })

  it('releases timers and subscriptions when its component scope is disposed', async () => {
    const request = deferred<object>()
    mocks.urlTest.mockReturnValueOnce(request.promise)
    const test = setup()
    const started = test.start('node')
    const staleCallback = [...callbacks][0]!
    test.scope.stop()
    expect(test.isLoading('node')).toBe(false)
    expect(unregister).toHaveBeenCalledTimes(1)
    staleCallback(create(OutboundListSchema, { outbounds: [item('node', 11n, 90)] }))
    request.resolve({})
    await expect(started).resolves.toBe(false)
    await vi.advanceTimersByTimeAsync(10_000)
    expect(test.onTimeout).not.toHaveBeenCalled()
  })

  it('does not let an old deadline or RPC acceptance clear a retry', async () => {
    const oldRequest = deferred<object>()
    mocks.urlTest.mockReturnValueOnce(oldRequest.promise)
    const test = setup()
    const oldStart = test.start('node')
    await vi.advanceTimersByTimeAsync(10_000)
    expect(test.onTimeout).toHaveBeenCalledTimes(1)
    await expect(test.start('node')).resolves.toBe(true)
    expect(test.isLoading('node')).toBe(true)
    oldRequest.resolve({})
    await expect(oldStart).resolves.toBe(false)
    expect(test.isLoading('node')).toBe(true)
    await flush()
    await vi.advanceTimersByTimeAsync(9_999)
    expect(test.onTimeout).toHaveBeenCalledTimes(1)
    expect(test.isLoading('node')).toBe(true)
    test.emit(item('node', 11n, 90))
    expect(test.isLoading('node')).toBe(false)
    await vi.advanceTimersByTimeAsync(1)
    expect(test.onTimeout).toHaveBeenCalledTimes(1)
  })
})
