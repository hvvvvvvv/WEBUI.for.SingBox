// @vitest-environment jsdom
import { createApp, defineComponent, h, nextTick, ref, type App } from 'vue'
import { EditorView } from '@codemirror/view'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { parse } from 'yaml'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/stores', () => ({ useAppSettingsStore: () => ({ themeMode: 'light' }) }))
vi.mock('@/lang', async () => ({
  default: { global: { t: (key: string) => key, locale: (await import('vue')).ref('en') } },
}))
vi.mock('@/utils', async () => ({
  ...(await import('@/utils/others')),
  message: { error: vi.fn() },
}))
vi.mock('@/components/Modal/index.vue', () => ({ IS_IN_MODAL: Symbol('modal') }))
import MixinAndScript from '@/views/ProfilesView/components/MixinAndScriptConfig.vue'
import CodeViewer from '@/components/CodeViewer/index.vue'
import Tabs from '@/components/Tabs/index.vue'

let app: App
let host: HTMLDivElement
beforeEach(() => {
  vi.useFakeTimers()
  host = document.createElement('div')
  document.body.append(host)
})
afterEach(() => {
  app.unmount()
  host.remove()
  vi.clearAllTimers()
  vi.useRealTimers()
})
const mount = async () => {
  const model = ref({
    mixin: { config: '{}', format: 'json', priority: 'mixin' },
    script: { code: 'old' },
  })
  const editor = ref<any>()
  const saved = vi.fn()
  app = createApp({
    render: () =>
      h('div', [
        h(MixinAndScript, { ref: editor, modelValue: model.value as any }),
        h(
          'button',
          {
            class: 'save',
            onClick: () => {
              editor.value.flush()
              saved(JSON.parse(JSON.stringify(model.value)))
            },
          },
          'save',
        ),
      ]),
  })
  app.component('Tabs', Tabs)
  app.component('CodeViewer', CodeViewer)
  app.component('Icon', { render: () => null })
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
    'Radio',
    defineComponent({
      props: ['modelValue', 'options'],
      emits: ['update:modelValue', 'change'],
      setup:
        (props, { emit }) =>
        () =>
          h(
            'div',
            props.options.map((option: any) =>
              h(
                'button',
                {
                  'data-option': option.value,
                  onClick: () => {
                    const old = props.modelValue
                    emit('update:modelValue', option.value)
                    emit('change', option.value, old)
                  },
                },
                option.label,
              ),
            ),
          ),
    }),
  )
  app.mount(host)
  await nextTick()
  await vi.advanceTimersByTimeAsync(1)
  return { model, saved }
}
const type = (value: string) => {
  const view = EditorView.findFromDOM(host.querySelector('.cm-editor')!)!
  view.dispatch({ changes: { from: 0, to: view.state.doc.length, insert: value } })
}

it('saves the current mixin and script documents without waiting for model debounce', async () => {
  const { saved } = await mount()
  type('{"route":{"auto_detect_interface":true}}')
  host.querySelector<HTMLButtonElement>('.save')!.click()
  expect(saved.mock.calls[0]![0].mixin.config).toContain('auto_detect_interface')
  const scriptTab = [...host.querySelectorAll<HTMLElement>('[role=tab]')].find(
    (tab) => tab.textContent === 'profile.scriptSettings.name',
  )!
  scriptTab.click()
  await vi.advanceTimersByTimeAsync(1)
  type('function onGenerate(config) { return {...config, log: {level: "debug"}} }')
  host.querySelector<HTMLButtonElement>('.save')!.click()
  expect(saved.mock.calls[1]![0].script.code).toContain('level: "debug"')
})

it('converts the current mixin document and does not restore stale input after changing format', async () => {
  const { model } = await mount()
  type('{"log":{"level":"debug"}}')
  host.querySelector<HTMLButtonElement>('[data-option=yaml]')!.click()
  await vi.advanceTimersByTimeAsync(300)
  expect(model.value.mixin.format).toBe('yaml')
  expect(model.value.mixin.config).toContain('level: debug')
  expect(parse(model.value.mixin.config)).toEqual({ log: { level: 'debug' } })
})

it('retains the last keystrokes when switching away from the mixin tab immediately', async () => {
  const { model } = await mount()
  type('{"log":{"level":"trace"}}')
  const scriptTab = [...host.querySelectorAll<HTMLElement>('[role=tab]')].find(
    (tab) => tab.textContent === 'profile.scriptSettings.name',
  )!
  scriptTab.click()
  await vi.advanceTimersByTimeAsync(1)
  expect(JSON.parse(model.value.mixin.config)).toEqual({ log: { level: 'trace' } })
})
