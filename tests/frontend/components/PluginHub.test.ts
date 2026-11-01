// @vitest-environment jsdom
import { createApp, defineComponent, h, nextTick, type App } from 'vue'
import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { Plugin, PluginCatalog, PluginCatalogItem } from '@/types/plugin'

const mocks = vi.hoisted(() => ({ request: vi.fn(), error: vi.fn() }))
vi.mock('@/api/plugins', () => ({
  requestPlugins: mocks.request,
  PluginApiError: class extends Error {},
}))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/utils', () => ({ message: { error: mocks.error } }))
import { usePluginsStore } from '@/stores/plugins'
import PluginHub from '@/views/PluginsView/components/PluginHub.vue'

const plugin: PluginCatalogItem = {
  id: 'rename',
  name: 'Rename nodes',
  description: 'Rename subscription nodes',
  version: '1.0.0',
  triggers: ['on::subscribe'],
  configuration: null,
  path: 'plugins/rename.js',
  sha256: 'a'.repeat(64),
}
const remote = (): PluginCatalog => ({
  plugins: [plugin],
  source: 'remote',
  repository: 'owner/plugins',
})

describe('independent plugin repository catalog', () => {
  let app: App | undefined
  let host: HTMLDivElement
  let store: ReturnType<typeof usePluginsStore>
  let pinia: ReturnType<typeof createPinia>
  beforeEach(() => {
    mocks.request.mockReset()
    mocks.error.mockReset()
    pinia = createPinia()
    setActivePinia(pinia)
    store = usePluginsStore()
    host = document.createElement('div')
    document.body.append(host)
  })
  afterEach(() => {
    app?.unmount()
    app = undefined
    host.remove()
  })
  const settle = async () => {
    await new Promise((resolve) => setTimeout(resolve, 0))
    await nextTick()
  }
  const mount = async () => {
    app = createApp(PluginHub)
    app.use(pinia)
    app.component(
      'Button',
      defineComponent({
        props: { loading: Boolean, disabled: Boolean },
        setup:
          (props, { slots }) =>
          () =>
            h(
              'button',
              {
                disabled: props.loading || props.disabled,
              },
              slots.default?.(),
            ),
      }),
    )
    app.component(
      'Input',
      defineComponent({
        props: { modelValue: String },
        emits: ['update:modelValue'],
        setup:
          (props, { emit, slots }) =>
          () =>
            h('div', [slots.prefix?.(), h('input', {
              value: props.modelValue,
              onInput: (event: Event) =>
                emit('update:modelValue', (event.target as HTMLInputElement).value),
            }), slots.suffix?.()]),
      }),
    )
    app.component(
      'Card',
      defineComponent({
        props: { title: String },
        setup:
          (props, { slots }) =>
          () =>
            h('section', [h('h3', props.title), slots.extra?.(), slots.default?.()]),
      }),
    )
    app.component(
      'Tag',
      defineComponent({
        setup:
          (_, { slots }) =>
          () =>
            h('span', slots.default?.()),
      }),
    )
    app.component('Icon', defineComponent({ setup: () => () => h('span') }))
    app.component('Empty', defineComponent({ setup: (_, {slots}) => () => h('div', slots.description?.()) }))
    app.directive('tips', {})
    app.mount(host)
    await settle()
  }
  const button = (text: string, within: ParentNode = host) =>
    [...within.querySelectorAll('button')].find((item) => item.textContent?.trim() === text || item.getAttribute('aria-label') === text)!

  it('does not offer retired cached packages and preserves installed user scripts', async () => {
    const retired = { ...plugin, id: 'plugin-modify-proxy-name', name: 'Previously installed rename' }
    mocks.request.mockResolvedValueOnce({ ...remote(), plugins: [plugin, retired] })
    store.plugins = [{ ...retired, source: 'function onSubscribe(p){return p}', configuration: {}, disabled: true, version: '0.9' }]
    await mount()
    expect(host.querySelectorAll('section')).toHaveLength(1)
    expect(host.textContent).not.toContain(retired.name)
    expect(host.textContent).not.toContain('plugins.showLegacy')
    expect(store.availableUpdate(store.plugins[0]!)).toBeUndefined()
    expect(store.plugins[0]?.source).toBe('function onSubscribe(p){return p}')
    expect(store.plugins[0]?.version).toBe('0.9')
    expect(mocks.request.mock.calls).toEqual([['/catalog']])
  })

  it('keeps a historical enabled plugin visible as an install conflict without disabling it', async () => {
    const retiredID = 'plugin-modify-proxy-name'
    mocks.request.mockResolvedValueOnce({ ...remote(), plugins: [{ ...plugin, conflicts: [retiredID] }] })
    store.plugins = [{ ...plugin, id: retiredID, name: 'My installed script', source: 'function onSubscribe(p){return p}', configuration: {}, disabled: false }]
    await mount()
    expect(button('plugins.install').disabled).toBe(true)
    expect(host.textContent).toContain('My installed script')
    expect(store.plugins[0]?.disabled).toBe(false)
    expect(mocks.request.mock.calls).toEqual([['/catalog']])
  })

  it('fetches the first catalog without refresh and shows loading until the repository responds', async () => {
    const first = Promise.withResolvers<PluginCatalog>()
    mocks.request.mockReturnValueOnce(first.promise)
    await mount()
    expect(mocks.request).toHaveBeenCalledExactlyOnceWith('/catalog')
    expect(store.catalog.source).toBe('empty')
    expect(host.querySelector('.plugin-hub')?.getAttribute('aria-busy')).toBe('true')
    expect(host.textContent).toContain('plugins.catalogLoading')
    expect(host.textContent).not.toContain('plugins.catalogUnavailable')
    expect(host.textContent).not.toContain('plugins.catalogEmpty')
    first.resolve(remote())
    await settle()
    expect(host.textContent).toContain(plugin.name)
    expect(host.textContent).not.toContain('plugins.catalogLoading')
    expect(host.querySelector('[role=alert]')).toBeNull()
    expect(mocks.request.mock.calls).toEqual([['/catalog']])
    expect(store.plugins).toEqual([])
    expect(store.catalog.plugins[0]).not.toHaveProperty('source')
  })

  it('refreshes only metadata and installs only the plugin whose button is clicked', async () => {
    const second: PluginCatalogItem = {
      ...plugin,
      id: 'dedupe',
      name: 'Remove duplicates',
      path: 'plugins/dedupe.js',
    }
    const list: PluginCatalog = { ...remote(), plugins: [plugin, second] }
    mocks.request.mockResolvedValueOnce(list)
    await mount()
    expect(mocks.request.mock.calls).toEqual([['/catalog']])
    expect(store.plugins).toEqual([])

    mocks.request.mockResolvedValueOnce(list)
    button('plugins.checkForUpdates').click()
    await settle()
    expect(mocks.request.mock.calls).toEqual([['/catalog'], ['/catalog?refresh=1']])
    expect(store.plugins).toEqual([])

    const installed = (entry: PluginCatalogItem): Plugin => ({
      id: entry.id,
      name: entry.name,
      description: entry.description,
      version: entry.version,
      triggers: entry.triggers,
      disabled: false,
      source: 'function onSubscribe(p) { return p }',
      configuration: {},
    })
    const selected = Promise.withResolvers<{ plugins: Plugin[]; revision: number }>()
    mocks.request.mockReturnValueOnce(selected.promise)
    button('plugins.install', host.querySelectorAll('section')[1]!).click()
    await settle()
    expect(mocks.request).toHaveBeenLastCalledWith('/install', {
      id: second.id,
      expected_revision: 0,
    })
    expect(mocks.request).toHaveBeenCalledTimes(3)
    expect(store.plugins).toEqual([])
    expect(button('plugins.install', host.querySelectorAll('section')[0]!).disabled).toBe(true)
    selected.resolve({ plugins: [installed(second)], revision: 1 })
    await settle()
    expect(store.plugins.map((item) => item.id)).toEqual([second.id])
    expect(button('plugins.installed', host.querySelectorAll('section')[1]!).disabled).toBe(true)

    mocks.request.mockResolvedValueOnce({
      plugins: [installed(second), installed(plugin)],
      revision: 2,
    })
    button('plugins.install', host.querySelectorAll('section')[0]!).click()
    await settle()
    expect(mocks.request).toHaveBeenLastCalledWith('/install', {
      id: plugin.id,
      expected_revision: 1,
    })
    expect(mocks.request.mock.calls.map(([path]) => path)).toEqual([
      '/catalog',
      '/catalog?refresh=1',
      '/install',
      '/install',
    ])
    expect(store.plugins.map((item) => item.id)).toEqual([second.id, plugin.id])
  })

  it('keeps a no-cache download warning visible through retry and clears it on success', async () => {
    mocks.request.mockResolvedValueOnce({
      plugins: [],
      source: 'empty',
      repository: 'owner/plugins',
      warning: 'Repository is offline',
    })
    await mount()
    expect(host.querySelector('[role=alert]')?.textContent).toContain('Repository is offline')
    expect(host.textContent).toContain('plugins.catalogUnavailable')
    expect(host.textContent).not.toContain('plugins.catalogEmpty')
    const retry = Promise.withResolvers<PluginCatalog>()
    mocks.request.mockReturnValueOnce(retry.promise)
    button('plugins.retry').click()
    await settle()
    expect(mocks.request).toHaveBeenLastCalledWith('/catalog?refresh=1')
    expect(host.textContent).toContain('plugins.catalogLoading')
    expect(host.querySelector('[role=alert]')?.textContent).toContain('Repository is offline')
    retry.resolve(remote())
    await settle()
    expect(host.textContent).toContain(plugin.name)
    expect(host.textContent).not.toContain('plugins.catalogUnavailable')
    expect(host.querySelector('[role=alert]')).toBeNull()
  })

  it('persists a transport error in the empty view and provides a working retry', async () => {
    mocks.request.mockRejectedValueOnce(new Error('API unreachable'))
    await mount()
    expect(mocks.error).toHaveBeenCalledWith('API unreachable')
    expect(host.querySelector('[role=alert]')?.textContent).toContain('API unreachable')
    expect(host.textContent).toContain('plugins.catalogUnavailable')
    mocks.request.mockResolvedValueOnce(remote())
    button('plugins.retry').click()
    await settle()
    expect(mocks.request).toHaveBeenLastCalledWith('/catalog?refresh=1')
    expect(host.textContent).toContain(plugin.name)
    expect(host.querySelector('[role=alert]')).toBeNull()
  })

  it('displays an existing cache without fetching and preserves it after an update failure', async () => {
    store.catalog = { ...remote(), source: 'cache', warning: 'Using last downloaded catalog' }
    await mount()
    expect(mocks.request).not.toHaveBeenCalled()
    expect(host.textContent).toContain('plugins.catalogCached')
    mocks.request.mockRejectedValueOnce(new Error('Network disconnected'))
    button('plugins.checkForUpdates').click()
    await settle()
    expect(host.textContent).toContain(plugin.name)
    expect(
      [...host.querySelectorAll('[role=alert]')].map((item) => item.textContent?.trim()),
    ).toEqual(['Using last downloaded catalog', 'Network disconnected'])
    expect(host.textContent).not.toContain('plugins.catalogUnavailable')
    expect(host.textContent).not.toContain('plugins.catalogEmpty')
  })

  it('distinguishes an empty search result from an unavailable catalog', async () => {
    store.catalog = remote()
    await mount()
    const input = host.querySelector('input')!
    input.value = 'no such plugin'
    input.dispatchEvent(new InputEvent('input', { bubbles: true }))
    await nextTick()
    expect(host.textContent).toContain('plugins.catalogNoResults')
    expect(host.textContent).not.toContain('plugins.catalogUnavailable')
  })
})
