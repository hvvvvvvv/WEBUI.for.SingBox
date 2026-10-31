import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

const auth = vi.hoisted(() => ({
  settings: { sessionInfo: { cacheToken: 'old' } },
  recover: vi.fn(),
}))
vi.mock('@/stores/appSettings', () => ({ useAppSettingsStore: () => auth.settings }))
vi.mock('@/bridge/http', () => ({ recoverAuthToken: auth.recover }))
import { requestPlugins } from '@/api/plugins'

describe('authenticated plugin REST API', () => {
  const fetchMock = vi.fn()
  beforeEach(() => {
    vi.resetAllMocks()
    auth.settings.sessionInfo.cacheToken = 'old'
    vi.stubGlobal('fetch', fetchMock)
  })
  afterEach(() => vi.unstubAllGlobals())
  it('recovers a 401 then sends the original JSON mutation once authorized', async () => {
    fetchMock
      .mockResolvedValueOnce({ ok: false, status: 401 })
      .mockResolvedValueOnce({ ok: true, json: async () => ({ plugins: [], revision: 2 }) })
    auth.recover.mockImplementation(async () => {
      auth.settings.sessionInfo.cacheToken = 'new'
      return true
    })
    await requestPlugins('/delete', { id: 'custom', expected_revision: 1 })
    expect(fetchMock.mock.calls[1]![0]).toBe('/api/plugins/delete')
    expect(fetchMock.mock.calls[1]![1]).toMatchObject({
      method: 'POST',
      headers: { Authorization: 'Bearer new' },
      body: '{"id":"custom","expected_revision":1}',
    })
  })
  it('preserves conflict status and backend detail for editor recovery', async () => {
    fetchMock.mockResolvedValue({
      ok: false,
      status: 409,
      text: async () => '{"error":"revision changed"}',
    })
    await expect(requestPlugins('/save', {})).rejects.toMatchObject({
      status: 409,
      message: 'revision changed',
    })
  })
})
