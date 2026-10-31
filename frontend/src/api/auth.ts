import { useAppSettingsStore } from '@/stores/appSettings'

export interface AuthStatus {
  enabled: boolean
  totp_enabled: boolean
}
export interface TotpEnrollment {
  secret: string
  otpauth_uri: string
  expires_at: string
}
export interface AuthRecovery {
  token: string
  recovery_codes: string[]
}
export class AuthError extends Error {
  readonly kind: 'credentials' | 'rate_limited' | 'session' | 'request'
  constructor(
    public status: number,
    message: string,
  ) {
    super(message)
    this.kind =
      status === 429 || message === 'Too many failed attempts, try again later'
        ? 'rate_limited'
        : message === 'Session expired; sign in again' || status === 401
          ? 'session'
          : message === 'Invalid password or verification code'
            ? 'credentials'
            : 'request'
  }
}

// Credential operations must never be automatically replayed: OTPs and recovery codes are single-use.
const requestAuth = async <T>(
  path: string,
  args: string[] = [],
  authenticated = true,
): Promise<T> => {
  const token = authenticated ? useAppSettingsStore().sessionInfo.cacheToken : ''
  if (authenticated && !token) throw new AuthError(401, 'Authentication required')
  const controller = new AbortController()
  const timer = setTimeout(() => controller.abort(), 30_000)
  try {
    const response = await fetch(`/api/auth${path}`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        ...(authenticated ? { Authorization: `Bearer ${token}` } : {}),
      },
      body: JSON.stringify({ args }),
      cache: 'no-store',
      signal: controller.signal,
    })
    const result = await response.json().catch(() => null)
    if (!response.ok || result?.flag !== true) {
      const detail = typeof result?.data === 'string' ? result.data : response.statusText
      throw new AuthError(
        response.status,
        (detail || 'Authentication request failed').slice(0, 300),
      )
    }
    return result.data as T
  } finally {
    clearTimeout(timer)
  }
}

const requestAuthObject = async <T>(
  path: string,
  args: string[] = [],
  authenticated = true,
): Promise<T> => {
  const data = await requestAuth<string>(path, args, authenticated)
  try {
    return JSON.parse(data) as T
  } catch {
    throw new AuthError(500, 'Invalid authentication response')
  }
}

let authChange: Promise<void> | null = null
export const waitForAuthChange = async () => {
  if (authChange) await authChange
}
const changeSession = async <T>(
  operation: () => Promise<T>,
  token: (result: T) => string,
  enabled: boolean,
): Promise<T> => {
  if (authChange) throw new AuthError(409, 'Another security change is in progress')
  let finish!: () => void
  authChange = new Promise<void>((resolve) => {
    finish = resolve
  })
  try {
    const result = await operation()
    // Background 401 recovery must see the new token before it can resume.
    replaceAuthSession(token(result), enabled)
    return result
  } finally {
    authChange = null
    finish()
  }
}

export const getAuthStatus = () => requestAuthObject<AuthStatus>('/status', [], false)
export const requestLoginToken = (password = '', code = '') =>
  requestAuth<string>('/login', [password, code], false)
export const setAccessPassword = (password: string, currentPassword = '', code = '') =>
  changeSession(
    () => requestAuth<string>('/setup', [password, currentPassword, code]),
    (token) => token,
    password !== '',
  )
export const beginTotpSetup = (password: string) =>
  requestAuthObject<TotpEnrollment>('/totp/setup', [password])
export const cancelTotpSetup = () => requestAuth<unknown>('/totp/cancel')
export const enableTotp = (code: string) =>
  changeSession(
    () => requestAuthObject<AuthRecovery>('/totp/enable', [code]),
    (result) => result.token,
    true,
  )
export const disableTotp = (password: string, code: string) =>
  changeSession(
    () => requestAuth<string>('/totp/disable', [password, code]),
    (token) => token,
    true,
  )
export const regenerateRecoveryCodes = (password: string, code: string) =>
  changeSession(
    () => requestAuthObject<AuthRecovery>('/totp/recovery', [password, code]),
    (result) => result.token,
    true,
  )

export const replaceAuthSession = (token: string, enabled: boolean) => {
  if (!token) throw new AuthError(500, 'The server did not return a session token')
  const settings = useAppSettingsStore()
  settings.sessionInfo.cacheToken = token
  settings.sessionInfo.authEnabled = enabled
  // App owns the login-view transition after LoginView emits authenticated.
  // Changing requireLogin here would unmount LoginView before that event can fire.
}
