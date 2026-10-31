import { useAppSettingsStore } from '@/stores'
import {
  AuthError,
  getAuthStatus,
  requestLoginToken,
  replaceAuthSession,
  waitForAuthChange,
} from '@/api/auth'

let authRecoverPromise: Promise<boolean> | null = null

const buildApiError = (status: number, statusText: string) => {
  return new Error(`API error: ${status} ${statusText}`)
}

export const loadAuthToken = async () => {
  await waitForAuthChange()
  const appSettings = useAppSettingsStore()
  if (appSettings.sessionInfo.cacheToken == '') {
    const status = await getAuthStatus()
    appSettings.sessionInfo.authEnabled = status.enabled
    if (status.enabled) {
      appSettings.sessionInfo.requireLogin = true
      return false
    }
    const token = await requestLoginToken().catch((error) => {
      if (error instanceof AuthError && [200, 401, 403].includes(error.status)) return null
      throw error
    })
    if (token == '' || token == null) {
      appSettings.sessionInfo.authEnabled = true
      appSettings.sessionInfo.requireLogin = true
      return false
    }
    appSettings.sessionInfo.cacheToken = token
    appSettings.sessionInfo.authEnabled = false
    appSettings.sessionInfo.requireLogin = false
  }
  return true
}

export const loginAuthToken = async (secret: string, code = '') => {
  const token = await requestLoginToken(secret, code)
  if (!token) return false
  replaceAuthSession(token, secret !== '')
  return true
}

export const checkAuthToken = async () => {
  await waitForAuthChange()
  const appSettings = useAppSettingsStore()
  const token = appSettings.sessionInfo.cacheToken
  if (!token) return false

  try {
    const resp = await fetch('/api/auth/session', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        Authorization: `Bearer ${token}`,
      },
      body: JSON.stringify({ args: [] }),
    })
    return resp.status !== 401
  } catch {
    return true
  }
}

export const recoverAuthToken = async () => {
  if (authRecoverPromise) {
    return authRecoverPromise
  }

  authRecoverPromise = (async () => {
    await waitForAuthChange()
    const appSettings = useAppSettingsStore()
    // The failed request may have used a token that a security change just replaced.
    if (appSettings.sessionInfo.cacheToken && (await checkAuthToken())) return true
    appSettings.sessionInfo.cacheToken = ''
    const recovered = await loadAuthToken().catch(() => false)
    if (!recovered) {
      location.reload()
    }
    return recovered
  })()

  try {
    return await authRecoverPromise
  } finally {
    authRecoverPromise = null
  }
}

export const apiCall = async <T = any>(path: string, ...args: any[]): Promise<T> => {
  const appSettings = useAppSettingsStore()
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
  }

  if (appSettings.sessionInfo.cacheToken == '' && !(await recoverAuthToken())) {
    location.reload()
    throw buildApiError(401, 'Unauthorized')
  }

  headers.Authorization = `Bearer ${appSettings.sessionInfo.cacheToken}`

  let resp = await fetch(`/api${path}`, {
    method: 'POST',
    headers,
    body: JSON.stringify({ args }),
  })

  if (resp.status === 401) {
    if (!(await recoverAuthToken())) {
      location.reload()
      throw buildApiError(401, 'Unauthorized')
    }

    headers.Authorization = `Bearer ${appSettings.sessionInfo.cacheToken}`
    resp = await fetch(`/api${path}`, {
      method: 'POST',
      headers,
      body: JSON.stringify({ args }),
    })
  }

  if (!resp.ok) {
    if (resp.status === 401) {
      location.reload()
    }
    throw buildApiError(resp.status, resp.statusText)
  }

  return resp.json()
}
