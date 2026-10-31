// @vitest-environment jsdom
import { createApp, defineComponent, h, nextTick, type App } from 'vue'
import { createPinia, disposePinia, setActivePinia, type Pinia } from 'pinia'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

const mocks = vi.hoisted(() => ({
  version: vi.fn(),
  platform: vi.fn(),
  config: vi.fn(),
  subscriptions: vi.fn(),
  sync: vi.fn(),
  events: vi.fn(),
  ws: vi.fn(),
  core: vi.fn(),
  sleep: vi.fn(),
  reload: vi.fn(),
}))
vi.mock('@/stores', async () => ({
  useAppSettingsStore: (await import('@/stores/appSettings')).useAppSettingsStore,
  useAppStore: () => ({
    setupAppVersion: mocks.version,
    setupPlatform: mocks.platform,
    modalStack: [],
    showAbout: false,
    isAppReloading: false,
  }),
  useAppConfigStore: () => ({ setupAppConfig: mocks.config }),
  useKernelApiStore: () => ({ initCoreState: mocks.core, running: false }),
  useSubscribesStore: () => ({ setupSubscribes: mocks.subscriptions }),
}))
vi.mock('@/bridge', async () => ({
  loadAuthToken: (await import('@/bridge/http')).loadAuthToken,
  initWebSocket: mocks.ws,
}))
vi.mock('@/hooks/useBackendEvents', () => ({
  useBackendEvents: () => ({ setupBackendEvents: mocks.events, syncConfigStores: mocks.sync }),
}))
vi.mock('@/stores/modalLayout', () => ({ useModalLayoutStore: () => ({ splitActive: false }) }))
vi.mock('@/utils', async () => ({
  ...(await import('@/utils/others')),
  APP_TITLE: 'WebUI',
  sleep: mocks.sleep,
  message: { error: vi.fn() },
}))
vi.mock('@/lang', () => ({ default: { global: { locale: { value: 'en' } } } }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/components/_common/NavigationBar.vue', () => ({
  default: { render: () => h('nav', { 'data-testid': 'navigation' }, 'Ready') },
}))
vi.mock('@/components/_common/SplashView.vue', () => ({
  default: { render: () => h('div', { 'data-testid': 'splash' }, 'Loading') },
}))
vi.mock('@/components/_common/AboutView.vue', () => ({ default: { render: () => null } }))
vi.mock('@/components/_common/CommandView.vue', () => ({ default: { render: () => null } }))
vi.mock('@/components/_common/FullScreenLoading.vue', () => ({ default: { render: () => null } }))
vi.mock('@/components/_common/ModalPanel.vue', () => ({ default: { render: () => null } }))
import RootApp from '@/App.vue'
import { useAppSettingsStore } from '@/stores/appSettings'

describe('App and LoginView with real authentication bridge and Pinia session', () => {
  let app: App | undefined
  let pinia: Pinia | undefined
  let host: HTMLDivElement
  let sessions: Set<string>
  let tokenNumber: number
  const fetchMock = vi.fn()
  const settle = async () => {
    await new Promise((resolve) => setTimeout(resolve, 0))
    await nextTick()
  }
  const response = (data: string, status = 200) => ({
    ok: status < 400,
    status,
    statusText: 'Unauthorized',
    json: async () => ({ flag: true, data }),
  })
  const unmount = () => {
    app?.unmount()
    app = undefined
    if (pinia) disposePinia(pinia)
    pinia = undefined
  }
  const mount = async () => {
    pinia = createPinia()
    setActivePinia(pinia)
    app = createApp(RootApp)
    app.use(pinia)
    app.component(
      'Button',
      defineComponent({
        props: ['icon'],
        setup:
          (props, { attrs, slots }) =>
          () =>
            h('button', { ...attrs, 'data-icon': props.icon }, slots.default?.()),
      }),
    )
    for (const name of ['Icon', 'Progress', 'Modal', 'Menu', 'Tips', 'RouterView'])
      app.component(name, { render: () => null })
    app.directive('tips', {})
    app.mount(host)
    await settle()
  }
  const login = async (code = '') => {
    const password = host.querySelector<HTMLInputElement>('#login-password')!
    password.value = 'valid password'
    password.dispatchEvent(new Event('input', { bubbles: true }))
    if (code) {
      const field = host.querySelector<HTMLInputElement>('#login-code')!
      field.value = code
      field.dispatchEvent(new Event('input', { bubbles: true }))
    }
    await nextTick()
    host
      .querySelector('form')!
      .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
    await settle()
  }
  beforeEach(() => {
    vi.resetAllMocks()
    localStorage.clear()
    sessionStorage.clear()
    sessions = new Set()
    tokenNumber = 0
    window.matchMedia = vi
      .fn()
      .mockReturnValue({ matches: false, addEventListener: vi.fn(), removeEventListener: vi.fn() })
    vi.stubGlobal('location', { reload: mocks.reload })
    vi.stubGlobal('fetch', fetchMock)
    fetchMock.mockImplementation(async (url: string, options: RequestInit) => {
      if (url === '/api/auth/status')
        return response(JSON.stringify({ enabled: true, totp_enabled: false }))
      if (url === '/api/auth/login') {
        expect(JSON.parse(String(options.body)).args[0]).toBe('valid password')
        const token = `token-${++tokenNumber}`
        sessions.add(token)
        return response(token)
      }
      if (url === '/api/auth/logout') {
        const token = String((options.headers as Record<string, string>).Authorization).slice(7)
        expect(sessions.delete(token)).toBe(true)
        return response('Success')
      }
      throw new Error('Unexpected request ' + url)
    })
    for (const operation of [
      mocks.version,
      mocks.platform,
      mocks.config,
      mocks.subscriptions,
      mocks.sync,
    ]) {
      operation.mockImplementation(async () => {
        expect(sessions.has(useAppSettingsStore().sessionInfo.cacheToken)).toBe(true)
      })
    }
    mocks.sleep.mockResolvedValue(undefined)
    host = document.createElement('div')
    document.body.append(host)
  })
  afterEach(() => {
    unmount()
    host.remove()
    vi.unstubAllGlobals()
  })

  it('completes initial login and startup before removing Splash', async () => {
    const startup = Promise.withResolvers<void>()
    mocks.config.mockReturnValueOnce(startup.promise)
    await mount()
    expect(host.querySelector('#login-password')).not.toBeNull()
    expect(mocks.config).not.toHaveBeenCalled()
    await login()
    expect(mocks.config).toHaveBeenCalledTimes(1)
    expect(mocks.platform).toHaveBeenCalledTimes(1)
    expect(mocks.ws).toHaveBeenCalledWith('token-1')
    expect(host.querySelector('#login-password')).toBeNull()
    expect(host.querySelector('[data-testid=splash]')).not.toBeNull()
    startup.resolve()
    await settle()
    expect(host.querySelector('[data-testid=splash]')).toBeNull()
    expect(host.querySelector('[data-testid=navigation]')).not.toBeNull()
    expect(mocks.core).toHaveBeenCalledTimes(1)
    expect(mocks.reload).not.toHaveBeenCalled()
  })

  it('logs out through the real TitleBar and successfully initializes again after reload and login', async () => {
    await mount()
    await login()
    expect(host.querySelector('[data-testid=navigation]')).not.toBeNull()
    host.querySelector<HTMLButtonElement>('[data-icon=logout]')!.click()
    await settle()
    expect(mocks.reload).toHaveBeenCalledTimes(1)
    expect(sessions.size).toBe(0)
    expect(JSON.parse(sessionStorage.getItem('sessionInfo')!)).toMatchObject({
      cacheToken: '',
      requireLogin: true,
    })
    unmount()
    await mount()
    expect(host.querySelector('#login-password')).not.toBeNull()
    await login()
    expect(host.querySelector('[data-testid=splash]')).toBeNull()
    expect(host.querySelector('[data-testid=navigation]')).not.toBeNull()
    expect(mocks.config).toHaveBeenCalledTimes(2)
    expect(mocks.core).toHaveBeenCalledTimes(2)
    expect(mocks.ws).toHaveBeenLastCalledWith('token-2')
    expect(useAppSettingsStore().sessionInfo.requireLogin).toBe(false)
  })
})
