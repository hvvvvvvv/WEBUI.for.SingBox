// @vitest-environment jsdom
import { createApp, defineComponent, h, nextTick, ref, type App } from 'vue'
import { EditorView } from '@codemirror/view'
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
vi.mock('@/utils', async () => ({
  ...(await import('@/utils/others')),
  message: { error: mocks.message },
}))
vi.mock('@/lang', async () => ({
  default: { global: { t: (key: string) => key, locale: (await import('vue')).ref('en') } },
}))
vi.mock('@/stores', () => ({ useAppSettingsStore: () => ({ themeMode: 'light' }) }))
vi.mock('@/components/Modal/index.vue', () => ({ IS_IN_MODAL: Symbol('modal') }))
vi.mock('@/bridge', () => ({ ClipboardGetText: vi.fn() }))
vi.mock('@/directives/tips', () => ({ default: {} }))
import { PluginApiError } from '@/stores/plugins'
import PluginEditor from '@/views/PluginsView/components/PluginEditor.vue'
import CodeViewer from '@/components/CodeViewer/index.vue'

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
    vi.useFakeTimers()
    vi.resetAllMocks()
    mocks.get.mockReturnValue(initial)
    mocks.save.mockResolvedValue({ plugins: [initial], revision: 8 })
    host = document.createElement('div')
    document.body.append(host)
  })
  afterEach(() => {
    app.unmount()
    host.remove()
    vi.clearAllTimers()
    vi.useRealTimers()
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
  })
  const mount = async (mode: 'source' | 'configuration' = 'source', realEditor = false) => {
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
    app.directive('tips', {})
    app.component('Icon', { render: () => null })
    for (const name of ['Input', 'Select', 'Switch', 'Radio']) {
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
      realEditor
        ? CodeViewer
        : defineComponent({
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
    // Vue's capture/bubble guards compare integer Date.now timestamps. A real
    // setTimeout(1) may wake within the same millisecond on a fast CI runner.
    // Advance a controlled clock instead, without waiting for the editor's
    // 300ms model debounce.
    await vi.advanceTimersByTimeAsync(1)
  }
  const save = () => host.querySelector<HTMLElement>('footer [role=button]')!.click()
  it('reads the current editor document even before its debounced model update', async () => {
    await mount()
    host.querySelector('textarea')!.value =
      'function onSubscribe(p) { p[0].tag="edited"; return p }'
    save()
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
  it.each(['["invalid"]', '{"latest":true}'])(
    'validates the real CodeMirror document %s before its model debounce',
    async (content) => {
      await mount('configuration', true)
      const view = EditorView.findFromDOM(host.querySelector('.cm-editor')!)!
      view.dispatch({ changes: { from: 0, to: view.state.doc.length, insert: content } })
      save()
      await nextTick()
      await Promise.resolve()
      if (content.startsWith('[')) {
        expect(mocks.message).toHaveBeenCalledWith('plugins.invalidConfiguration')
        expect(mocks.save).not.toHaveBeenCalled()
      } else {
        expect(mocks.save).toHaveBeenCalledOnce()
        expect(mocks.save.mock.calls[0]![0].configuration).toEqual({ latest: true })
      }
    },
  )
  it.each(['save', 'export'])(
    'keeps a source import followed immediately by %s',
    async (action) => {
      await mount('source', true)
      const view = EditorView.findFromDOM(host.querySelector('.cm-editor')!)!
      const importedSource = 'function onSubscribe(p) { return p.slice(2) }'
      const imported = Promise.withResolvers<string>()
      const file = host.querySelector<HTMLInputElement>('input[type=file]')!
      Object.defineProperty(file, 'files', {
        value: [{ name: 'imported.js', size: 128, text: () => imported.promise }],
      })
      file.dispatchEvent(new Event('change', { bubbles: true }))
      imported.resolve(importedSource)
      await Promise.resolve()
      // The draft has changed, but Vue has not flushed it into CodeMirror yet.
      expect(view.state.doc.toString()).toBe(initial.source)
      const blobs: unknown[][] = []
      if (action === 'save') save()
      else {
        vi.stubGlobal(
          'Blob',
          class {
            constructor(parts: unknown[]) {
              blobs.push(parts)
            }
          },
        )
        vi.stubGlobal('URL', { createObjectURL: () => 'blob:test', revokeObjectURL: vi.fn() })
        vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => undefined)
        const button = [...host.querySelectorAll<HTMLElement>('[role=button]')].find(
          (item) => item.textContent?.trim() === 'plugins.exportSource',
        )!
        button.click()
      }
      await nextTick()
      await Promise.resolve()
      if (action === 'save') {
        expect(mocks.save).toHaveBeenCalledOnce()
        expect(mocks.save.mock.calls[0]![0].source).toBe(importedSource)
      } else expect(blobs).toEqual([[importedSource]])
    },
  )
  it('edits schema controls and keeps the same draft when switching to advanced JSON and back', async () => {
    mocks.get.mockReturnValue({
      ...initial,
      configuration_schema: [
        {
          key: 'enabled',
          title: 'Enabled',
          description: '',
          component: 'Switch',
          value: false,
        },
      ],
    })
    await mount('configuration')
    expect(host.querySelector('textarea')).toBeNull()
    host.querySelector<HTMLElement>('[role=switch]')!.click()
    await nextTick()
    const button = (label: string) =>
      [...host.querySelectorAll<HTMLElement>('[role=button]')].find(
        (item) => item.textContent?.trim() === label,
      )!
    button('plugins.advancedConfiguration').click()
    await nextTick()
    expect(JSON.parse(host.querySelector('textarea')!.value)).toEqual({
      enabled: true,
    })
    host.querySelector('textarea')!.value = '{"enabled":false,"custom":"kept"}'
    button('plugins.formConfiguration').click()
    await nextTick()
    expect(host.querySelector('[role=switch]')!.getAttribute('aria-checked')).toBe('false')
    save()
    await vi.waitFor(() => expect(mocks.save).toHaveBeenCalledOnce())
    expect(mocks.save.mock.calls[0]![0].configuration).toEqual({
      enabled: false,
      custom: 'kept',
    })
    expect(mocks.save.mock.calls[0]![0].configuration_schema).toHaveLength(1)
  })
  it('preserves invalid advanced JSON when a user tries to switch back to the form', async () => {
    mocks.get.mockReturnValue({
      ...initial,
      configuration_schema: [
        {
          key: 'enabled',
          title: 'Enabled',
          description: '',
          component: 'Switch',
          value: false,
        },
      ],
    })
    await mount('configuration')
    const button = (label: string) =>
      [...host.querySelectorAll<HTMLElement>('[role=button]')].find(
        (item) => item.textContent?.trim() === label,
      )!
    button('plugins.advancedConfiguration').click()
    await nextTick()
    host.querySelector('textarea')!.value = '[broken'
    button('plugins.formConfiguration').click()
    await nextTick()
    expect(host.querySelector('textarea')!.value).toBe('[broken')
    expect(mocks.message).toHaveBeenCalledWith('plugins.invalidConfiguration')
    expect(mocks.save).not.toHaveBeenCalled()
  })
})
