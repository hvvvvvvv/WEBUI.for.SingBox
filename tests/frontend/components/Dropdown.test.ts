// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from 'vitest'
import { createApp, defineComponent, h, KeepAlive, nextTick, ref, Teleport, type App } from 'vue'

vi.mock('@/utils', async () => {
  const { debounce } = await import('@/utils/others')
  return { debounce }
})
import Dropdown from '@/components/Dropdown/index.vue'

describe('dropdown coordinates and cached pages', () => {
  let app: App
  let host: HTMLDivElement
  const active = ref(true)
  const flush = async () => {
    await nextTick()
    await nextTick()
  }
  const mount = (zoom = '1.25', teleported = false, trigger: ('click' | 'hover')[] = ['click']) => {
    host = document.createElement('div')
    document.body.append(host)
    active.value = true
    // jsdom does not implement CSS zoom/layout; supply the browser's computed
    // zoom while keeping the actual DOM/Teleport ancestry and component code.
    const computedStyle = window.getComputedStyle.bind(window)
    vi.spyOn(window, 'getComputedStyle').mockImplementation((element) => {
      const style = computedStyle(element)
      Object.defineProperty(style, 'zoom', {
        value: (element as HTMLElement).dataset.cssZoom || '1',
      })
      return style
    })
    const page = defineComponent({
      render: () =>
        h(
          Dropdown,
          { trigger, delay: 100 },
          {
            default: ({ opened }: { opened: boolean }) =>
              h('button', { 'aria-expanded': opened }, 'Open'),
            overlay: () => h('button', 'Option'),
          },
        ),
    })
    app = createApp({
      render: () =>
        h('div', { class: 'app-zoomed', 'data-css-zoom': zoom }, [
          teleported
            ? h(Teleport, { to: 'body' }, [h(page)])
            : h(KeepAlive, null, {
                default: () => (active.value ? h(page) : null),
              }),
        ]),
    })
    app.mount(host)
    const root = document.querySelector<HTMLElement>('.gui-dropdown')!
    const overlay = document.querySelector<HTMLElement>('.gui-dropdown-overlay')!
    vi.spyOn(root, 'getBoundingClientRect').mockReturnValue({
      left: 500,
      top: 100,
      bottom: 140,
      width: 160,
      right: 660,
      height: 40,
      x: 500,
      y: 100,
      toJSON: () => ({}),
    })
    Object.defineProperty(overlay, 'offsetWidth', {
      configurable: true,
      value: 200,
    })
    Object.defineProperty(overlay, 'offsetHeight', {
      configurable: true,
      value: 120,
    })
    return { root, overlay }
  }

  afterEach(() => {
    app?.unmount()
    host?.remove()
    vi.useRealTimers()
    vi.restoreAllMocks()
  })

  it.each([
    ['desktop page', '1.25', false, 364, 112, 128],
    ['mobile page', '1', false, 480, 140, 160],
    ['body dialog with a zoomed application behind it', '1.25', true, 480, 140, 160],
    ['percentage CSS zoom', '125%', false, 364, 112, 128],
  ] as const)(
    'positions %s relative to its own zoom',
    async (_, zoom, teleported, left, top, width) => {
      const { root, overlay } = mount(zoom, teleported)
      root.querySelector('button')!.click()
      await flush()
      expect(parseFloat(overlay.style.left)).toBeCloseTo(left)
      expect(parseFloat(overlay.style.top)).toBeCloseTo(top)
      expect(parseFloat(overlay.style.minWidth)).toBeCloseTo(width)
    },
  )

  it('recalculates after a desktop-to-mobile resize and a scroll', async () => {
    const { root, overlay } = mount()
    root.querySelector('button')!.click()
    await flush()
    expect(overlay.style.top).toBe('112px')
    host.querySelector<HTMLElement>('.app-zoomed')!.dataset.cssZoom = '1'
    window.dispatchEvent(new Event('resize'))
    await flush()
    expect(overlay.style.top).toBe('140px')
    vi.mocked(root.getBoundingClientRect).mockReturnValue({
      ...root.getBoundingClientRect(),
      top: 50,
      bottom: 90,
    })
    root.dispatchEvent(new Event('scroll'))
    await flush()
    expect(overlay.style.top).toBe('90px')
  })

  it('closes a cached menu and does not restore it when navigating back', async () => {
    const { root, overlay } = mount()
    root.querySelector('button')!.click()
    await flush()
    expect(overlay.style.display).not.toBe('none')
    active.value = false
    await flush()
    active.value = true
    await flush()
    expect(root.querySelector('button')!.getAttribute('aria-expanded')).toBe('false')
    root.querySelector('button')!.click()
    await flush()
    expect(overlay.style.display).not.toBe('none')
    document.body.click()
    await flush()
    expect(root.querySelector('button')!.getAttribute('aria-expanded')).toBe('false')
  })

  it('cancels a delayed hover opening when the page is deactivated', async () => {
    vi.useFakeTimers()
    const { root, overlay } = mount('1.25', false, ['hover'])
    root.dispatchEvent(new MouseEvent('mouseenter'))
    active.value = false
    await flush()
    await vi.advanceTimersByTimeAsync(150)
    active.value = true
    await flush()
    expect(overlay.style.display).toBe('none')
  })
})
