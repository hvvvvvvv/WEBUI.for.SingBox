import { useAppSettingsStore } from '@/stores'


type AuthResult = {
  flag: boolean
  data: string
}

let authRecoverPromise: Promise<boolean> | null = null

const buildApiError = (status: number, statusText: string) => {
  return new Error(`API error: ${status} ${statusText}`)
}

export const requestAuthToken = async (secret = '') => {
  const resp = await fetch('/api/auth/login', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({ args: [secret] }),
  })

  if (!resp.ok) {
    throw buildApiError(resp.status, resp.statusText)
  }

  const result = (await resp.json()) as AuthResult

  return result.flag ? result.data : null
}


export const loadAuthToken = async () => {
  const appSettings = useAppSettingsStore()
  if (appSettings.sessionInfo.cacheToken == "") {
    const token = await requestAuthToken()
    if (token == "" || token == null) {
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

export const loginAuthToken = async (secret: string) => {
  const appSettings = useAppSettingsStore()
  const token = await requestAuthToken(secret)
  if (!token) return false
  appSettings.sessionInfo.cacheToken = token
  appSettings.sessionInfo.authEnabled = true
  return true
}

// export const clearAuthToken = () => {
//   authToken = ''
//   localStorage.removeItem(AUTH_TOKEN_KEY)
// }

export const checkAuthToken = async () => {
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
    const appSettings = useAppSettingsStore()
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

  if (appSettings.sessionInfo.cacheToken == "" && !(await recoverAuthToken())) {
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
    if (!await recoverAuthToken()) {
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
