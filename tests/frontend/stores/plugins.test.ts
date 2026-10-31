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
import type { Plugin, PluginCatalogItem } from '@/types/plugin'

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
const catalogItem = (id = 'rename'): PluginCatalogItem => ({
  id,
  name: id,
  description: '',
  version: 'v1.0.0',
  triggers: ['on::subscribe'],
  configuration: {},
  path: `plugins/${id}.js`,
  sha256: 'a'.repeat(64),
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
      plugins: [{ ...catalogItem(), version: 'v2.0.0' }],
      source: 'remote',
      repository: 'Ghostdehole/WEBUI.for.SingBox-Plugins',
    })
    await store.loadCatalog(true)
    expect(request).toHaveBeenLastCalledWith('/catalog?refresh=1')
    expect(store.plugins[0]!.version).toBe('v1.0.0')
    expect(store.plugins[0]!.source).toBe(plugin().source)
    expect(request.mock.calls).toEqual([['/catalog?refresh=1']])
    expect(store.availableUpdate(store.plugins[0]!)?.version).toBe('v2.0.0')
    expect(store.availableUpdate(store.plugins[0]!)).not.toHaveProperty('source')
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
      plugins: [{ ...catalogItem(), version: 'v2.0.0' }],
      source: 'remote',
      repository: 'Ghostdehole/WEBUI.for.SingBox-Plugins',
    }
    request.mockResolvedValueOnce(refreshed)
    await store.loadCatalog(true)
    initial.resolve({
      plugins: [catalogItem()],
      source: 'cache',
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
  it('starts empty and lets the first catalog request fetch the independent repository', async () => {
    const store = usePluginsStore()
    expect(store.catalog).toEqual({ plugins: [], source: 'empty', repository: '' })
    const initial = Promise.withResolvers<unknown>()
    request.mockReturnValueOnce(initial.promise)
    const loading = store.loadCatalog()
    expect(request).toHaveBeenCalledWith('/catalog')
    expect(store.catalogLoading).toBe(true)
    initial.resolve({ plugins: [catalogItem()], source: 'remote', repository: 'owner/plugins' })
    await loading
    expect(store.catalog.source).toBe('remote')
    expect(store.catalog.plugins).toHaveLength(1)
    expect(store.catalogLoading).toBe(false)
  })
  it('retains the cache and warning on transport failure and clears errors after a successful retry', async () => {
    const store = usePluginsStore()
    store.catalog = {
      plugins: [catalogItem()],
      source: 'cache',
      repository: 'owner/plugins',
      warning: 'cached warning',
    }
    request.mockRejectedValueOnce(new Error('network offline'))
    await expect(store.loadCatalog(true)).rejects.toThrow('network offline')
    expect(store.catalog.plugins).toHaveLength(1)
    expect(store.catalog.source).toBe('cache')
    expect(store.catalog.warning).toBe('cached warning')
    expect(store.catalogError).toBe('network offline')
    request.mockResolvedValueOnce({
      plugins: [catalogItem()],
      source: 'remote',
      repository: 'owner/plugins',
    })
    await store.loadCatalog(true)
    expect(store.catalogError).toBe('')
    expect(store.catalog.warning).toBeUndefined()
    expect(store.catalog.source).toBe('remote')
  })
  it('does not replace a newer success with an older catalog error', async () => {
    const store = usePluginsStore()
    const initial = Promise.withResolvers<unknown>()
    request.mockReturnValueOnce(initial.promise)
    const pending = store.loadCatalog()
    const rejected = expect(pending).rejects.toThrow('old request failed')
    request.mockResolvedValueOnce({
      plugins: [catalogItem()],
      source: 'remote',
      repository: 'owner/plugins',
    })
    await store.loadCatalog(true)
    initial.reject(new Error('old request failed'))
    await rejected
    expect(store.catalogError).toBe('')
    expect(store.catalog.plugins).toHaveLength(1)
  })
})
