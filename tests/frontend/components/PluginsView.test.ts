// @vitest-environment jsdom
import { createApp, defineComponent, h, KeepAlive, nextTick, ref, type App } from 'vue'
import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { Plugin, PluginCatalog } from '@/types/plugin'

const mocks = vi.hoisted(() => ({
  request: vi.fn(),
  error: vi.fn(),
  success: vi.fn(),
  warn: vi.fn(),
}))
vi.mock('@/api/plugins', () => ({
  requestPlugins: mocks.request,
  PluginApiError: class extends Error {},
}))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/stores/appSettings', () => ({
  useAppSettingsStore: () => ({ app: { pluginsView: 'grid' } }),
}))
vi.mock('@/utils', () => ({
  confirmDelete: vi.fn(),
  message: { error: mocks.error, success: mocks.success, warn: mocks.warn },
}))
vi.mock('@/views/PluginsView/components/PluginEditor.vue', () => ({
  default: { render: () => null },
}))
vi.mock('@/views/PluginsView/components/PluginRun.vue', () => ({ default: { render: () => null } }))
vi.mock('@/components/Modal', async () => {
  const { defineComponent, h, ref, shallowRef } = await import('vue')
  return {
    useModal: () => {
      const content = shallowRef<any>(null)
      const visible = ref(false)
      const api = {
        setProps() {
          return api
        },
        setContent(component: any) {
          content.value = component
          return api
        },
        open() {
          visible.value = true
        },
      }
      return [
        defineComponent({
          setup: () => () =>
            visible.value && content.value
              ? h('div', { role: 'dialog' }, [h(content.value)])
              : null,
        }),
        api,
      ]
    },
  }
})
import { usePluginsStore } from '@/stores/plugins'
import PluginsView from '@/views/PluginsView/index.vue'

const installed: Plugin = {
  id: 'installed',
  name: 'Installed plugin',
  description: '',
  version: '1',
  source: 'function onGenerate(c) { return c }',
  disabled: false,
  triggers: ['on::generate'],
  configuration: {},
}
const catalog = (source: PluginCatalog['source'] = 'remote', warning?: string): PluginCatalog => ({
  source,
  repository: 'owner/plugins',
  ...(warning ? { warning } : {}),
  plugins:
    source === 'empty'
      ? []
      : [
          {
            id: installed.id,
            name: installed.name,
            description: '',
            version: '2',
            triggers: ['on::generate'],
            configuration: {},
            path: 'plugins/installed.js',
            sha256: 'a'.repeat(64),
          },
        ],
})

describe('installed plugins page requests catalog only on user action', () => {
  let app: App | undefined
  let host: HTMLDivElement
  let store: ReturnType<typeof usePluginsStore>
  let pinia: ReturnType<typeof createPinia>
  const active = ref(true)
  const settle = async () => {
    await new Promise((resolve) => setTimeout(resolve, 0))
    await nextTick()
  }
  beforeEach(() => {
    vi.resetAllMocks()
    pinia = createPinia()
    setActivePinia(pinia)
    store = usePluginsStore()
    active.value = true
    mocks.request.mockResolvedValueOnce({ plugins: [installed], revision: 1 })
    host = document.createElement('div')
    document.body.append(host)
  })
  afterEach(() => {
    app?.unmount()
    app = undefined
    host.remove()
  })
  const mount = async () => {
    app = createApp({
      render: () => h(KeepAlive, null, { default: () => (active.value ? h(PluginsView) : null) }),
    })
    app.use(pinia)
    app.component(
      'Button',
      defineComponent({
        props: { disabled: Boolean, loading: Boolean },
        setup:
          (props, { slots }) =>
          () =>
            h('button', { disabled: props.disabled || props.loading }, slots.default?.()),
      }),
    )
    app.component(
      'Card',
      defineComponent({
        props: { title: String },
        setup:
          (props, { slots }) =>
          () =>
            h('section', [
              h('h3', props.title),
              slots['title-prefix']?.(),
              slots.extra?.(),
              slots.default?.(),
            ]),
      }),
    )
    app.component(
      'Dropdown',
      defineComponent({
        setup:
          (_, { slots }) =>
          () =>
            h('div', slots.default?.()),
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
    app.component(
      'Empty',
      defineComponent({
        setup:
          (_, { slots }) =>
          () =>
            h('div', slots.description?.()),
      }),
    )
    app.component('Radio', { render: () => null })
    app.component('Input', { render: () => h('input') })
    for (const directive of ['menu', 'draggable', 'tips']) app.directive(directive, {})
    app.mount(host)
    await settle()
  }
  const button = (text: string) =>
    [...host.querySelectorAll('button')].find((item) => item.textContent?.trim() === text)!

  it('loads only installed plugins on first activation and reactivation', async () => {
    await mount()
    expect(mocks.request.mock.calls).toEqual([[]])
    expect(store.catalog.source).toBe('empty')
    expect(host.textContent).toContain(installed.name)
    active.value = false
    await settle()
    mocks.request.mockResolvedValueOnce({ plugins: [installed], revision: 1 })
    active.value = true
    await settle()
    expect(mocks.request.mock.calls).toEqual([[], []])
    expect(store.catalog.source).toBe('empty')
  })

  it('fetches catalog metadata only after opening the plugin hub', async () => {
    await mount()
    mocks.request.mockResolvedValueOnce(catalog())
    button('plugins.hub').click()
    await settle()
    expect(host.querySelector('[role=dialog]')).not.toBeNull()
    expect(mocks.request.mock.calls).toEqual([[], ['/catalog']])
    expect(store.plugins).toEqual([installed])
  })

  it('checks metadata only on click and reports success after a remote response', async () => {
    await mount()
    mocks.request.mockResolvedValueOnce(catalog())
    button('plugins.checkForUpdates').click()
    await settle()
    expect(mocks.request.mock.calls).toEqual([[], ['/catalog?refresh=1']])
    expect(mocks.success).toHaveBeenCalledExactlyOnceWith('plugins.updateSuccess')
    expect(mocks.warn).not.toHaveBeenCalled()
    expect(store.plugins).toEqual([installed])
  })

  it.each(['cache', 'empty'] as const)(
    'shows %s fallback warnings without reporting success',
    async (source) => {
      await mount()
      mocks.request.mockResolvedValueOnce(
        catalog(source, 'Catalog download failed; using available cache'),
      )
      button('plugins.checkForUpdates').click()
      await settle()
      expect(mocks.success).not.toHaveBeenCalled()
      expect(mocks.warn).toHaveBeenCalledExactlyOnceWith(
        'Catalog download failed; using available cache',
      )
      expect(host.textContent).toContain('Catalog download failed; using available cache')
      expect(store.plugins).toEqual([installed])
    },
  )

  it('reports an empty or failed catalog request without a success message', async () => {
    await mount()
    mocks.request.mockResolvedValueOnce(catalog('empty'))
    button('plugins.checkForUpdates').click()
    await settle()
    expect(mocks.warn).toHaveBeenCalledExactlyOnceWith('plugins.catalogUnavailable')
    mocks.request.mockRejectedValueOnce(new Error('API offline'))
    button('plugins.checkForUpdates').click()
    await settle()
    expect(mocks.error).toHaveBeenCalledExactlyOnceWith('API offline')
    expect(mocks.success).not.toHaveBeenCalled()
  })
})
