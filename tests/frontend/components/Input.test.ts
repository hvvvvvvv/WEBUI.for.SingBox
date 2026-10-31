// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createApp, defineComponent, h, nextTick, ref, type App } from 'vue'

vi.mock('@/lang', () => ({ default: { global: { t: (text: string) => text } } }))
vi.mock('@/utils', async () => {
  const { debounce } = await import('@/utils/others')
  return { debounce }
})
vi.mock('@/bridge', () => ({ ClipboardGetText: vi.fn() }))

import Input from '@/components/Input/index.vue'

describe('Input IME composition', () => {
  let app: App
  let host: HTMLDivElement
  let input: HTMLInputElement
  const changed = vi.fn()
  const submitted = vi.fn()

  beforeEach(() => {
    vi.useFakeTimers()
    changed.mockClear()
    submitted.mockClear()
    host = document.createElement('div')
    document.body.append(host)
    const value = ref('')
    app = createApp({
      render: () =>
        h(Input, {
          modelValue: value.value,
          delay: 50,
          'onUpdate:modelValue': (next: string) => {
            value.value = next
            changed(next)
          },
          onSubmit: submitted,
        }),
    })
    const empty = defineComponent(() => () => null)
    for (const name of ['Icon', 'Button', 'CodeViewer']) app.component(name, empty)
    app.mount(host)
    input = host.querySelector('input')!
    input.focus()
  })

  afterEach(() => {
    app.unmount()
    host.remove()
    vi.useRealTimers()
  })

  it('cancels pending input and publishes only the committed Chinese text', async () => {
    input.value = 'n'
    input.dispatchEvent(new InputEvent('input', { bubbles: true }))
    input.dispatchEvent(new CompositionEvent('compositionstart', { bubbles: true }))
    input.value = 'ni'
    input.dispatchEvent(new InputEvent('input', { bubbles: true, isComposing: true }))
    await vi.advanceTimersByTimeAsync(60)
    expect(changed).not.toHaveBeenCalled()

    input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', isComposing: true }))
    await nextTick()
    expect(document.activeElement).toBe(input)
    expect(submitted).not.toHaveBeenCalled()

    input.value = '你'
    input.dispatchEvent(new CompositionEvent('compositionend', { bubbles: true, data: '你' }))
    // Browsers may send a final input event after compositionend.
    input.dispatchEvent(new InputEvent('input', { bubbles: true, isComposing: false }))
    await vi.advanceTimersByTimeAsync(60)
    expect(changed.mock.calls).toEqual([['你']])
    expect(input.value).toBe('你')

    input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter' }))
    await nextTick()
    await vi.runOnlyPendingTimersAsync()
    expect(submitted.mock.calls).toEqual([['你']])
  })

  it('keeps focus for the legacy IME keyCode 229 Enter event', async () => {
    input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', keyCode: 229 }))
    await nextTick()
    await vi.runOnlyPendingTimersAsync()
    expect(document.activeElement).toBe(input)
    expect(submitted).not.toHaveBeenCalled()
  })
})
