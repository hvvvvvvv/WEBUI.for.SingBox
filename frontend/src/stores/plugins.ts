import { defineStore } from 'pinia'
import { ref } from 'vue'

import { PluginApiError, requestPlugins } from '@/api/plugins'
import type {
  Plugin,
  PluginCatalog,
  PluginCatalogItem,
  PluginRunResult,
  PluginSnapshot,
} from '@/types/plugin'

export { PluginApiError }

const retiredCatalogIDs = new Set([
  'plugin-index-the-proxy-name',
  'plugin-modify-proxy-name',
  'plugin-beautiful-node-name',
  'plugin-duplicate-proxies-removal',
  'plugin-network-info',
  'plugin-geoip-search',
])

export const usePluginsStore = defineStore('plugins', () => {
  const plugins = ref<Plugin[]>([])
  const revision = ref(0)
  const loading = ref(false)
  const busy = ref(false)
  const catalog = ref<PluginCatalog>({ plugins: [], source: 'empty', repository: '' })
  const catalogLoading = ref(false)
  const catalogError = ref('')
  let sequence = 0
  let appliedSequence = 0
  let catalogSequence = 0
  let appliedCatalogSequence = 0

  const applySnapshot = (snapshot: PluginSnapshot, requestID: number) => {
    // A later GET can still read an older revision while a mutation is in flight.
    if (
      snapshot.revision < revision.value ||
      (snapshot.revision === revision.value && requestID < appliedSequence)
    )
      return
    appliedSequence = Math.max(appliedSequence, requestID)
    plugins.value = snapshot.plugins || []
    revision.value = snapshot.revision
  }
  const setupPlugins = async () => {
    const requestID = ++sequence
    loading.value = true
    try {
      applySnapshot(await requestPlugins<PluginSnapshot>(), requestID)
    } finally {
      loading.value = false
    }
  }
  const mutate = async (path: string, body: object) => {
    const requestID = ++sequence
    busy.value = true
    try {
      const snapshot = await requestPlugins<PluginSnapshot>(path, body)
      applySnapshot(snapshot, requestID)
      return snapshot
    } catch (error) {
      if (error instanceof PluginApiError && [409, 404].includes(error.status)) {
        await setupPlugins().catch(() => undefined)
      }
      throw error
    } finally {
      busy.value = false
    }
  }
  const savePlugin = (plugin: Plugin, expectedRevision = revision.value) =>
    mutate('/save', { plugin, expected_revision: expectedRevision })
  const deletePlugin = (id: string, expectedRevision = revision.value) =>
    mutate('/delete', { id, expected_revision: expectedRevision })
  const installPlugin = (id: string) =>
    mutate('/install', { id, expected_revision: revision.value })
  const updatePlugin = (id: string) => mutate('/update', { id, expected_revision: revision.value })
  const reorderPlugins = async (
    ids: string[],
    expectedRevision = revision.value,
    fallbackIDs: string[] = [],
  ) => {
    try {
      await mutate('/reorder', { ids, expected_revision: expectedRevision })
    } catch (error) {
      await setupPlugins().catch(() => {
        const byID = new Map(plugins.value.map((plugin) => [plugin.id, plugin]))
        const fallback = fallbackIDs.flatMap((id) => byID.get(id) || [])
        if (fallback.length === plugins.value.length) plugins.value = fallback
      })
      throw error
    }
  }
  const loadCatalog = async (refresh = false) => {
    const requestID = ++catalogSequence
    catalogLoading.value = true
    try {
      const result = await requestPlugins<PluginCatalog>(`/catalog${refresh ? '?refresh=1' : ''}`)
      if (requestID < appliedCatalogSequence) return
      appliedCatalogSequence = requestID
      catalog.value = {
        ...result,
        plugins: result.plugins.filter((item) => !retiredCatalogIDs.has(item.id)),
      }
      catalogError.value = ''
    } catch (error) {
      if (requestID >= appliedCatalogSequence) {
        appliedCatalogSequence = requestID
        catalogError.value = error instanceof Error ? error.message : String(error)
      }
      throw error
    } finally {
      if (requestID === catalogSequence) catalogLoading.value = false
    }
  }
  const runPlugin = (id: string, subscriptionID?: string) =>
    requestPlugins<PluginRunResult>('/run', {
      id,
      ...(subscriptionID === undefined ? {} : { subscription_id: subscriptionID }),
    })
  const getPluginById = (id: string) => plugins.value.find((plugin) => plugin.id === id)
  const availableUpdate = (plugin: Pick<Plugin, 'id' | 'version'>): PluginCatalogItem | undefined =>
    catalog.value.plugins.find((item) => item.id === plugin.id && item.version !== plugin.version)

  return {
    plugins,
    revision,
    loading,
    busy,
    catalog,
    catalogLoading,
    catalogError,
    setupPlugins,
    savePlugin,
    deletePlugin,
    installPlugin,
    updatePlugin,
    reorderPlugins,
    loadCatalog,
    runPlugin,
    getPluginById,
    availableUpdate,
  }
})
