import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import type { PluginCatalog } from '@/types/plugin'

const auth = vi.hoisted(() => ({
  settings: { sessionInfo: { cacheToken: 'old' } },
  recover: vi.fn(),
}))
vi.mock('@/stores/appSettings', () => ({ useAppSettingsStore: () => auth.settings }))
vi.mock('@/bridge/http', () => ({ recoverAuthToken: auth.recover }))
import { requestPlugins } from '@/api/plugins'
import { usePluginsStore } from '@/stores/plugins'

describe('authenticated plugin REST API', () => {
  const fetchMock = vi.fn()
  beforeEach(() => {
    vi.resetAllMocks()
    auth.settings.sessionInfo.cacheToken = 'old'
    setActivePinia(createPinia())
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
  it('uses GET for catalog metadata and POST with only the selected ID for explicit installation', async () => {
    const catalog: PluginCatalog = {
      source: 'remote',
      repository: 'owner/plugins',
      plugins: [
        {
          id: 'selected',
          name: 'Selected plugin',
          description: '',
          version: '1',
          triggers: ['on::subscribe'],
          configuration: null,
          path: 'plugins/selected.js',
          sha256: 'a'.repeat(64),
        },
      ],
    }
    const store = usePluginsStore()
    fetchMock.mockResolvedValueOnce({ ok: true, json: async () => catalog })
    await store.loadCatalog()
    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(fetchMock.mock.calls[0]![0]).toBe('/api/plugins/catalog')
    expect(fetchMock.mock.calls[0]![1]).toMatchObject({ method: 'GET' })
    expect(fetchMock.mock.calls[0]![1]).not.toHaveProperty('body')
    expect(store.plugins).toEqual([])

    fetchMock.mockResolvedValueOnce({ ok: true, json: async () => catalog })
    await store.loadCatalog(true)
    expect(fetchMock).toHaveBeenCalledTimes(2)
    expect(fetchMock.mock.calls[1]![0]).toBe('/api/plugins/catalog?refresh=1')
    expect(fetchMock.mock.calls[1]![1]).toMatchObject({ method: 'GET' })
    expect(fetchMock.mock.calls[1]![1]).not.toHaveProperty('body')
    expect(store.plugins).toEqual([])

    fetchMock.mockResolvedValueOnce({ ok: true, json: async () => ({ plugins: [], revision: 1 }) })
    await store.installPlugin('selected')
    expect(fetchMock).toHaveBeenCalledTimes(3)
    expect(fetchMock.mock.calls[2]![0]).toBe('/api/plugins/install')
    expect(fetchMock.mock.calls[2]![1]).toMatchObject({
      method: 'POST',
      body: JSON.stringify({ id: 'selected', expected_revision: 0 }),
    })
  })
})
