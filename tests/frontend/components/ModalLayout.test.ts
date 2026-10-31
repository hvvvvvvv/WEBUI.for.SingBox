// @vitest-environment jsdom
import {
  createApp,
  defineComponent,
  h,
  nextTick,
  onMounted,
  onUnmounted,
  reactive,
  ref,
  type App,
} from 'vue'
import { createPinia, disposePinia, setActivePinia, type Pinia } from 'pinia'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

const shared = vi.hoisted(() => ({ settings: null as any, app: null as any }))
vi.mock('@/stores/appSettings', () => ({ useAppSettingsStore: () => shared.settings }))
vi.mock('@/stores', () => ({ useAppStore: () => shared.app }))
vi.mock('@/lang', () => ({ default: { global: { t: (text: string) => text } } }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (text: string) => text }) }))
vi.mock('@/utils', () => ({ message: { info: () => ({ destroy: vi.fn() }) } }))
vi.mock('@/hooks', async () => ({ useBool: (await import('@/hooks/useBool')).useBool }))
import Modal from '@/components/Modal/index.vue'
import ModalPanel from '@/components/_common/ModalPanel.vue'
import { useModalLayoutStore } from '@/stores/modalLayout'

describe('browser side-by-side modal tabs', () => {
  let app: App
  let pinia: Pinia
  let host: HTMLDivElement
  let resize: (event: { matches: boolean }) => void
  const mounted = vi.fn()
  const unmounted = vi.fn()
  const firstOpen = ref(true)
  const secondOpen = ref(true)
  const allowClose = ref(true)
  beforeEach(() => {
    vi.useFakeTimers()
    vi.clearAllMocks()
    shared.settings = reactive({ app: { debugModalSideBySide: false } })
    shared.app = { modalStack: [], modalZIndexCounter: 999 }
    firstOpen.value = true
    secondOpen.value = true
    allowClose.value = true
    window.matchMedia = vi.fn().mockReturnValue({
      matches: true,
      addEventListener: (_: string, listener: typeof resize) => {
        resize = listener
      },
      removeEventListener: vi.fn(),
    })
    pinia = createPinia()
    setActivePinia(pinia)
    host = document.createElement('div')
    document.body.append(host)
  })
  afterEach(() => {
    app.unmount()
    disposePinia(pinia)
    host.remove()
    vi.useRealTimers()
  })
  const mount = async () => {
    const Editor = defineComponent({
      props: ['name'],
      setup(props) {
        const value = ref('draft')
        onMounted(() => mounted(props.name))
        onUnmounted(() => unmounted(props.name))
        return () =>
          h('input', {
            'data-editor': props.name,
            value: value.value,
            onInput: (event: Event) => (value.value = (event.target as HTMLInputElement).value),
          })
      },
    })
    app = createApp({
      render: () =>
        h('div', [
          h(ModalPanel),
          h(
            Modal,
            {
              title: 'First',
              open: firstOpen.value,
              'onUpdate:open': (value: boolean) => (firstOpen.value = value),
            },
            () => h(Editor, { name: 'first' }),
          ),
          h(
            Modal,
            {
              title: 'Second',
              open: secondOpen.value,
              beforeClose: () => allowClose.value,
              'onUpdate:open': (value: boolean) => (secondOpen.value = value),
            },
            () => h(Editor, { name: 'second' }),
          ),
        ]),
    })
    app.use(pinia)
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
    app.mount(host)
    await nextTick()
    await nextTick()
    await vi.advanceTimersByTimeAsync(1)
  }

  it('moves the same editors between split panels and dialogs, including narrow-screen fallback', async () => {
    await mount()
    const layout = useModalLayoutStore()
    expect(layout.tabs).toHaveLength(2)
    expect(layout.splitActive).toBe(false)
    const draft = document.querySelector<HTMLInputElement>('[data-editor=first]')!
    draft.value = 'unsaved changes'
    draft.dispatchEvent(new Event('input', { bubbles: true }))
    shared.settings.app.debugModalSideBySide = true
    await nextTick()
    expect(layout.splitActive).toBe(true)
    expect(host.querySelectorAll('#app-modal-content [role=dialog]')).toHaveLength(2)
    expect(host.querySelector('#app-modal-content [role=dialog]')!.hasAttribute('aria-modal')).toBe(
      false,
    )
    layout.tabs[0]!.activate()
    await nextTick()
    expect(host.querySelector<HTMLInputElement>('[data-editor=first]')!.value).toBe(
      'unsaved changes',
    )
    expect(
      host
        .querySelector('[data-editor=second]')!
        .closest('.gui-modal-mask')!
        .getAttribute('aria-hidden'),
    ).toBe('true')
    shared.settings.app.debugModalSideBySide = false
    await nextTick()
    expect(host.querySelectorAll('#app-modal-content [role=dialog]')).toHaveLength(0)
    shared.settings.app.debugModalSideBySide = true
    await nextTick()
    resize({ matches: false })
    await nextTick()
    expect(layout.splitActive).toBe(false)
    expect(document.querySelectorAll('[role=dialog][aria-modal=true]')).toHaveLength(2)
    expect(shared.settings.app.debugModalSideBySide).toBe(true)
    resize({ matches: true })
    await nextTick()
    expect(layout.splitActive).toBe(true)
    expect(mounted).toHaveBeenCalledTimes(2)
    expect(unmounted).not.toHaveBeenCalled()
  })

  it('honors close guards and releases a closed tab content and Escape handler', async () => {
    await mount()
    const layout = useModalLayoutStore()
    shared.settings.app.debugModalSideBySide = true
    await nextTick()
    allowClose.value = false
    host.querySelector<HTMLButtonElement>('[aria-label="common.close: Second"]')!.click()
    await vi.advanceTimersByTimeAsync(300)
    expect(secondOpen.value).toBe(true)
    expect(layout.tabs).toHaveLength(2)
    allowClose.value = true
    host.querySelector<HTMLButtonElement>('[aria-label="common.close: Second"]')!.click()
    await vi.advanceTimersByTimeAsync(300)
    await nextTick()
    expect(secondOpen.value).toBe(false)
    expect(layout.tabs).toHaveLength(1)
    expect(layout.activeTab).toBe(layout.tabs[0]!.id)
    expect(unmounted).toHaveBeenCalledWith('second')
    expect(shared.app.modalStack).toHaveLength(1)
    host.querySelector<HTMLButtonElement>('[aria-label="common.close: First"]')!.click()
    await vi.advanceTimersByTimeAsync(300)
    await nextTick()
    expect(layout.tabs).toHaveLength(0)
    expect(layout.splitActive).toBe(false)
    expect(shared.app.modalStack).toHaveLength(0)
    expect(unmounted).toHaveBeenCalledWith('first')
  })
})
