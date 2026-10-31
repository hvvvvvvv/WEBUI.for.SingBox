// @vitest-environment jsdom
import { createApp, nextTick, type App } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

const mocks = vi.hoisted(() => ({ status: vi.fn(), login: vi.fn(), authenticated: vi.fn() }))
vi.mock('@/api/auth', () => ({
  getAuthStatus: mocks.status,
  AuthError: class extends Error {
    kind: string
    constructor(
      public status: number,
      message: string,
    ) {
      super(message)
      this.kind = message === 'Invalid password or verification code' ? 'credentials' : 'request'
    }
  },
}))
vi.mock('@/bridge/http', () => ({ loginAuthToken: mocks.login }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
import { AuthError } from '@/api/auth'
import LoginView from '@/views/LoginView.vue'

describe('password and optional TOTP login', () => {
  let app: App
  let host: HTMLDivElement
  const settle = async () => {
    await new Promise((resolve) => setTimeout(resolve, 0))
    await nextTick()
  }
  const input = async (id: string, value: string) => {
    const element = host.querySelector<HTMLInputElement>(`#${id}`)!
    element.value = value
    element.dispatchEvent(new Event('input', { bubbles: true }))
    await nextTick()
  }
  const submit = async () => {
    host
      .querySelector('form')!
      .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
    await settle()
  }
  const mount = async () => {
    app = createApp(LoginView, { onAuthenticated: mocks.authenticated })
    app.mount(host)
    await settle()
  }
  beforeEach(() => {
    vi.resetAllMocks()
    mocks.status.mockResolvedValue({ enabled: true, totp_enabled: false })
    mocks.login.mockResolvedValue(true)
    host = document.createElement('div')
    document.body.append(host)
  })
  afterEach(() => {
    app.unmount()
    host.remove()
  })
  it('uses labeled password inputs and does not require a code for password-only login', async () => {
    await mount()
    expect(host.querySelector('label[for="login-password"]')).not.toBeNull()
    expect(host.querySelector('#login-code')).toBeNull()
    expect(host.querySelector('#login-password')?.getAttribute('autocomplete')).toBe(
      'current-password',
    )
    await input('login-password', ' short old password ')
    await submit()
    expect(mocks.login).toHaveBeenCalledExactlyOnceWith(' short old password ', '')
    expect(mocks.authenticated).toHaveBeenCalledTimes(1)
    expect(host.querySelector<HTMLInputElement>('#login-password')!.value).toBe('')
  })
  it('accepts a recovery code without stripping characters and blocks duplicate submissions', async () => {
    mocks.status.mockResolvedValue({ enabled: true, totp_enabled: true })
    const request = Promise.withResolvers<boolean>()
    mocks.login.mockReturnValueOnce(request.promise)
    await mount()
    await input('login-password', 'password')
    await submit()
    expect(mocks.login).not.toHaveBeenCalled()
    await input('login-code', 'ABCD-EFGH-IJKL')
    await submit()
    await submit()
    expect(mocks.login).toHaveBeenCalledExactlyOnceWith('password', 'ABCD-EFGH-IJKL')
    request.resolve(true)
    await settle()
    expect(mocks.authenticated).toHaveBeenCalledTimes(1)
    expect(host.querySelector<HTMLInputElement>('#login-code')!.value).toBe('')
  })
  it('keeps the password and clears rejected OTPs while displaying an accessible error', async () => {
    mocks.status.mockResolvedValue({ enabled: true, totp_enabled: true })
    mocks.login.mockRejectedValueOnce(new AuthError(200, 'Invalid password or verification code'))
    await mount()
    await input('login-password', 'password')
    await input('login-code', '123456')
    await submit()
    expect(host.querySelector('[role=alert]')?.textContent).toContain('auth.invalidSecret')
    expect(host.querySelector<HTMLInputElement>('#login-password')!.value).toBe('password')
    expect(host.querySelector<HTMLInputElement>('#login-code')!.value).toBe('')
    expect(mocks.authenticated).not.toHaveBeenCalled()
  })
  it('provides retry when public status is unavailable without attempting login', async () => {
    mocks.status.mockRejectedValueOnce(new Error('offline'))
    await mount()
    expect(host.querySelector('[role=alert]')?.textContent).toContain('auth.statusFailed')
    expect(mocks.login).not.toHaveBeenCalled()
    host.querySelector<HTMLButtonElement>('button')!.click()
    await settle()
    expect(mocks.status).toHaveBeenCalledTimes(2)
    expect(host.querySelector('#login-password')).not.toBeNull()
  })
})
