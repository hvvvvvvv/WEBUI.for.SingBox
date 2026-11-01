// @vitest-environment jsdom
import { createApp, defineComponent, h, nextTick, type App } from 'vue'
import { createPinia, disposePinia, setActivePinia, type Pinia } from 'pinia'
import { createI18n } from 'vue-i18n'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('@/lang', () => ({ default: { global: { locale: { value: 'en' } } } }))
vi.mock('@/utils', async () => {
  const { debounce, deepClone } = await import('@/utils/others')
  return { debounce, deepClone }
})
vi.mock('@/stores', async () => ({
  useAppSettingsStore: (await import('@/stores/appSettings')).useAppSettingsStore,
  useAppStore: () => ({ locales: [] }),
}))
vi.mock('@/router/routes', () => ({
  default: [
    'Overview',
    'Profiles',
    'Subscriptions',
    'Rulesets',
    'Plugins',
    'ScheduledTasks',
    'Settings',
  ].map((name) => ({ name, meta: { name } })),
}))

import { useAppSettingsStore } from '@/stores/appSettings'
import { useBackgroundStore } from '@/stores/background'
import PersonalizationSettings from '@/views/SettingsView/components/components/PersonalizationSettings.vue'
import en from '@/lang/locale/en'

describe('official segmented page visibility selector', () => {
  let app: App
  let pinia: Pinia
  let host: HTMLDivElement

  beforeEach(() => {
    vi.useFakeTimers()
    localStorage.clear()
    sessionStorage.clear()
    window.matchMedia = vi.fn().mockReturnValue({
      matches: false,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
    })
    pinia = createPinia()
    setActivePinia(pinia)
    useAppSettingsStore().setupAppSettings()
    host = document.createElement('div')
    document.body.append(host)
    app = createApp(PersonalizationSettings)
    app.use(pinia)
    app.use(
      createI18n({
        legacy: false,
        locale: 'en',
        missingWarn: false,
        fallbackWarn: false,
        messages: { en },
      }),
    )
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
      'Button',
      defineComponent({
        props: { disabled: Boolean, loading: Boolean },
        setup:
          (props, { slots }) =>
          () =>
            h('button', { disabled: props.disabled || props.loading }, slots.default?.()),
      }),
    )
    for (const name of ['Radio', 'ColorPicker', 'Input'])
      app.component(name, { render: () => null })
    app.directive('tips', {})
    app.mount(host)
  })

  afterEach(() => {
    app.unmount()
    disposePinia(pinia)
    host.remove()
    vi.useRealTimers()
  })

  it('independently toggles each page and follows externally restored preferences', async () => {
    const buttons = [...host.querySelectorAll<HTMLButtonElement>('[role=checkbox]')]
    expect(buttons.map((button) => button.textContent?.trim())).toEqual([
      'Overview',
      'Profiles',
      'Subscriptions',
      'Rulesets',
      'Plugins',
      'ScheduledTasks',
    ])
    expect(buttons.map((button) => button.getAttribute('aria-checked'))).toEqual([
      'true',
      'true',
      'true',
      'false',
      'true',
      'false',
    ])
    expect(host.querySelector('input[type=checkbox]')).toBeNull()
    const store = useAppSettingsStore()
    buttons[3]!.click()
    await nextTick()
    buttons[5]!.click()
    await nextTick()
    buttons[1]!.click()
    await nextTick()
    expect(store.app.pages).toEqual([
      'Overview',
      'Subscriptions',
      'Plugins',
      'Rulesets',
      'ScheduledTasks',
    ])
    store.app.pages = ['ScheduledTasks']
    await nextTick()
    expect(buttons.map((button) => button.getAttribute('aria-checked'))).toEqual([
      'false',
      'false',
      'false',
      'false',
      'false',
      'true',
    ])
    buttons[5]!.click()
    await nextTick()
    expect(store.app.pages).toEqual([])
  })

  it('offers local image upload and removal and displays a failed deletion without hiding the old image', async () => {
    const background = useBackgroundStore()
    const input = host.querySelector<HTMLInputElement>('input[type=file]')!
    expect(input.accept).toBe('image/png,image/jpeg,image/webp')
    expect(host.textContent).toContain('Saved only in this browser. Never uploaded.')
    const file = new File(['pixels'], 'background.png', { type: 'image/png' })
    const upload = vi.spyOn(background, 'upload').mockResolvedValue(true)
    Object.defineProperty(input, 'files', { value: [file] })
    input.dispatchEvent(new Event('change', { bubbles: true }))
    await nextTick()
    expect(upload).toHaveBeenCalledExactlyOnceWith(file)
    background.url = 'blob:previous-background'
    vi.spyOn(background, 'remove').mockImplementation(async () => {
      background.error = 'settings.background.removeFailed'
      return false
    })
    await nextTick()
    const remove = [...host.querySelectorAll('button')].find(
      (button) => button.textContent?.trim() === 'Remove',
    )!
    remove.click()
    await nextTick()
    expect(background.url).toBe('blob:previous-background')
    expect(host.querySelector('[role=alert]')?.textContent).toContain('Unable to remove the image.')
  })
})
