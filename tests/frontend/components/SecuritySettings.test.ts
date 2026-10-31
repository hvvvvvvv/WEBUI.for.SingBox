// @vitest-environment jsdom
import { createApp, h, KeepAlive, nextTick, ref, type App } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

const mocks = vi.hoisted(() => ({
  status: vi.fn(),
  begin: vi.fn(),
  cancel: vi.fn(),
  enable: vi.fn(),
  disable: vi.fn(),
  regenerate: vi.fn(),
  password: vi.fn(),
  replace: vi.fn(),
  ws: vi.fn(),
  qr: vi.fn(),
}))
vi.mock('@/api/auth', () => ({
  getAuthStatus: mocks.status,
  beginTotpSetup: mocks.begin,
  cancelTotpSetup: mocks.cancel,
  enableTotp: mocks.enable,
  disableTotp: mocks.disable,
  regenerateRecoveryCodes: mocks.regenerate,
  setAccessPassword: mocks.password,
  replaceAuthSession: mocks.replace,
  AuthError: class extends Error {
    constructor(
      public status: number,
      message: string,
    ) {
      super(message)
    }
  },
}))
vi.mock('@/bridge/ws', () => ({ initWebSocket: mocks.ws }))
vi.mock('qrcode', () => ({ default: { toCanvas: mocks.qr } }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
import { AuthError } from '@/api/auth'
import SecuritySettings from '@/views/SettingsView/components/SecuritySettings.vue'

describe('access settings and authenticator enrollment', () => {
  let app: App | undefined
  let host: HTMLDivElement
  const active = ref(true)
  const longPassword = 'a secure long passphrase'
  const enrollment = () => ({
    secret: 'PRIVATESETUPKEY',
    otpauth_uri: 'otpauth://totp/WebUI?secret=PRIVATESETUPKEY',
    expires_at: new Date(Date.now() + 300_000).toISOString(),
  })
  const settle = async () => {
    await new Promise((resolve) => setTimeout(resolve, 0))
    await nextTick()
  }
  const mount = async () => {
    app = createApp({
      render: () =>
        h(KeepAlive, null, { default: () => (active.value ? h(SecuritySettings) : null) }),
    })
    app.mount(host)
    await settle()
  }
  const input = async (id: string, value: string) => {
    const element = host.querySelector<HTMLInputElement>(`#${id}`)!
    element.value = value
    element.dispatchEvent(new Event('input', { bubbles: true }))
    await nextTick()
  }
  const button = (key: string) =>
    [...host.querySelectorAll<HTMLButtonElement>('button')].find(
      (item) => item.textContent?.trim() === key,
    )!
  const click = async (key: string) => {
    button(key).click()
    await settle()
  }
  const startSetup = async () => {
    await input('totp-password', 'current password')
    await click('auth.startTotp')
  }
  beforeEach(() => {
    vi.resetAllMocks()
    active.value = true
    localStorage.clear()
    sessionStorage.clear()
    mocks.status.mockResolvedValue({ enabled: true, totp_enabled: false })
    mocks.begin.mockImplementation(async () => enrollment())
    mocks.cancel.mockResolvedValue('')
    mocks.qr.mockResolvedValue(undefined)
    mocks.password.mockResolvedValue('new-password-token')
    mocks.enable.mockResolvedValue({
      token: 'enabled-token',
      recovery_codes: ['AAAA-BBBB', 'CCCC-DDDD'],
    })
    mocks.disable.mockResolvedValue('disabled-token')
    mocks.regenerate.mockResolvedValue({ token: 'regenerated-token', recovery_codes: ['NEW-CODE'] })
    host = document.createElement('div')
    document.body.append(host)
  })
  afterEach(() => {
    app?.unmount()
    app = undefined
    host.remove()
  })

  it('validates new passwords before setup and requires a password before starting TOTP', async () => {
    mocks.status.mockResolvedValue({ enabled: false, totp_enabled: false })
    await mount()
    expect(host.querySelector('#current-password')).toBeNull()
    expect(host.querySelector('#totp-password')).toBeNull()
    expect(host.textContent).toContain('auth.passwordFirst')
    await input('new-password', 'short')
    await input('confirm-password', 'short')
    await click('auth.setPassword')
    expect(mocks.password).not.toHaveBeenCalled()
    expect(host.querySelector('[role=alert]')?.textContent).toContain('auth.passwordRules')
    await input('new-password', longPassword)
    await input('confirm-password', 'mismatch')
    await click('auth.setPassword')
    expect(host.querySelector('[role=alert]')?.textContent).toContain('auth.mismatch')
    await input('confirm-password', longPassword)
    await click('auth.setPassword')
    expect(mocks.password).toHaveBeenCalledExactlyOnceWith(longPassword, '', '')
    expect(mocks.replace).toHaveBeenCalledWith('new-password-token', true)
    expect(mocks.ws).toHaveBeenCalledWith('new-password-token')
    expect(host.querySelector<HTMLInputElement>('#new-password')!.value).toBe('')
    expect(host.querySelector('#totp-password')).not.toBeNull()
  })

  it('requires current password and raw second factor to change a protected password', async () => {
    mocks.status.mockResolvedValue({ enabled: true, totp_enabled: true })
    await mount()
    expect(button('auth.clearAuth')).toBeUndefined()
    await input('new-password', longPassword)
    await input('confirm-password', longPassword)
    expect(button('auth.changePassword').disabled).toBe(true)
    await input('current-password', ' old ')
    await input('password-code', 'RAW-RECOVERY')
    await click('auth.changePassword')
    expect(mocks.password).toHaveBeenCalledExactlyOnceWith(longPassword, ' old ', 'RAW-RECOVERY')
    expect(host.querySelector<HTMLInputElement>('#password-code')!.value).toBe('')
  })

  it('renders QR locally and enables only after OTP confirmation, then requires saving recovery codes', async () => {
    await mount()
    await startSetup()
    expect(mocks.begin).toHaveBeenCalledExactlyOnceWith('current password')
    expect(mocks.enable).not.toHaveBeenCalled()
    expect(mocks.qr).toHaveBeenCalledWith(
      expect.any(HTMLCanvasElement),
      enrollment().otpauth_uri,
      expect.objectContaining({ width: 224 }),
    )
    expect(host.querySelector<HTMLInputElement>('#totp-secret')!.value).toBe('PRIVATESETUPKEY')
    expect(button('auth.changePassword').disabled).toBe(true)
    expect(button('auth.enableTotp').disabled).toBe(true)
    await input('totp-confirm-code', '123456')
    await click('auth.enableTotp')
    expect(mocks.enable).toHaveBeenCalledExactlyOnceWith('123456')
    expect(mocks.replace).toHaveBeenCalledWith('enabled-token', true)
    expect(host.querySelector('#totp-secret')).toBeNull()
    expect(host.querySelector<HTMLTextAreaElement>('#recovery-codes')!.value).toBe(
      'AAAA-BBBB\nCCCC-DDDD',
    )
    expect(button('auth.finish').disabled).toBe(true)
    expect(button('auth.regenerateCodes').disabled).toBe(true)
    const checkbox = host.querySelector<HTMLInputElement>('.recovery-card input[type=checkbox]')!
    checkbox.checked = true
    checkbox.dispatchEvent(new Event('change', { bubbles: true }))
    await nextTick()
    await click('auth.finish')
    expect(host.querySelector('#recovery-codes')).toBeNull()
    expect(button('auth.regenerateCodes').disabled).toBe(false)
    expect(localStorage.length).toBe(0)
    expect(sessionStorage.length).toBe(0)
  })

  it('retains enrollment for a fresh OTP after rejection and never adopts a failed token', async () => {
    mocks.enable.mockRejectedValueOnce(new AuthError(200, 'Invalid or already used code'))
    await mount()
    await startSetup()
    await input('totp-confirm-code', '123456')
    await click('auth.enableTotp')
    expect(mocks.replace).not.toHaveBeenCalled()
    expect(host.querySelector('[role=alert]')?.textContent).toContain(
      'Invalid or already used code',
    )
    expect(host.querySelector<HTMLInputElement>('#totp-confirm-code')!.value).toBe('')
    expect(host.querySelector('#totp-secret')).not.toBeNull()
  })

  it('supports manual key entry if QR rendering fails and clears pending setup on cancel', async () => {
    mocks.qr.mockRejectedValueOnce(new Error('canvas unavailable'))
    await mount()
    await startSetup()
    expect(host.textContent).toContain('auth.qrFailed')
    expect(host.querySelector<HTMLInputElement>('#totp-secret')!.value).toBe('PRIVATESETUPKEY')
    await click('auth.cancel')
    expect(mocks.cancel).toHaveBeenCalledTimes(1)
    expect(host.querySelector('#totp-secret')).toBeNull()
    expect(host.querySelector<HTMLInputElement>('#totp-password')!.value).toBe('')
  })

  it('cancels and clears pending credentials when the settings tab is deactivated', async () => {
    await mount()
    await startSetup()
    active.value = false
    await settle()
    expect(mocks.cancel).toHaveBeenCalledTimes(1)
    active.value = true
    await settle()
    expect(host.querySelector('#totp-secret')).toBeNull()
    expect(host.querySelector<HTMLInputElement>('#totp-password')!.value).toBe('')
    expect(mocks.status).toHaveBeenCalledTimes(2)
  })

  it('cancels late enrollment responses after leaving the page instead of showing stale secrets', async () => {
    const pending = Promise.withResolvers<ReturnType<typeof enrollment>>()
    mocks.begin.mockReturnValueOnce(pending.promise)
    await mount()
    await startSetup()
    active.value = false
    await settle()
    pending.resolve(enrollment())
    await settle()
    expect(mocks.cancel).toHaveBeenCalledTimes(2)
    active.value = true
    await settle()
    expect(host.querySelector('#totp-secret')).toBeNull()
    expect(mocks.qr).not.toHaveBeenCalled()
  })

  it('rejects an expired setup instead of rendering a stale key', async () => {
    mocks.begin.mockResolvedValueOnce({
      ...enrollment(),
      expires_at: new Date(Date.now() - 1000).toISOString(),
    })
    await mount()
    await startSetup()
    expect(host.querySelector('[role=alert]')?.textContent).toContain('auth.setupExpired')
    expect(host.querySelector('#totp-secret')).toBeNull()
    expect(mocks.cancel).toHaveBeenCalledTimes(1)
  })

  it.each(['disable', 'regenerate'] as const)(
    'verifies password and recovery code before %s and replaces the current session',
    async (operation) => {
      mocks.status.mockResolvedValue({ enabled: true, totp_enabled: true })
      await mount()
      expect(host.querySelector('#totp-secret')).toBeNull()
      expect(mocks.begin).not.toHaveBeenCalled()
      await click(operation === 'disable' ? 'auth.disableTotp' : 'auth.regenerateCodes')
      expect(button('auth.confirmAction').disabled).toBe(true)
      await input('totp-action-password', ' current ')
      await input('totp-action-code', 'OLD-RECOVERY')
      await click('auth.confirmAction')
      expect(mocks[operation]).toHaveBeenCalledExactlyOnceWith(' current ', 'OLD-RECOVERY')
      expect(mocks.replace).toHaveBeenCalledWith(
        operation === 'disable' ? 'disabled-token' : 'regenerated-token',
        true,
      )
      if (operation === 'disable') expect(host.querySelector('#totp-password')).not.toBeNull()
      else
        expect(host.querySelector<HTMLTextAreaElement>('#recovery-codes')!.value).toBe('NEW-CODE')
    },
  )

  it('requires explicit confirmation plus the current password to remove password protection', async () => {
    await mount()
    await click('auth.clearAuth')
    expect(button('auth.confirmClear').disabled).toBe(true)
    const checkbox = host.querySelector<HTMLInputElement>('.security-danger input[type=checkbox]')!
    checkbox.checked = true
    checkbox.dispatchEvent(new Event('change', { bubbles: true }))
    await nextTick()
    expect(button('auth.confirmClear').disabled).toBe(true)
    await input('current-password', 'current')
    await click('auth.confirmClear')
    expect(mocks.password).toHaveBeenCalledExactlyOnceWith('', 'current', '')
    expect(mocks.replace).toHaveBeenCalledWith('new-password-token', false)
    expect(host.textContent).toContain('auth.passwordFirst')
  })
})
