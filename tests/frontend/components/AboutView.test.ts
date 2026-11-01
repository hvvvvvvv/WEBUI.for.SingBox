// @vitest-environment jsdom
import { createApp, defineComponent, h, nextTick, reactive, type App } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

const state = vi.hoisted(() => ({
  checkForUpdates: vi.fn(),
  applyAppUpdate: vi.fn(),
  downloadApp: vi.fn(),
  currentVersion: '1.28.1-backend',
  updatedVersion: '1.28.2-staged',
  remoteVersion: '1.28.3-remote',
  updateReady: false,
  updatable: false,
  downloading: false,
  checkForUpdatesLoading: false,
}))
vi.mock('@/stores', () => ({ useAppStore: () => reactive(state) }))
vi.mock('@/utils/env', () => ({ APP_TITLE: 'WEBUI.for.SingBox', APP_VERSION: '1.28.1-ui' }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
import AboutView from '@/components/_common/AboutView.vue'

describe('About version information and updates', () => {
  let app: App
  let host: HTMLDivElement
  beforeEach(() => {
    vi.clearAllMocks()
    state.updateReady = false
    state.updatable = false
    host = document.createElement('div')
    document.body.append(host)
    app = createApp(AboutView)
    app.component(
      'Button',
      defineComponent({
        setup:
          (_, { slots }) =>
          () =>
            h('button', slots.default?.()),
      }),
    )
    app.mount(host)
  })
  afterEach(() => {
    app.unmount()
    host.remove()
  })

  it('distinguishes the running bridge version from the UI build version', () => {
    expect(host.textContent).toContain('Bridge: 1.28.1-backend - UI: 1.28.1-ui')
    expect(host.querySelector('img')?.getAttribute('alt')).toBe('WEBUI.for.SingBox')
    expect(state.checkForUpdates).toHaveBeenCalledExactlyOnceWith()
    host.querySelector('button')!.click()
    expect(state.checkForUpdates).toHaveBeenLastCalledWith(true)
  })

  it('retains explicit download and apply actions for available updates', async () => {
    reactive(state).updatable = true
    await nextTick()
    const button = (text: string) =>
      [...host.querySelectorAll('button')].find((item) => item.textContent?.includes(text))!
    button('about.new').click()
    expect(state.downloadApp).toHaveBeenCalledTimes(1)
    reactive(state).updatable = false
    reactive(state).updateReady = true
    await nextTick()
    expect(host.textContent).toContain(state.updatedVersion)
    button('about.applyUpdate').click()
    expect(state.applyAppUpdate).toHaveBeenCalledTimes(1)
  })
})
