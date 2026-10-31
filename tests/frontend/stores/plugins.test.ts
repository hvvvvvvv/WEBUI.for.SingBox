import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'

const request = vi.hoisted(() => vi.fn())
vi.mock('@/api/plugins', () => ({
  requestPlugins: request,
  PluginApiError: class extends Error {
    constructor(
      public status: number,
      message: string,
    ) {
      super(message)
    }
  },
}))
import { PluginApiError, usePluginsStore } from '@/stores/plugins'
import type { Plugin } from '@/types/plugin'

const plugin = (id = 'rename'): Plugin => ({
  id,
  name: id,
  description: '',
  version: 'v1.0.0',
  disabled: false,
  source: 'function onSubscribe(p) { return p }',
  triggers: ['on::subscribe'],
  configuration: {},
})

describe('global backend plugin store', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    request.mockReset()
  })
  it('persists source, configuration and revision through the global save endpoint', async () => {
    const store = usePluginsStore()
    request.mockResolvedValueOnce({ plugins: [], revision: 3 })
    await store.setupPlugins()
    const edited = {
      ...plugin(),
      source: 'function onSubscribe(p) { p[0].tag="new"; return p }',
      configuration: { prefix: 'Hi' },
    }
    request.mockResolvedValueOnce({ plugins: [edited], revision: 4 })
    await store.savePlugin(edited)
    expect(request).toHaveBeenLastCalledWith('/save', { plugin: edited, expected_revision: 3 })
    expect(store.plugins).toEqual([edited])
    expect(store.revision).toBe(4)
  })
  it('refreshes on conflict without mutating or retrying an editor draft', async () => {
    const store = usePluginsStore()
    const draft = plugin()
    request
      .mockRejectedValueOnce(new PluginApiError(409, 'conflict'))
      .mockResolvedValueOnce({ plugins: [{ ...draft, source: 'new remote source' }], revision: 9 })
    await expect(store.savePlugin(draft, 1)).rejects.toMatchObject({ status: 409 })
    expect(request).toHaveBeenCalledTimes(2)
    expect(store.revision).toBe(9)
    expect(store.plugins[0]!.source).toBe('new remote source')
    expect(draft.source).toContain('function onSubscribe')
  })
  it('does not let a delayed list response replace a newer successful mutation', async () => {
    const store = usePluginsStore()
    const read = Promise.withResolvers<unknown>()
    request.mockReturnValueOnce(read.promise)
    const pending = store.setupPlugins()
    request.mockResolvedValueOnce({ plugins: [plugin()], revision: 2 })
    await store.savePlugin(plugin(), 1)
    read.resolve({ plugins: [], revision: 1 })
    await pending
    expect(store.plugins).toHaveLength(1)
    expect(store.revision).toBe(2)
  })
  it('keeps catalog refresh separate from installation and source updates', async () => {
    const store = usePluginsStore()
    store.plugins = [plugin()]
    request.mockResolvedValueOnce({
      plugins: [{ ...plugin(), version: 'v2.0.0', source: 'new source' }],
      source: 'remote',
      repository: 'Ghostdehole/WEBUI.for.SingBox-Plugins',
    })
    await store.loadCatalog(true)
    expect(request).toHaveBeenLastCalledWith('/catalog?refresh=1')
    expect(store.plugins[0]!.version).toBe('v1.0.0')
    expect(store.availableUpdate(store.plugins[0]!)?.version).toBe('v2.0.0')
  })
  it('keeps a committed revision when a later GET read the pre-mutation snapshot', async () => {
    const store = usePluginsStore()
    const write = Promise.withResolvers<unknown>()
    const read = Promise.withResolvers<unknown>()
    request.mockReturnValueOnce(write.promise).mockReturnValueOnce(read.promise)
    const saving = store.savePlugin(plugin(), 1)
    const refreshing = store.setupPlugins()
    write.resolve({ plugins: [plugin()], revision: 2 })
    await saving
    read.resolve({ plugins: [], revision: 1 })
    await refreshing
    expect(store.plugins).toHaveLength(1)
    expect(store.revision).toBe(2)
  })
  it('does not replace a refreshed catalog with a delayed initial response', async () => {
    const store = usePluginsStore()
    const initial = Promise.withResolvers<unknown>()
    request.mockReturnValueOnce(initial.promise)
    const pending = store.loadCatalog()
    const refreshed = {
      plugins: [{ ...plugin(), version: 'v2.0.0', source: 'updated source' }],
      source: 'remote',
      repository: 'Ghostdehole/WEBUI.for.SingBox-Plugins',
    }
    request.mockResolvedValueOnce(refreshed)
    await store.loadCatalog(true)
    initial.resolve({
      plugins: [plugin()],
      source: 'bundled',
      repository: refreshed.repository,
      warning: 'outdated warning',
    })
    await pending
    expect(store.catalog).toEqual(refreshed)
    expect(store.catalogLoading).toBe(false)
  })
  it('restores the prior drag order when both reorder and reload are offline', async () => {
    const store = usePluginsStore()
    store.plugins = [plugin('b'), plugin('a')]
    request.mockRejectedValue(new Error('offline'))
    await expect(store.reorderPlugins(['b', 'a'], 5, ['a', 'b'])).rejects.toThrow('offline')
    expect(store.plugins.map((item) => item.id)).toEqual(['a', 'b'])
  })
})
