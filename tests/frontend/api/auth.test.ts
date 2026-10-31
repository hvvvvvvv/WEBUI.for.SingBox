import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

const state = vi.hoisted(() => ({
  sessionInfo: { cacheToken: 'session', authEnabled: true, requireLogin: false },
}))
vi.mock('@/stores/appSettings', () => ({ useAppSettingsStore: () => state }))
vi.mock('@/stores', () => ({ useAppSettingsStore: () => state }))
import {
  beginTotpSetup,
  cancelTotpSetup,
  disableTotp,
  enableTotp,
  getAuthStatus,
  regenerateRecoveryCodes,
  replaceAuthSession,
  requestLoginToken,
  setAccessPassword,
} from '@/api/auth'
import { loadAuthToken, loginAuthToken, recoverAuthToken } from '@/bridge/http'

describe('access and TOTP API', () => {
  const fetchMock = vi.fn()
  const response = (data: unknown, flag = true, status = 200) => ({
    ok: status < 400,
    status,
    statusText: 'Unauthorized',
    json: async () => ({ flag, data }),
  })
  beforeEach(() => {
    fetchMock.mockReset()
    state.sessionInfo = { cacheToken: 'session', authEnabled: true, requireLogin: false }
    vi.stubGlobal('fetch', fetchMock)
  })
  afterEach(() => {
    vi.unstubAllGlobals()
    vi.useRealTimers()
  })

  it('fetches public status without an authorization header and decodes the legacy string envelope', async () => {
    fetchMock.mockResolvedValueOnce(response(JSON.stringify({ enabled: true, totp_enabled: true })))
    await expect(getAuthStatus()).resolves.toEqual({ enabled: true, totp_enabled: true })
    expect(fetchMock).toHaveBeenCalledExactlyOnceWith(
      '/api/auth/status',
      expect.objectContaining({
        method: 'POST',
        body: '{"args":[]}',
        cache: 'no-store',
        headers: { 'Content-Type': 'application/json' },
      }),
    )
  })
  it('sends password and a raw OTP or recovery code only in the request body', async () => {
    fetchMock.mockResolvedValueOnce(response('rotated-token'))
    await expect(loginAuthToken(' password with spaces ', 'ABCD-EFGH')).resolves.toBe(true)
    expect(fetchMock.mock.calls[0]![0]).toBe('/api/auth/login')
    expect(fetchMock.mock.calls[0]![1]).toMatchObject({
      body: JSON.stringify({ args: [' password with spaces ', 'ABCD-EFGH'] }),
      headers: { 'Content-Type': 'application/json' },
    })
    expect(state.sessionInfo).toEqual({
      cacheToken: 'rotated-token',
      authEnabled: true,
      requireLogin: false,
    })
  })
  it('preserves the login view until App receives the authenticated event', async () => {
    state.sessionInfo = { cacheToken: '', authEnabled: true, requireLogin: true }
    fetchMock.mockResolvedValueOnce(response('signed-in'))
    await expect(loginAuthToken('password')).resolves.toBe(true)
    expect(state.sessionInfo).toEqual({
      cacheToken: 'signed-in',
      authEnabled: true,
      requireLogin: true,
    })
  })
  it('never retries a rejected one-time code or changes the current session', async () => {
    fetchMock.mockResolvedValueOnce(response('invalid code', false))
    await expect(enableTotp('123456')).rejects.toMatchObject({ message: 'invalid code' })
    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(state.sessionInfo.cacheToken).toBe('session')
    fetchMock.mockResolvedValueOnce(response('expired session', false, 401))
    await expect(disableTotp('password', 'RECOVERY')).rejects.toMatchObject({ status: 401 })
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })
  it('recognizes rate limiting in a legacy HTTP 200 failure envelope', async () => {
    fetchMock.mockResolvedValueOnce(response('Too many failed attempts, try again later', false))
    await expect(requestLoginToken('password', '123456')).rejects.toMatchObject({
      kind: 'rate_limited',
    })
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })
  it('waits for token rotation before background 401 recovery and preserves the new session', async () => {
    const mutation = Promise.withResolvers<ReturnType<typeof response>>()
    fetchMock.mockReturnValueOnce(mutation.promise)
    const changing = enableTotp('123456')
    const recovering = recoverAuthToken()
    await Promise.resolve()
    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(state.sessionInfo.cacheToken).toBe('session')
    fetchMock.mockResolvedValueOnce(response('Valid'))
    mutation.resolve(response(JSON.stringify({ token: 'new-token', recovery_codes: ['KEEP-ME'] })))
    await expect(changing).resolves.toEqual({ token: 'new-token', recovery_codes: ['KEEP-ME'] })
    await expect(recovering).resolves.toBe(true)
    expect(fetchMock.mock.calls[1]![0]).toBe('/api/auth/session')
    expect(fetchMock.mock.calls[1]![1].headers.Authorization).toBe('Bearer new-token')
    expect(state.sessionInfo.cacheToken).toBe('new-token')
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })
  it('rejects concurrent security changes without sending or replaying another OTP', async () => {
    const mutation = Promise.withResolvers<ReturnType<typeof response>>()
    fetchMock.mockReturnValueOnce(mutation.promise)
    const first = enableTotp('123456')
    await expect(disableTotp('password', '654321')).rejects.toMatchObject({ status: 409 })
    expect(fetchMock).toHaveBeenCalledTimes(1)
    mutation.resolve(response(JSON.stringify({ token: 'rotated', recovery_codes: ['RECOVERY'] })))
    await first
  })
  it('uses the current bearer token for setup, confirmation, cancellation and recovery operations', async () => {
    const pending = {
      secret: 'PRIVATEKEY',
      otpauth_uri: 'otpauth://totp/WebUI?secret=PRIVATEKEY',
      expires_at: '2030-01-01T00:00:00Z',
    }
    fetchMock.mockResolvedValueOnce(response(JSON.stringify(pending)))
    await expect(beginTotpSetup('current')).resolves.toEqual(pending)
    expect(fetchMock.mock.calls[0]![1]).toMatchObject({
      headers: { Authorization: 'Bearer session' },
      body: '{"args":["current"]}',
    })
    fetchMock.mockResolvedValueOnce(response(''))
    await cancelTotpSetup()
    expect(fetchMock.mock.calls[1]![0]).toBe('/api/auth/totp/cancel')
    const result = { token: 'new', recovery_codes: ['AAA-BBB', 'CCC-DDD'] }
    fetchMock.mockResolvedValueOnce(response(JSON.stringify(result)))
    await expect(regenerateRecoveryCodes('current', 'AAA-BBB')).resolves.toEqual(result)
    expect(fetchMock.mock.calls[2]![1].body).toBe('{"args":["current","AAA-BBB"]}')
  })
  it('rotates only session fields after an explicit password change', async () => {
    fetchMock.mockResolvedValueOnce(response('password-token'))
    const token = await setAccessPassword('new long password', 'old', '111111')
    expect(fetchMock.mock.calls[0]![1].body).toBe('{"args":["new long password","old","111111"]}')
    replaceAuthSession(token, true)
    expect(state.sessionInfo).toEqual({
      cacheToken: 'password-token',
      authEnabled: true,
      requireLogin: false,
    })
  })
  it('preserves the bootstrap login path and asks for login when password authentication is enabled', async () => {
    state.sessionInfo.cacheToken = ''
    fetchMock.mockResolvedValueOnce(response(JSON.stringify({ enabled: true, totp_enabled: true })))
    await expect(loadAuthToken()).resolves.toBe(false)
    expect(state.sessionInfo.requireLogin).toBe(true)
    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(fetchMock.mock.calls[0]![0]).toBe('/api/auth/status')
    fetchMock.mockResolvedValueOnce(
      response(JSON.stringify({ enabled: false, totp_enabled: false })),
    )
    fetchMock.mockResolvedValueOnce(response('bootstrap'))
    await expect(loadAuthToken()).resolves.toBe(true)
    expect(state.sessionInfo).toEqual({
      cacheToken: 'bootstrap',
      authEnabled: false,
      requireLogin: false,
    })
  })
  it('rejects malformed structured responses and bounds stalled requests', async () => {
    fetchMock.mockResolvedValueOnce(response('not-json'))
    await expect(getAuthStatus()).rejects.toThrow('Invalid authentication response')
    vi.useFakeTimers()
    fetchMock.mockImplementationOnce(
      (_url, init: RequestInit) =>
        new Promise((_resolve, reject) => {
          init.signal!.addEventListener('abort', () =>
            reject(new DOMException('Aborted', 'AbortError')),
          )
        }),
    )
    const pending = requestLoginToken('secret')
    const rejected = expect(pending).rejects.toMatchObject({ name: 'AbortError' })
    await vi.advanceTimersByTimeAsync(30_000)
    await rejected
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })
})
