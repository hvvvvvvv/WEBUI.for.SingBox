import { recoverAuthToken } from '@/bridge/http'
import { useAppSettingsStore } from '@/stores/appSettings'

export class PluginApiError extends Error {
  constructor(
    public status: number,
    message: string,
  ) {
    super(message)
  }
}

export const requestPlugins = async <T>(path = '', body?: unknown): Promise<T> => {
  const settings = useAppSettingsStore()
  if (!settings.sessionInfo.cacheToken && !(await recoverAuthToken())) {
    throw new PluginApiError(401, 'Unauthorized')
  }
  const controller = new AbortController()
  const timeout = setTimeout(() => controller.abort(), 30_000)
  try {
    const request = () =>
      fetch(`/api/plugins${path}`, {
        method: body === undefined ? 'GET' : 'POST',
        headers: {
          Authorization: `Bearer ${settings.sessionInfo.cacheToken}`,
          ...(body === undefined ? {} : { 'Content-Type': 'application/json' }),
        },
        ...(body === undefined ? {} : { body: JSON.stringify(body) }),
        signal: controller.signal,
        cache: 'no-store',
      })
    let response = await request()
    if (response.status === 401 && (await recoverAuthToken())) response = await request()
    if (!response.ok) {
      const text = await response.text()
      let detail = text
      try {
        const error = JSON.parse(text)
        detail = error.message || error.error || text
      } catch {
        /* Plain-text API errors are also supported. */
      }
      throw new PluginApiError(
        response.status,
        String(detail || `HTTP ${response.status}`).slice(0, 1000),
      )
    }
    return (await response.json()) as T
  } finally {
    clearTimeout(timeout)
  }
}
