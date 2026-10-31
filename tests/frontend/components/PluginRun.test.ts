// @vitest-environment jsdom
import { createApp, defineComponent, h, nextTick, type App } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

const mocks = vi.hoisted(() => ({ run: vi.fn(), setup: vi.fn() }))
vi.mock('@/stores/plugins', () => ({ usePluginsStore: () => ({ runPlugin: mocks.run }) }))
vi.mock('@/stores/subscribes', () => ({
  useSubscribesStore: () => ({
    setupSubscribes: mocks.setup,
    subscribes: [
      { id: 'cached', name: 'Cached', proxies: [{ tag: 'one' }, { tag: 'two' }] },
      { id: 'empty', name: 'Empty', proxies: [] },
    ],
  }),
}))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
import PluginRun from '@/views/PluginsView/components/PluginRun.vue'

describe('manual backend plugin execution', () => {
  let app: App
  let host: HTMLDivElement
  beforeEach(() => {
    vi.clearAllMocks()
    mocks.setup.mockResolvedValue(undefined)
    mocks.run.mockResolvedValue({
      before: 2,
      after: 1,
      logs: ['deduplicated'],
      subscription_id: 'cached',
    })
    host = document.createElement('div')
    document.body.append(host)
  })
  afterEach(() => {
    app.unmount()
    host.remove()
  })
  it('requires a cached subscription, executes server-side and refreshes persisted nodes', async () => {
    app = createApp(PluginRun, { id: 'beautify' })
    app.component(
      'Button',
      defineComponent({
        props: ['disabled', 'loading'],
        setup:
          (props, { slots }) =>
          () =>
            h('button', { disabled: props.disabled || props.loading }, slots.default?.()),
      }),
    )
    app.component(
      'Select',
      defineComponent({
        props: ['options', 'modelValue'],
        emits: ['update:modelValue'],
        setup:
          (props, { emit }) =>
          () =>
            h(
              'select',
              {
                value: props.modelValue,
                onChange: (e: Event) =>
                  emit('update:modelValue', (e.target as HTMLSelectElement).value),
              },
              [
                h('option', { value: '' }),
                ...props.options.map((item: any) => h('option', { value: item.value }, item.label)),
              ],
            ),
      }),
    )
    app.mount(host)
    await nextTick()
    await nextTick()
    expect(host.querySelector('button')!.disabled).toBe(true)
    expect([...host.querySelectorAll('option')].map((item) => item.value)).toEqual(['', 'cached'])
    const select = host.querySelector('select')!
    select.value = 'cached'
    select.dispatchEvent(new Event('change', { bubbles: true }))
    await nextTick()
    host.querySelector('button')!.click()
    await vi.waitFor(() => expect(mocks.run).toHaveBeenCalledWith('beautify', 'cached'))
    await vi.waitFor(() => expect(mocks.setup).toHaveBeenCalledTimes(2))
    expect(host.textContent).toContain('deduplicated')
    expect(host.textContent).toContain('plugins.runResult')
  })
})
