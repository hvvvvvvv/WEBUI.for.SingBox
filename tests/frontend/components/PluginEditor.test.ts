// @vitest-environment jsdom
import { createApp, defineComponent, h, nextTick, ref, type App } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { Plugin } from '@/types/plugin'

const mocks = vi.hoisted(() => ({
  save: vi.fn(),
  setup: vi.fn(),
  get: vi.fn(),
  message: vi.fn(),
  submit: vi.fn(),
}))
vi.mock('@/stores/plugins', () => ({
  PluginApiError: class extends Error {
    constructor(
      public status: number,
      message: string,
    ) {
      super(message)
    }
  },
  usePluginsStore: () => ({
    revision: 7,
    savePlugin: mocks.save,
    setupPlugins: mocks.setup,
    getPluginById: mocks.get,
  }),
}))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/utils', () => ({ message: { error: mocks.message } }))
import { PluginApiError } from '@/stores/plugins'
import PluginEditor from '@/views/PluginsView/components/PluginEditor.vue'

describe('plugin source editor persistence and conflicts', () => {
  let app: App
  let host: HTMLDivElement
  const initial: Plugin = {
    id: 'custom',
    name: 'Custom',
    description: '',
    version: '1.0.0',
    disabled: false,
    triggers: ['on::subscribe'],
    source: 'function onSubscribe(p) { return p }',
    configuration: {},
  }
  beforeEach(() => {
    vi.resetAllMocks()
    mocks.get.mockReturnValue(initial)
    mocks.save.mockResolvedValue({ plugins: [initial], revision: 8 })
    host = document.createElement('div')
    document.body.append(host)
  })
  afterEach(() => {
    app.unmount()
    host.remove()
  })
  const mount = async (mode: 'source' | 'configuration' = 'source') => {
    const editor = ref<any>()
    app = createApp({
      render: () =>
        h('div', [
          h(PluginEditor, { ref: editor, id: 'custom', mode }),
          h('footer', editor.value?.modalSlots.submit?.()),
        ]),
    })
    app.provide('submit', mocks.submit)
    app.provide('cancel', vi.fn())
    app.component('Icon', { render: () => null })
    for (const name of ['Input', 'Select', 'Switch']) {
      app.component(name, { render: () => null })
    }
    app.component(
      'Button',
      defineComponent({
        setup:
          (_, { slots }) =>
          () =>
            h('button', slots.default?.()),
      }),
    )
    app.component(
      'CodeViewer',
      defineComponent({
        props: ['modelValue'],
        setup(props, { expose }) {
          const area = ref<HTMLTextAreaElement>()
          expose({ getContent: () => area.value?.value ?? props.modelValue })
          return () => h('textarea', { ref: area, value: props.modelValue })
        },
      }),
    )
    app.mount(host)
    await nextTick()
    // Native user clicks occur in a later event-loop task than listener attachment.
    await new Promise((resolve) => setTimeout(resolve, 1))
  }
  const save = () => host.querySelector<HTMLElement>('footer [role=button]')!.click()
  it('reads the current editor document even before its debounced model update', async () => {
    await mount()
    host.querySelector('textarea')!.value =
      'function onSubscribe(p) { p[0].tag="edited"; return p }'
    save()
    await vi.waitFor(() => expect(mocks.save).toHaveBeenCalledOnce())
    expect(mocks.save.mock.calls[0]![0].source).toContain('tag="edited"')
    expect(mocks.save.mock.calls[0]![1]).toBe(7)
    expect(initial.source).not.toContain('edited')
    expect(mocks.submit).toHaveBeenCalledOnce()
  })
  it('keeps a local source draft and blocks blind overwrite after a revision conflict', async () => {
    await mount()
    host.querySelector('textarea')!.value = 'function onSubscribe(p) { return p.slice(1) }'
    mocks.save.mockRejectedValue(new PluginApiError(409, 'changed'))
    save()
    await vi.waitFor(() => expect(host.textContent).toContain('plugins.draftPreserved'))
    expect(host.querySelector('textarea')!.value).toContain('slice(1)')
    expect(host.querySelector('footer [role=button]')!.getAttribute('aria-disabled')).toBe('true')
    expect(mocks.submit).not.toHaveBeenCalled()
    expect(mocks.save).toHaveBeenCalledOnce()
  })
  it('rejects configuration arrays before sending them to the server', async () => {
    await mount('configuration')
    host.querySelector('textarea')!.value = '["not-an-object"]'
    save()
    await vi.waitFor(() =>
      expect(mocks.message).toHaveBeenCalledWith('plugins.invalidConfiguration'),
    )
    expect(mocks.save).not.toHaveBeenCalled()
  })
})
