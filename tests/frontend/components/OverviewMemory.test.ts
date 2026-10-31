// @vitest-environment jsdom
import { createApp, defineComponent, h, nextTick, reactive, type App } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

const mocks = vi.hoisted(() => ({
  memory: vi.fn(),
  kernel: null as any,
  settings: null as any,
}))
vi.mock('@/stores', () => ({
  useKernelApiStore: () => mocks.kernel,
  useAppSettingsStore: () => mocks.settings,
}))
vi.mock('@/bridge', () => ({ createRpcClient: () => ({ getCurrentCoreMemory: mocks.memory }) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/utils', () => ({
  formatBytes: (value: bigint | number) => `${value} B`,
  handleChangeMode: vi.fn(),
  message: { error: vi.fn() },
}))
vi.mock('@/components/Modal', () => ({ useModal: () => [{ render: () => null }, {}] }))
vi.mock('@/views/HomeView/components/ConnectionsController.vue', () => ({
  default: { render: () => null },
}))
vi.mock('@/views/HomeView/components/LogsController.vue', () => ({
  default: { render: () => null },
}))
import Overview from '@/views/HomeView/components/OverView.vue'

describe('overview native memory and optional process RSS', () => {
  let app: App
  let host: HTMLDivElement
  beforeEach(async () => {
    mocks.memory.mockReset().mockResolvedValue({ rss: 4096n })
    mocks.settings = reactive({ app: { kernel: { realMemoryUsage: false } } })
    mocks.kernel = reactive({
      running: true,
      pid: 42,
      runtimeInbounds: [],
      config: { modeList: [], mode: '' },
      onStatus: (listener: (value: any) => void) => {
        listener({
          memory: 512n,
          goroutines: 2,
          connectionsIn: 1,
          uplinkTotal: 0n,
          downlinkTotal: 0n,
          trafficAvailable: false,
        })
        return vi.fn()
      },
    })
    host = document.createElement('div')
    document.body.append(host)
    app = createApp({ render: () => h(Overview) })
    app.component(
      'Card',
      defineComponent({
        props: ['title'],
        setup:
          (props, { slots }) =>
          () =>
            h('section', [h('h3', props.title), slots.default?.()]),
      }),
    )
    for (const name of ['Icon', 'Switch', 'Button', 'TrafficChart'])
      app.component(name, { render: () => null })
    app.directive('tips', {})
    app.mount(host)
    await nextTick()
  })
  afterEach(() => {
    app.unmount()
    host.remove()
  })

  it('keeps native memory as the default, labels RSS distinctly and restores native data when disabled', async () => {
    const card = () => host.querySelector('[data-memory-card]')!.textContent
    expect(card()).toContain('home.overview.memoryNative')
    expect(card()).toContain('512 B')
    expect(mocks.memory).not.toHaveBeenCalled()
    mocks.settings.app.kernel.realMemoryUsage = true
    await vi.waitFor(() => expect(card()).toContain('4096 B'))
    expect(card()).toContain('home.overview.memoryRSS')
    expect(mocks.memory).toHaveBeenCalledWith(
      {},
      { signal: expect.any(AbortSignal), timeoutMs: 5000 },
    )
    mocks.settings.app.kernel.realMemoryUsage = false
    await nextTick()
    expect(card()).toContain('home.overview.memoryNative')
    expect(card()).toContain('512 B')
  })

  it('does not label a native fallback as RSS when the process measurement fails', async () => {
    mocks.memory.mockRejectedValueOnce(new Error('unavailable'))
    mocks.settings.app.kernel.realMemoryUsage = true
    await vi.waitFor(() =>
      expect(host.querySelector('[data-memory-card]')!.textContent).toContain(
        'home.overview.memoryUnavailable',
      ),
    )
    expect(host.querySelector('[data-memory-card]')!.textContent).not.toContain('512 B')
  })

  it('shows a dash rather than permanent loading when the core stops with RSS enabled', async () => {
    const card = () => host.querySelector('[data-memory-card]')!.textContent
    mocks.settings.app.kernel.realMemoryUsage = true
    await vi.waitFor(() => expect(card()).toContain('4096 B'))
    mocks.kernel.running = false
    await nextTick()
    expect(card()).toContain('home.overview.memoryRSS')
    expect(card()).toContain('—')
    expect(card()).not.toContain('common.loading')
    expect(card()).not.toContain('4096 B')
    expect(mocks.memory).toHaveBeenCalledOnce()
  })
})
