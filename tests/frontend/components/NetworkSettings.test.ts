// @vitest-environment jsdom
import { createApp, defineComponent, h, nextTick, reactive, type App } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

const mocks = vi.hoisted(() => ({
  save: vi.fn(),
  test: vi.fn(),
  success: vi.fn(),
  error: vi.fn(),
  config: {} as Record<string, string | boolean>,
}))
vi.mock('@/stores/appConfig', () => ({
  useAppConfigStore: () => ({
    get config() { return mocks.config },
    saveNow: mocks.save,
    testNetworkRequest: mocks.test,
  }),
}))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ currentVersion: '1.0' }) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/lang', () => ({ default: { global: { t: (key: string) => key } } }))
vi.mock('@/bridge', () => ({ ClipboardGetText: vi.fn() }))
vi.mock('@/utils', async () => ({
  ...(await import('@/utils/others')),
  APP_TITLE: 'webui',
  message: { success: mocks.success, error: mocks.error },
}))
import NetworkSettings from '@/views/SettingsView/components/components/NetworkSettings.vue'
import Button from '@/components/Button/index.vue'
import Input from '@/components/Input/index.vue'
import Radio from '@/components/Radio/index.vue'
import Switch from '@/components/Switch/index.vue'

describe('upstream network settings controls with backend persistence', () => {
  let app: App
  let host: HTMLDivElement
  const initial = () => ({
    requestProxyMode: 'custom',
    customProxy: 'http://127.0.0.1:8080',
    userAgent: 'old-agent',
    githubApiToken: '',
    githubDownloadMirror: '',
    githubDownloadAccelerationDisabled: true,
  })
  const flush = async () => {
    await nextTick()
    await new Promise((resolve) => setTimeout(resolve, 0))
    await nextTick()
  }
  beforeEach(async () => {
    vi.resetAllMocks()
    mocks.config = reactive(initial())
    mocks.save.mockResolvedValue(undefined)
    mocks.test.mockResolvedValue({ statusCode: 204, durationMs: 1n })
    host = document.createElement('div')
    document.body.append(host)
    app = createApp(NetworkSettings)
    app.component('Card', defineComponent({ setup: (_, { slots }) => () => h('section', slots.default?.()) }))
    app.component('Input', Input)
    app.component('Radio', Radio)
    app.component('Switch', Switch)
    app.component('Button', Button)
    app.component('Icon', { render: () => null })
    app.component('CodeViewer', { render: () => null })
    app.directive('tips', {})
    app.mount(host)
    await flush()
  })
  afterEach(() => {
    app.unmount()
    host.remove()
  })
  const button = (text: string) =>
    [...host.querySelectorAll<HTMLElement>('[role="button"]')].find(
      (item) => item.textContent?.trim() === text,
    )!
  const labeled = (label: string) => host.querySelector<HTMLElement>(`[role="button"][aria-label="${label}"]`)!
  const input = (label: string) => host.querySelector<HTMLInputElement>(`input[aria-label="${label}"]`)!
  const radio = (mode: string) => [...host.querySelectorAll<HTMLButtonElement>('[role="radio"]')]
    .find(item => item.textContent?.trim() === `settings.requestProxy.${mode}`)!
  const edit = async (label: string, value: string, submit = true) => {
    labeled(label).click()
    await flush()
    const target = input(label)
    target.value = value
    target.dispatchEvent(new Event('input', { bubbles: true }))
    if (submit) {
      target.blur()
      await vi.waitFor(() => expect(host.contains(target)).toBe(false))
    }
    await flush()
    return target
  }

  it('renders upstream segmented modes and click-to-edit fields without changing defaults', () => {
    expect(host.querySelectorAll('[role="radio"]')).toHaveLength(4)
    expect(radio('custom').getAttribute('aria-checked')).toBe('true')
    expect(host.querySelector('[role="combobox"]')).toBeNull()
    expect(host.querySelectorAll('input')).toHaveLength(0)
    expect(labeled('settings.userAgent.name').textContent).toBe('old-agent')
    expect(labeled('settings.githubapi.name').textContent).toBe('common.none')
    expect(host.querySelector('[role="switch"]')?.getAttribute('aria-checked')).toBe('false')
    expect(mocks.save).not.toHaveBeenCalled()
  })

  it('preserves failed edit drafts and persists them before testing the backend', async () => {
    mocks.save.mockImplementationOnce(async () => {
      Object.assign(mocks.config, initial())
      throw new Error('disk write failed')
    })
    await edit('settings.userAgent.name', 'new-agent')
    expect(host.querySelector('[role="alert"]')?.textContent).toContain('disk write failed')
    expect(labeled('settings.userAgent.name').textContent).toBe('new-agent')
    expect(button('common.save')).toBeDefined()
    mocks.save.mockImplementationOnce(async () => {
      expect(mocks.config.userAgent).toBe('new-agent')
    })
    button('settings.requestProxy.test').click()
    await flush()
    expect(mocks.save).toHaveBeenCalledTimes(2)
    expect(mocks.test).toHaveBeenCalledWith('https://www.gstatic.com/generate_204')
    expect(mocks.save.mock.invocationCallOrder[1]).toBeLessThan(mocks.test.mock.invocationCallOrder[0]!)
    expect(mocks.success).toHaveBeenCalledWith('settings.requestProxy.testSuccess')
    expect(host.querySelector('[role="status"]')).toBeNull()
  })

  it('does not probe when saving the network policy fails', async () => {
    mocks.save.mockRejectedValue(new Error('invalid proxy'))
    button('settings.requestProxy.test').click()
    await flush()
    expect(mocks.test).not.toHaveBeenCalled()
    expect(mocks.error).toHaveBeenCalledWith('invalid proxy')
  })

  it('recovers from choosing custom mode without a URL while keeping the failed draft visible', async () => {
    Object.assign(mocks.config, { requestProxyMode: 'environment', customProxy: '' })
    await flush()
    mocks.save.mockImplementationOnce(async () => {
      Object.assign(mocks.config, { requestProxyMode: 'environment', customProxy: '' })
      throw new Error('Custom proxy URL is required')
    })
    radio('custom').click()
    await flush()
    expect(mocks.config.requestProxyMode).toBe('environment')
    expect(radio('custom').getAttribute('aria-checked')).toBe('true')
    expect(host.querySelector('[role="alert"]')?.textContent).toContain('Custom proxy URL is required')
    expect(host.querySelector('[role="status"]')).toBeNull()
    expect(button('common.save')).toBeDefined()
    expect(labeled('settings.requestProxy.custom')).not.toBeNull()
    await edit('settings.requestProxy.custom', 'socks5://127.0.0.1:1080')
    expect(mocks.config.requestProxyMode).toBe('custom')
    expect(mocks.config.customProxy).toBe('socks5://127.0.0.1:1080')
    expect(mocks.save).toHaveBeenCalledTimes(2)
    expect(host.querySelector('[role="alert"]')).toBeNull()
    expect(host.querySelector('[role="status"]')).toBeNull()
  })

  it.each(['save', 'probe'])('locks real radio and editable controls while %s is pending', async phase => {
    let release!: () => void
    const pending = new Promise<void>(resolve => { release = resolve })
    if (phase === 'save') mocks.save.mockReturnValueOnce(pending)
    else mocks.test.mockImplementationOnce(async () => { await pending; return { statusCode: 204, durationMs: 1n } })
    if (phase === 'save') radio('core').click()
    else button('settings.requestProxy.test').click()
    await flush()
    expect(radio('none').disabled).toBe(true)
    radio('none').click()
    labeled('settings.userAgent.name').click()
    await flush()
    expect(input('settings.userAgent.name')).toBeNull()
    expect(mocks.config.requestProxyMode).toBe(phase === 'save' ? 'core' : 'custom')
    release()
    await flush()
    expect(radio('none').disabled).toBe(false)
    radio('environment').click()
    await flush()
    expect(mocks.config.requestProxyMode).toBe('environment')
  })

  it('keeps mirror URL when disabled and persists enabling an empty URL', async () => {
    const toggle = host.querySelector<HTMLElement>('[role="switch"]')!
    toggle.click()
    await flush()
    expect(mocks.config.githubDownloadAccelerationDisabled).toBe(false)
    expect(labeled('settings.githubDownloadAcceleration.mirror')).not.toBeNull()
    await edit('settings.githubDownloadAcceleration.mirror', 'https://mirror.invalid/{url}')
    expect(toggle.getAttribute('aria-checked')).toBe('true')
    toggle.click()
    await flush()
    expect(mocks.config.githubDownloadAccelerationDisabled).toBe(true)
    expect(mocks.config.githubDownloadMirror).toBe('https://mirror.invalid/{url}')
    expect(labeled('settings.githubDownloadAcceleration.mirror')).toBeNull()
    toggle.click()
    await flush()
    expect(labeled('settings.githubDownloadAcceleration.mirror').textContent).toBe('https://mirror.invalid/{url}')
  })

  it('masks saved tokens, edits as password, and resets only on explicit click', async () => {
    mocks.config.githubApiToken = 'token-private-value'
    await flush()
    expect(labeled('settings.githubapi.name').textContent).toBe('••••••••')
    expect(host.textContent).not.toContain('token-private-value')
    expect(host.innerHTML).not.toContain('token-private-value')
    const target = await edit('settings.githubapi.name', 'replacement-token', false)
    expect(target.type).toBe('password')
    target.blur()
    await flush()
    expect(mocks.config.githubApiToken).toBe('replacement-token')
    expect(host.textContent).not.toContain('replacement-token')
    labeled('settings.githubapi.reset').click()
    await flush()
    expect(mocks.config.githubApiToken).toBe('')
    expect(labeled('settings.githubapi.name').textContent).toBe('common.none')
    labeled('settings.userAgent.reset').click()
    await flush()
    expect(mocks.config.userAgent).toBe('')
    expect(labeled('settings.userAgent.name').textContent).toBe('webui/1.0')
  })

  it('supports keyboard proxy selection without losing the saved custom address', async () => {
    radio('custom').focus()
    radio('custom').dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowLeft', bubbles: true, cancelable: true }))
    await flush()
    expect(mocks.config.requestProxyMode).toBe('core')
    expect(mocks.config.customProxy).toBe('http://127.0.0.1:8080')
    expect(document.activeElement).toBe(radio('core'))
  })
})
