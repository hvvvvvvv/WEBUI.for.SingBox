import { useAppSettingsStore } from '@/stores'


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

// export const clearAuthToken = () => {
//   authToken = ''
//   localStorage.removeItem(AUTH_TOKEN_KEY)
// }

export const apiCall = async <T = any>(path: string, ...args: any[]): Promise<T> => {
  const appSettings = useAppSettingsStore()
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
  }

  if (appSettings.sessionInfo.cacheToken == "" && !(await loadAuthToken())) {
    location.reload()
  }

  headers.Authorization = `Bearer ${appSettings.sessionInfo.cacheToken}`

  let resp = await fetch(`/api${path}`, {
    method: 'POST',
    headers,
    body: JSON.stringify({ args }),
  })

  if (resp.status === 401) {
    appSettings.sessionInfo.cacheToken = ''
    if (!await loadAuthToken()) {
      location.reload()
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
