// @vitest-environment jsdom
import { createApp, h, nextTick, ref, type App } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { Plugin } from '@/types/plugin'

const mocks = vi.hoisted(() => ({
  save: vi.fn(),
  setup: vi.fn(),
  state: { plugins: [] as Plugin[], revision: 7 },
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
    get plugins() {
      return mocks.state.plugins
    },
    get revision() {
      return mocks.state.revision
    },
    setupPlugins: mocks.setup,
    savePlugin: mocks.save,
  }),
}))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/lang', () => ({ default: { global: { t: (key: string) => key } } }))
vi.mock('@/utils', async () => await import('@/utils/others'))
vi.mock('@/bridge', () => ({ ClipboardGetText: vi.fn() }))
vi.mock('@/directives/tips', () => ({ default: {} }))
import { PluginApiError } from '@/stores/plugins'
import Button from '@/components/Button/index.vue'
import PluginConfigItem from '@/views/PluginsView/components/PluginConfigItem.vue'
import PluginSettings from '@/views/SettingsView/components/PluginSettings.vue'

const plugin = (id = 'rename'): Plugin => ({
  id,
  name: id,
  description: '',
  version: '1',
  source: 'function onSubscribe(p) { return p }',
  disabled: false,
  triggers: ['on::subscribe'],
  configuration: {},
  configuration_schema: [
    {
      key: 'flag',
      title: 'Show flag',
      description: 'Add a country flag',
      component: 'Switch',
      value: false,
    },
    {
      key: 'style',
      title: 'Style',
      description: '',
      component: 'Radio',
      value: 'short',
      options: ['Short,short', 'Full,full'],
    },
    {
      key: 'prefix',
      title: 'Prefix',
      description: '',
      component: 'Input',
      value: 'Default',
    },
  ],
})

describe('plugin graphical settings and automatic persistence', () => {
  let app: App
  let host: HTMLDivElement
  beforeEach(() => {
    vi.resetAllMocks()
    mocks.state = { plugins: [plugin()], revision: 7 }
    mocks.setup.mockResolvedValue(undefined)
    mocks.save.mockImplementation(async (item: Plugin, revision: number) => {
      expect(revision).toBe(mocks.state.revision)
      mocks.state.plugins = mocks.state.plugins.map((current) =>
        current.id === item.id ? item : current,
      )
      mocks.state.revision++
      return { ...mocks.state }
    })
    host = document.createElement('div')
    document.body.append(host)
  })
  afterEach(() => {
    app.unmount()
    host.remove()
  })
  const mount = async (render: () => ReturnType<typeof h> = () => h(PluginSettings)) => {
    app = createApp({ render })
    app.component('Icon', { render: () => null })
    app.component('CodeViewer', { render: () => null })
    app.component('Button', Button)
    app.directive('tips', {})
    app.mount(host)
    await nextTick()
    await new Promise((resolve) => setTimeout(resolve, 1))
  }
  const button = (text: string, within: ParentNode = host) =>
    [...within.querySelectorAll<HTMLElement>('[role=button]')].find(
      (item) => item.textContent?.trim() === text,
    )!

  it('renders numbered controls with defaults and supports overrides and both reset actions', async () => {
    const config = ref<Record<string, unknown>>({ custom: 'preserved' })
    const original = plugin()
    await mount(() =>
      h(PluginConfigItem, {
        plugin: original,
        modelValue: config.value,
        'onUpdate:modelValue': (value: Record<string, unknown>) => {
          config.value = value
        },
      }),
    )
    expect(host.textContent).toContain('1. Show flag')
    expect(host.textContent).toContain('2. Style')
    expect(host.textContent).toContain('3. Prefix')
    expect(host.querySelector('[role=switch]')!.getAttribute('aria-checked')).toBe('false')
    host.querySelector<HTMLElement>('[role=switch]')!.click()
    await nextTick()
    expect(config.value).toEqual({ custom: 'preserved', flag: true })
    host.querySelector<HTMLElement>('[aria-label="plugins.resetSetting: Show flag"]')!.click()
    await nextTick()
    expect(config.value).toEqual({ custom: 'preserved' })
    host.querySelectorAll<HTMLElement>('.gui-radio-button')[1]!.click()
    await nextTick()
    expect(config.value.style).toBe('full')
    host.querySelector<HTMLElement>('.is-editable > div')!.click()
    await nextTick()
    const input = host.querySelector('input')!
    input.value = 'Custom'
    input.dispatchEvent(new InputEvent('input', { bubbles: true }))
    await vi.waitFor(() => expect(config.value.prefix).toBe('Custom'))
    button('plugins.resetSettings').click()
    await nextTick()
    expect(config.value).toEqual({})
    expect(original.configuration).toEqual({})
    expect(mocks.save).not.toHaveBeenCalled()
  })

  it('persists multiline settings as literal text and blocks changes while disabled', async () => {
    const item = plugin()
    item.configuration_schema = [
      {
        key: 'hosts',
        title: 'Hosts',
        description: '',
        component: 'TextArea',
        value: '',
      },
    ]
    const config = ref<Record<string, unknown>>({})
    const disabled = ref(false)
    await mount(() =>
      h(PluginConfigItem, {
        plugin: item,
        modelValue: config.value,
        disabled: disabled.value,
        'onUpdate:modelValue': (value: Record<string, unknown>) => {
          config.value = value
        },
      }),
    )
    const textarea = host.querySelector('textarea')!
    const text = '127.0.0.1 router.lan\n<img src=x onerror=alert(1)>'
    textarea.value = text
    textarea.dispatchEvent(new Event('change', { bubbles: true }))
    await nextTick()
    expect(config.value.hosts).toBe(text)
    expect(host.querySelector('img')).toBeNull()
    disabled.value = true
    await nextTick()
    expect(textarea.disabled).toBe(true)
    textarea.value = 'changed'
    textarea.dispatchEvent(new Event('change', { bubbles: true }))
    await nextTick()
    expect(config.value.hosts).toBe(text)
  })

  it('automatically serializes changes across cards and preserves edits made during a save', async () => {
    mocks.state.plugins.push(plugin('second'))
    const pending = Promise.withResolvers<{
      plugins: Plugin[]
      revision: number
    }>()
    mocks.save.mockReturnValueOnce(pending.promise)
    await mount()
    const first = host.querySelector<HTMLElement>('[data-plugin-id=rename]')!
    const second = host.querySelector<HTMLElement>('[data-plugin-id=second]')!
    first.querySelector<HTMLElement>('[role=switch]')!.click()
    await nextTick()
    expect(mocks.save).toHaveBeenCalledTimes(1)
    first.querySelectorAll<HTMLElement>('.gui-radio-button')[1]!.click()
    second.querySelector<HTMLElement>('[role=switch]')!.click()
    await nextTick()
    expect(mocks.save).toHaveBeenCalledTimes(1)
    const sent = mocks.save.mock.calls[0]![0] as Plugin
    mocks.state.plugins[0] = sent
    mocks.state.revision = 8
    pending.resolve({ plugins: mocks.state.plugins, revision: 8 })
    await vi.waitFor(() => expect(mocks.save).toHaveBeenCalledTimes(3))
    expect(mocks.save.mock.calls.map((call) => call[1])).toEqual([7, 8, 9])
    expect(mocks.save.mock.calls[1]![0].configuration).toEqual({
      flag: true,
      style: 'full',
    })
    expect(mocks.save.mock.calls[2]![0].configuration).toEqual({ flag: true })
    await vi.waitFor(() => expect(host.textContent).toContain('plugins.settingsSaved'))
  })

  it('matches backend radio option parsing and safely handles schema keys inherited by objects', async () => {
    const item = plugin()
    item.configuration_schema = [
      {
        key: '__proto__',
        title: 'Value',
        description: '',
        component: 'Radio',
        value: 'a,b',
        options: ['Comma,a,b', 'Empty,'],
      },
      {
        key: 'constructor',
        title: 'Default label',
        description: '',
        component: 'Input',
        value: 'Default text',
      },
    ]
    const config = ref<Record<string, unknown>>({})
    await mount(() =>
      h(PluginConfigItem, {
        plugin: item,
        modelValue: config.value,
        'onUpdate:modelValue': (value: Record<string, unknown>) => {
          config.value = value
        },
      }),
    )
    expect(host.querySelector('.gui-radio-button.active')?.textContent).toBe('Comma')
    expect(host.textContent).toContain('Default text')
    host.querySelectorAll<HTMLElement>('.gui-radio-button')[1]!.click()
    await nextTick()
    expect(Object.hasOwn(config.value, '__proto__')).toBe(true)
    expect(config.value.__proto__).toBe('')
    expect(Object.getPrototypeOf(config.value)).toBe(Object.prototype)
  })

  it('blocks edits while replacing a conflicted draft with a requested fresh snapshot', async () => {
    mocks.save.mockRejectedValueOnce(new PluginApiError(409, 'conflict'))
    await mount()
    host.querySelector<HTMLElement>('[role=switch]')!.click()
    await vi.waitFor(() => expect(host.textContent).toContain('plugins.settingsConflict'))
    const pending = Promise.withResolvers<void>()
    mocks.setup.mockReturnValueOnce(pending.promise)
    button('common.loadLatest').click()
    await nextTick()
    expect(host.querySelector('[role=switch]')!.getAttribute('aria-disabled')).toBe('true')
    host.querySelectorAll<HTMLElement>('.gui-radio-button')[1]!.click()
    await nextTick()
    expect(mocks.save).toHaveBeenCalledTimes(1)
    pending.resolve()
    await vi.waitFor(() => expect(host.textContent).not.toContain('plugins.settingsConflict'))
    expect(host.querySelector('[role=switch]')!.getAttribute('aria-checked')).toBe('false')
  })

  it('keeps a conflicting draft visible and never retries until the user loads the latest version', async () => {
    mocks.save.mockImplementationOnce(async () => {
      mocks.state.revision = 9
      throw new PluginApiError(409, 'changed elsewhere')
    })
    await mount()
    host.querySelector<HTMLElement>('[role=switch]')!.click()
    await vi.waitFor(() => expect(host.textContent).toContain('plugins.settingsConflict'))
    expect(host.querySelector('[role=switch]')!.getAttribute('aria-checked')).toBe('true')
    host.querySelectorAll<HTMLElement>('.gui-radio-button')[1]!.click()
    await nextTick()
    expect(mocks.save).toHaveBeenCalledTimes(1)
    button('common.loadLatest').click()
    await vi.waitFor(() => expect(host.textContent).not.toContain('plugins.settingsConflict'))
    expect(host.querySelector('[role=switch]')!.getAttribute('aria-checked')).toBe('false')
    host.querySelector<HTMLElement>('[role=switch]')!.click()
    await vi.waitFor(() => expect(mocks.save).toHaveBeenCalledTimes(2))
    expect(mocks.save.mock.calls[1]![1]).toBe(9)
  })

  it('retains unsaved settings on a transport error and supports explicit retry', async () => {
    mocks.save.mockRejectedValueOnce(new Error('offline'))
    await mount()
    host.querySelector<HTMLElement>('[role=switch]')!.click()
    await vi.waitFor(() => expect(host.textContent).toContain('offline'))
    expect(host.querySelector('[role=switch]')!.getAttribute('aria-checked')).toBe('true')
    button('plugins.retry').click()
    await vi.waitFor(() => expect(mocks.save).toHaveBeenCalledTimes(2))
    expect(mocks.save.mock.calls[1]![0].configuration).toEqual({ flag: true })
    await vi.waitFor(() => expect(host.textContent).toContain('plugins.settingsSaved'))
  })
})
