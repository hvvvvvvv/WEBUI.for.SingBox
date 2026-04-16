let authToken = ''

const AUTH_TOKEN_KEY = 'auth_token'
export const AUTH_REQUIRED_EVENT = 'app:auth-required'

let authRequiredDispatched = false

type AuthResult = {
  flag: boolean
  data: string
}

const buildApiError = (status: number, statusText: string) => {
  return new Error(`API error: ${status} ${statusText}`)
}

const requestAuthToken = async (secret = '') => {
  const resp = await fetch('/api/auth/login', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({ args: [secret] }),
  })

  if (!resp.ok) {
    const pending = handleUnauthorizedResponse(resp.status)
    if (pending) return pending
    throw buildApiError(resp.status, resp.statusText)
  }

  const result = (await resp.json()) as AuthResult
  if (!result.flag) {
    throw new Error(result.data || 'Authentication failed')
  }

  return result.data || ''
}

const dispatchAuthRequired = () => {
  clearAuthToken()
  if (authRequiredDispatched) {
    return
  }
  authRequiredDispatched = true
  window.dispatchEvent(new Event(AUTH_REQUIRED_EVENT))
}

export const handleUnauthorizedResponse = (status: number) => {
  if (status !== 401) {
    return null
  }

  dispatchAuthRequired()
  return new Promise<never>(() => {})
}

export const setAuthToken = (token: string) => {
  authToken = token
  authRequiredDispatched = false
  if (token) {
    localStorage.setItem(AUTH_TOKEN_KEY, token)
  } else {
    localStorage.removeItem(AUTH_TOKEN_KEY)
  }
}

export const getAuthToken = () => authToken

export const loadAuthToken = async () => {
  const cachedToken = localStorage.getItem(AUTH_TOKEN_KEY) || ''

  if (cachedToken) {
    setAuthToken(cachedToken)
    return cachedToken
  }

  clearAuthToken()

  try {
    const token = await requestAuthToken('')
    setAuthToken(token)
    return token
  } catch {
    clearAuthToken()
    return ''
  }
}

export const clearAuthToken = () => {
  authToken = ''
  localStorage.removeItem(AUTH_TOKEN_KEY)
}

export const apiCall = async <T = any>(path: string, ...args: any[]): Promise<T> => {
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
  }

  if (authToken) {
    headers.Authorization = `Bearer ${authToken}`
  }

  const resp = await fetch(`/api${path}`, {
    method: 'POST',
    headers,
    body: JSON.stringify({ args }),
  })

  if (!resp.ok) {
    const pending = handleUnauthorizedResponse(resp.status)
    if (pending) return pending
    throw buildApiError(resp.status, resp.statusText)
  }

  return resp.json()
}
