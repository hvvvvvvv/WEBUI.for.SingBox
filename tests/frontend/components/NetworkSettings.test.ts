// @vitest-environment jsdom
import { createApp, defineComponent, h, nextTick, reactive, type App } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

const mocks = vi.hoisted(() => ({
  save: vi.fn(),
  test: vi.fn(),
  config: {} as Record<string, string>,
}))
vi.mock('@/stores/appConfig', () => ({
  useAppConfigStore: () => ({
    get config() {
      return mocks.config
    },
    saveNow: mocks.save,
    testNetworkRequest: mocks.test,
  }),
}))
vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ currentVersion: '1.0' }),
}))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/utils', () => ({
  APP_TITLE: 'webui',
  message: { success: vi.fn() },
  deepClone: (value: unknown) => JSON.parse(JSON.stringify(value)),
  debounce: (fn: (...args: any[]) => any) => Object.assign(fn, { cancel: vi.fn() }),
  getZoomLevel: () => 1,
}))
import NetworkSettings from '@/views/SettingsView/components/components/NetworkSettings.vue'
import Button from '@/components/Button/index.vue'
import Select from '@/components/Select/index.vue'
import Dropdown from '@/components/Dropdown/index.vue'

describe('backend network settings editor', () => {
  let app: App
  let host: HTMLDivElement
  const initial = () => ({
    requestProxyMode: 'custom',
    customProxy: 'http://127.0.0.1:8080',
    userAgent: 'old-agent',
    githubApiToken: '',
    githubDownloadMirror: '',
  })
  const flush = async () => {
    await nextTick()
    await new Promise((resolve) => setTimeout(resolve, 0))
    await nextTick()
  }
  beforeEach(async () => {
    vi.resetAllMocks()
    mocks.config = reactive(initial())
    mocks.save.mockResolvedValue(undefined)
    mocks.test.mockResolvedValue({ statusCode: 204, durationMs: 1n })
    host = document.createElement('div')
    document.body.append(host)
    app = createApp(NetworkSettings)
    app.component(
      'Card',
      defineComponent({
        setup:
          (_, { slots }) =>
          () =>
            h('section', slots.default?.()),
      }),
    )
    app.component(
      'Input',
      defineComponent({
        props: ['modelValue', 'disabled'],
        emits: ['update:modelValue'],
        setup:
          (props, { emit }) =>
          () =>
            h('input', {
              value: props.modelValue,
              disabled: props.disabled,
              onInput: (event: Event) =>
                emit('update:modelValue', (event.target as HTMLInputElement).value),
            }),
      }),
    )
    app.component('Select', Select)
    app.component('Dropdown', Dropdown)
    app.component('Empty', { render: () => null })
    app.component('Button', Button)
    app.component('Icon', { render: () => null })
    app.mount(host)
    await flush()
  })
  afterEach(() => {
    app.unmount()
    host.remove()
  })
  const input = (label: string) =>
    host.querySelector<HTMLInputElement>(`input[aria-label="${label}"]`)!
  const button = (text: string) =>
    [...host.querySelectorAll<HTMLElement>('[role="button"]')].find(
      (item) => item.textContent?.trim() === text,
    )!
  const setValue = async (label: string, value: string) => {
    const target = input(label)
    target.value = value
    target.dispatchEvent(new Event('input', { bubbles: true }))
    await flush()
  }

  it('keeps a failed draft editable, and saves it before the backend probe', async () => {
    await setValue('settings.userAgent.name', 'new-agent')
    expect(mocks.config.userAgent).toBe('old-agent')
    mocks.save.mockImplementationOnce(async () => {
      Object.assign(mocks.config, initial())
      throw new Error('disk write failed')
    })
    button('common.save').click()
    await flush()
    expect(host.querySelector('[role="alert"]')?.textContent).toContain('disk write failed')
    expect(input('settings.userAgent.name').value).toBe('new-agent')
    expect(button('common.save').getAttribute('aria-disabled')).not.toBe('true')
    mocks.save.mockImplementationOnce(async () => {
      expect(mocks.config.userAgent).toBe('new-agent')
    })
    button('settings.requestProxy.test').click()
    await flush()
    expect(mocks.save).toHaveBeenCalledTimes(2)
    expect(mocks.test).toHaveBeenCalledWith('https://www.gstatic.com/generate_204')
    expect(mocks.save.mock.invocationCallOrder[1]).toBeLessThan(
      mocks.test.mock.invocationCallOrder[0]!,
    )
    expect(host.querySelector('[role="status"]')?.textContent).toBe(
      'settings.requestProxy.testSuccess',
    )
    expect(input('settings.githubapi.name').type).toBe('password')
  })

  it('does not probe when saving the network policy fails', async () => {
    mocks.save.mockRejectedValue(new Error('invalid proxy'))
    button('settings.requestProxy.test').click()
    await flush()
    expect(mocks.test).not.toHaveBeenCalled()
    expect(host.querySelector('[role="alert"]')?.textContent).toContain('invalid proxy')
  })

  it.each(['save', 'probe'])(
    'locks the real Select while %s is pending and restores it afterwards',
    async (phase) => {
      const trigger = host.querySelector<HTMLElement>('[role="combobox"]')!
      const option = (mode: string) =>
        [...host.querySelectorAll<HTMLElement>('[role="option"]')].find(
          (item) => item.textContent?.trim() === `settings.requestProxy.${mode}`,
        )!
      trigger.click()
      await flush()
      option('core').click()
      await flush()
      expect(trigger.textContent).toContain('settings.requestProxy.core')
      let release!: () => void
      const pending = new Promise<void>((resolve) => {
        release = resolve
      })
      if (phase === 'save') mocks.save.mockReturnValueOnce(pending)
      else
        mocks.test.mockImplementationOnce(async () => {
          await pending
          return { statusCode: 204, durationMs: 1n }
        })
      button(phase === 'save' ? 'common.save' : 'settings.requestProxy.test').click()
      await flush()
      expect(trigger.closest('[inert]')).not.toBeNull()
      expect(trigger.closest('[aria-disabled="true"]')).not.toBeNull()
      trigger.click()
      trigger.dispatchEvent(
        new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true, cancelable: true }),
      )
      option('none').click()
      await flush()
      expect(trigger.getAttribute('aria-expanded')).toBe('false')
      expect(trigger.textContent).toContain('settings.requestProxy.core')
      expect(mocks.config.requestProxyMode).toBe('core')
      release()
      await flush()
      expect(trigger.closest('[inert]')).toBeNull()
      expect(trigger.textContent).toContain('settings.requestProxy.core')
      trigger.click()
      await flush()
      option('custom').click()
      await flush()
      expect(trigger.textContent).toContain('settings.requestProxy.custom')
      expect(button('common.save').getAttribute('aria-disabled')).not.toBe('true')
    },
  )
})
