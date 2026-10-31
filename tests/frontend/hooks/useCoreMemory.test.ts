// @vitest-environment jsdom
import {
  createApp,
  defineComponent,
  h,
  KeepAlive,
  nextTick,
  reactive,
  ref,
  type App,
} from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { useCoreMemory } from '@/hooks/useCoreMemory'

describe('overview RSS sampling lifecycle', () => {
  let app: App
  let host: HTMLDivElement
  let memory: ReturnType<typeof useCoreMemory>
  const state = reactive({ enabled: false, running: true, pid: 42 })
  const visible = ref(true)
  const load = vi.fn<(signal: AbortSignal) => Promise<bigint>>()
  beforeEach(() => {
    vi.useFakeTimers()
    load.mockReset().mockResolvedValue(1024n)
    Object.assign(state, { enabled: false, running: true, pid: 42 })
    visible.value = true
    host = document.createElement('div')
    document.body.append(host)
    const overview = defineComponent({
      setup() {
        memory = useCoreMemory({
          enabled: () => state.enabled,
          running: () => state.running,
          pid: () => state.pid,
          load,
        })
        return () => h('span', String(memory.rss.value ?? 'pending'))
      },
    })
    const other = defineComponent({ render: () => h('div', 'other page') })
    app = createApp({
      render: () => h(KeepAlive, null, () => (visible.value ? h(overview) : h(other))),
    })
    app.mount(host)
  })
  afterEach(() => {
    app.unmount()
    host.remove()
    vi.useRealTimers()
  })

  it('does not request RSS by default and samples at most once per 30 seconds when enabled', async () => {
    await vi.advanceTimersByTimeAsync(60_000)
    expect(load).not.toHaveBeenCalled()
    state.enabled = true
    await vi.advanceTimersByTimeAsync(0)
    expect(load).toHaveBeenCalledTimes(1)
    expect(memory.rss.value).toBe(1024n)
    await vi.advanceTimersByTimeAsync(29_999)
    expect(load).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(1)
    expect(load).toHaveBeenCalledTimes(2)
    state.enabled = false
    expect(memory.rss.value).toBeUndefined()
    await vi.advanceTimersByTimeAsync(60_000)
    expect(load).toHaveBeenCalledTimes(2)
    expect(vi.getTimerCount()).toBe(0)
  })

  it('waits for a running core and discards a late result from an old PID', async () => {
    state.running = false
    state.enabled = true
    await vi.advanceTimersByTimeAsync(60_000)
    expect(load).not.toHaveBeenCalled()
    const old = Promise.withResolvers<bigint>()
    const current = Promise.withResolvers<bigint>()
    load.mockReturnValueOnce(old.promise).mockReturnValueOnce(current.promise)
    state.running = true
    expect(load).toHaveBeenCalledTimes(1)
    const oldSignal = load.mock.calls[0]![0]
    state.pid = 43
    expect(oldSignal.aborted).toBe(true)
    current.resolve(2048n)
    await vi.advanceTimersByTimeAsync(0)
    expect(memory.rss.value).toBe(2048n)
    old.resolve(9999n)
    await vi.advanceTimersByTimeAsync(0)
    expect(memory.rss.value).toBe(2048n)
    state.running = false
    await vi.advanceTimersByTimeAsync(60_000)
    expect(memory.rss.value).toBeUndefined()
    expect(load).toHaveBeenCalledTimes(2)
  })

  it('aborts pending work and stops polling when the kept-alive overview is deactivated', async () => {
    const pending = Promise.withResolvers<bigint>()
    load.mockReturnValueOnce(pending.promise)
    state.enabled = true
    const signal = load.mock.calls[0]![0]
    visible.value = false
    await nextTick()
    expect(signal.aborted).toBe(true)
    pending.resolve(777n)
    await vi.advanceTimersByTimeAsync(60_000)
    expect(memory.rss.value).toBeUndefined()
    expect(load).toHaveBeenCalledTimes(1)
    expect(vi.getTimerCount()).toBe(0)
    visible.value = true
    await nextTick()
    await vi.advanceTimersByTimeAsync(0)
    expect(load).toHaveBeenCalledTimes(2)
    expect(memory.rss.value).toBe(1024n)
  })

  it('does not overlap slow requests, and does not retain an old value after a failed sample', async () => {
    state.enabled = true
    await vi.advanceTimersByTimeAsync(0)
    expect(memory.rss.value).toBe(1024n)
    const pending = Promise.withResolvers<bigint>()
    load.mockReturnValueOnce(pending.promise)
    await vi.advanceTimersByTimeAsync(90_000)
    expect(load).toHaveBeenCalledTimes(2)
    pending.reject(new Error('temporarily unavailable'))
    await vi.advanceTimersByTimeAsync(0)
    expect(memory.rss.value).toBeUndefined()
    expect(memory.unavailable.value).toBe(true)
    await vi.advanceTimersByTimeAsync(30_000)
    expect(memory.rss.value).toBe(1024n)
    expect(memory.unavailable.value).toBe(false)
  })
})
