// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from 'vitest'
import { createApp, defineComponent, h, nextTick, ref, type App, type Component } from 'vue'

vi.mock('@/lang', () => ({ default: { global: { t: (text: string) => text } } }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (text: string) => text }) }))
vi.mock('@/utils', async () => {
  const { debounce, deepClone } = await import('@/utils/others')
  return { debounce, deepClone, getZoomLevel: () => 1 }
})

import Button from '@/components/Button/index.vue'
import Dropdown from '@/components/Dropdown/index.vue'
import Select from '@/components/Select/index.vue'
import Switch from '@/components/Switch/index.vue'

describe('keyboard controls', () => {
  let app: App
  let host: HTMLDivElement

  const mount = (component: Component) => {
    host = document.createElement('div')
    document.body.append(host)
    app = createApp(component)
    app.component('Button', Button)
    app.component('Dropdown', Dropdown)
    const empty = defineComponent(() => () => null)
    app.component('Icon', empty)
    app.component('Empty', empty)
    app.directive('tips', {})
    app.mount(host)
  }

  afterEach(() => {
    app.unmount()
    host.remove()
  })

  const key = async (element: Element, value: string, type = 'keydown') => {
    element.dispatchEvent(new KeyboardEvent(type, { key: value, bubbles: true, cancelable: true }))
    await nextTick()
  }

  it('activates buttons with Enter/Space and blocks disabled or busy actions', async () => {
    const clicked = vi.fn()
    const disabled = ref(false)
    const loading = ref(false)
    mount({
      render: () =>
        h(
          Button,
          { disabled: disabled.value, loading: loading.value, onClick: clicked },
          () => 'Save',
        ),
    })
    const button = host.querySelector<HTMLElement>('[role="button"]')!
    button.focus()
    await key(button, 'Enter')
    await key(button, ' ')
    expect(clicked).toHaveBeenCalledTimes(1)
    await key(button, ' ', 'keyup')
    expect(clicked).toHaveBeenCalledTimes(2)

    disabled.value = true
    await nextTick()
    await key(button, 'Enter')
    button.click()
    expect(button.getAttribute('aria-disabled')).toBe('true')
    expect(button.tabIndex).toBe(-1)
    expect(clicked).toHaveBeenCalledTimes(2)

    disabled.value = false
    loading.value = true
    await nextTick()
    await key(button, ' ', 'keyup')
    button.click()
    expect(clicked).toHaveBeenCalledTimes(2)
  })

  it('toggles switches from the keyboard and preserves disabled state', async () => {
    const value = ref(false)
    const disabled = ref(false)
    mount({
      render: () =>
        h(Switch, {
          modelValue: value.value,
          disabled: disabled.value,
          'onUpdate:modelValue': (next: boolean) => (value.value = next),
        }),
    })
    const control = host.querySelector<HTMLElement>('[role="switch"]')!
    await key(control, ' ')
    expect(value.value).toBe(true)
    expect(control.getAttribute('aria-checked')).toBe('true')
    disabled.value = true
    await nextTick()
    await key(control, 'Enter')
    expect(value.value).toBe(true)
    expect(control.getAttribute('aria-disabled')).toBe('true')
  })

  it('navigates enabled select options and closes with selection or Escape', async () => {
    const value = ref('first')
    mount({
      render: () =>
        h(Select, {
          modelValue: value.value,
          options: [
            { label: 'First', value: 'first' },
            { label: 'Disabled', value: 'disabled', disabled: true },
            { label: 'Last', value: 'last' },
          ],
          'onUpdate:modelValue': (next: string) => (value.value = next),
        }),
    })
    const trigger = host.querySelector<HTMLElement>('[role="combobox"]')!
    trigger.focus()
    await key(trigger, 'ArrowDown')
    await nextTick()
    const options = host.querySelectorAll<HTMLElement>('[role="option"]')
    expect(trigger.getAttribute('aria-expanded')).toBe('true')
    expect(document.activeElement).toBe(options[0])
    await key(options[0]!, 'ArrowDown')
    expect(document.activeElement).toBe(options[2])
    await key(options[2]!, 'Enter')
    expect(value.value).toBe('last')
    expect(trigger.getAttribute('aria-expanded')).toBe('false')
    expect(document.activeElement).toBe(trigger)

    await key(trigger, 'Enter')
    await nextTick()
    expect(trigger.getAttribute('aria-expanded')).toBe('true')
    await key(document.activeElement!, 'Escape')
    expect(trigger.getAttribute('aria-expanded')).toBe('false')
    expect(document.activeElement).toBe(trigger)
  })
})
