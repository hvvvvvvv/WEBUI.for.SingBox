// @vitest-environment jsdom
import { createApp, defineComponent, h, nextTick, ref, type App } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

const mocks = vi.hoisted(() => ({
  save: vi.fn(),
  restart: vi.fn(),
  message: vi.fn(),
  submit: vi.fn(),
  paths: vi.fn(),
  settings: {
    config: {
      branch: 'main',
      main: { env: {}, args: ['run'] },
      alpha: { env: {}, args: ['run'] },
    },
  },
}))
vi.mock('@/stores', () => ({
  useAppConfigStore: () => ({
    get config() {
      return mocks.settings.config
    },
    saveNow: mocks.save,
  }),
  useKernelApiStore: () => ({ running: true, restartCore: mocks.restart }),
}))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/bridge', () => ({ createRpcClient: () => ({ getRuntimePaths: mocks.paths }) }))
vi.mock('@/directives/tips', () => ({ default: {} }))
vi.mock('@/utils', async () => ({
  ...(await import('@/utils/others')),
  message: { error: mocks.message, success: vi.fn() },
}))
import CoreConfig from '@/views/SettingsView/components/components/CoreConfig.vue'

describe('core arguments save retries', () => {
  let app: App
  let host: HTMLDivElement
  const snapshots: Array<typeof mocks.settings.config> = []
  beforeEach(() => {
    vi.resetAllMocks()
    snapshots.length = 0
    mocks.paths.mockResolvedValue({ appBasePath: '/srv/router', coreBasePath: 'data/sing-box' })
    mocks.settings.config = {
      branch: 'main',
      main: { env: {}, args: ['run'] },
      alpha: { env: {}, args: ['run'] },
    }
    mocks.save.mockImplementation(async () => {
      // The real store replaces its config with the server's saved snapshot.
      mocks.settings.config = JSON.parse(JSON.stringify(mocks.settings.config))
      snapshots.push(JSON.parse(JSON.stringify(mocks.settings.config)))
      return mocks.settings.config
    })
    mocks.restart
      .mockRejectedValueOnce(new Error('invalid arguments'))
      .mockResolvedValue(undefined)
    host = document.createElement('div')
    document.body.append(host)
  })
  afterEach(() => {
    app.unmount()
    host.remove()
  })

  it.each([false, true])(
    'uses server paths and never invents a preview when failed=%s',
    async (failed) => {
      mocks.settings.config.main.args = [
        'run',
        '-c',
        '$APP_BASE_PATH/$CORE_BASE_PATH/config.json',
      ]
      if (failed) mocks.paths.mockRejectedValueOnce(new Error('offline'))
      app = createApp({ render: () => h(CoreConfig, { isAlpha: false }) })
      app.provide('submit', mocks.submit)
      app.provide('cancel', vi.fn())
      app.component('Icon', { render: () => null })
      app.component(
        'Tabs',
        defineComponent({
          setup:
            (_, { slots }) =>
            () =>
              h('div', slots.args?.()),
        }),
      )
      app.component('InputList', { render: () => null })
      app.component('KeyValueEditor', { render: () => null })
      app.mount(host)
      await vi.waitFor(() => expect(host.textContent).not.toContain('common.loading'))
      expect(mocks.paths).toHaveBeenCalledWith(
        {},
        { signal: expect.any(AbortSignal), timeoutMs: 5000 },
      )
      if (failed) {
        expect(host.querySelector('[data-runtime-args-preview]')).toBeNull()
        expect(host.textContent).toContain('settings.kernel.config.previewUnavailable')
      } else {
        expect(host.querySelector('[data-runtime-args-preview]')?.textContent).toContain(
          '/srv/router/data/sing-box/config.json',
        )
        expect(host.textContent).not.toContain('$APP_BASE_PATH')
      }
      expect(mocks.settings.config.main.args[2]).toBe(
        '$APP_BASE_PATH/$CORE_BASE_PATH/config.json',
      )
    },
  )

  it.each([false, true])(
    'saves corrections after a failed restart for isAlpha=%s',
    async (isAlpha) => {
      mocks.settings.config.branch = isAlpha ? 'alpha' : 'main'
      const editor = ref<any>()
      app = createApp({
        render: () =>
          h('div', [
            h(CoreConfig, { ref: editor, isAlpha }),
            h('footer', editor.value?.modalSlots.submit?.()),
          ]),
      })
      app.provide('submit', mocks.submit)
      app.provide('cancel', vi.fn())
      app.component('Icon', { render: () => null })
      app.component(
        'Tabs',
        defineComponent({
          setup:
            (_, { slots }) =>
            () =>
              h('div', slots.args?.()),
        }),
      )
      app.component(
        'InputList',
        defineComponent({
          props: ['modelValue'],
          emits: ['update:modelValue'],
          setup:
            (props, { emit }) =>
            () =>
              h('input', {
                value: props.modelValue.join(','),
                onInput: (event: Event) =>
                  emit(
                    'update:modelValue',
                    (event.target as HTMLInputElement).value.split(','),
                  ),
              }),
        }),
      )
      app.component('KeyValueEditor', { render: () => null })
      app.mount(host)
      await nextTick()
      await new Promise((resolve) => setTimeout(resolve, 1))
      const edit = (value: string) => {
        const input = host.querySelector('input')!
        input.value = value
        input.dispatchEvent(new InputEvent('input', { bubbles: true }))
      }
      const save = () => host.querySelector<HTMLElement>('footer [role=button]')!.click()
      edit('run,--invalid')
      save()
      await vi.waitFor(() => expect(mocks.message).toHaveBeenCalledWith('invalid arguments'))
      expect(mocks.submit).not.toHaveBeenCalled()
      edit('run,--disable-color')
      save()
      await vi.waitFor(() => expect(mocks.submit).toHaveBeenCalledOnce())
      const branch = isAlpha ? 'alpha' : 'main'
      expect(snapshots.map((snapshot) => snapshot[branch].args)).toEqual([
        ['run', '--invalid'],
        ['run', '--disable-color'],
      ])
      expect(mocks.restart).toHaveBeenCalledTimes(2)
    },
  )
})
