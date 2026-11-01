// @vitest-environment jsdom
import { createPinia, disposePinia, setActivePinia, type Pinia } from 'pinia'
import { nextTick } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('@/lang', () => ({ default: { global: { locale: { value: 'en' } } } }))
vi.mock('@/utils', async () => {
  const { debounce, deepClone } = await import('@/utils/others')
  return { debounce, deepClone }
})
import { AvailablePages, DefaultVisiblePages } from '@/constant/app'
import { Theme } from '@/enums/app'
import { useAppSettingsStore } from '@/stores/appSettings'

describe('browser UI preferences', () => {
  let pinia: Pinia
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
  })
  afterEach(() => {
    disposePinia(pinia)
    vi.useRealTimers()
    delete (document as Partial<Document>).startViewTransition
  })

  it('uses the four official default pages for a new browser', () => {
    const store = useAppSettingsStore()
    store.setupAppSettings()
    expect(store.app.pages).toEqual(['Overview', 'Profiles', 'Subscriptions', 'Plugins'])
    for (const key of [
      'debugOutline',
      'debugNoAnimation',
      'debugNoRounded',
      'debugUsePointer',
      'debugModalSideBySide',
    ] as const) {
      expect(store.app[key]).toBe(false)
    }
  })

  it('preserves all five feature flags explicitly enabled in saved preferences', () => {
    const keys = [
      'debugOutline',
      'debugNoAnimation',
      'debugNoRounded',
      'debugUsePointer',
      'debugModalSideBySide',
    ] as const
    localStorage.setItem(
      'appSettings',
      JSON.stringify(Object.fromEntries(keys.map((key) => [key, true]))),
    )
    const store = useAppSettingsStore()
    store.setupAppSettings()
    for (const key of keys) expect(store.app[key]).toBe(true)
    expect(document.body.getAttribute('feature-use-pointer')).toBe('true')
  })

  it('preserves all six explicitly selected pages across normalization and reload', async () => {
    localStorage.setItem('appSettings', JSON.stringify({ pages: AvailablePages }))
    const store = useAppSettingsStore()
    store.setupAppSettings()
    expect(store.app.pages).toEqual(AvailablePages)
    store.app.fontFamily = 'Changed font'
    await nextTick()
    await vi.advanceTimersByTimeAsync(600)
    disposePinia(pinia)
    pinia = createPinia()
    setActivePinia(pinia)
    const reloaded = useAppSettingsStore()
    reloaded.setupAppSettings()
    expect(reloaded.app.pages).toEqual(AvailablePages)
  })

  it('migrates settings with no saved pages to the official defaults without changing theme and font', () => {
    localStorage.setItem(
      'appSettings',
      JSON.stringify({
        lang: 'zh',
        theme: 'dark',
        fontFamily: 'Custom Font',
        primaryColor: '#123456',
      }),
    )
    const store = useAppSettingsStore()
    store.setupAppSettings()
    expect(store.app.pages).toEqual(DefaultVisiblePages)
    expect(store.app.lang).toBe('zh')
    expect(store.app.theme).toBe('dark')
    expect(store.app.fontFamily).toBe('Custom Font')
    expect(store.app.primaryColor).toBe('#123456')
    expect(store.app.debugUsePointer).toBe(false)
    expect(store.app.debugModalSideBySide).toBe(false)
    expect(store.app.kernel.realMemoryUsage).toBe(false)
  })
  it('applies and follows the system theme while authentication is still required', async () => {
    const addEventListener = vi.fn()
    vi.mocked(window.matchMedia).mockReturnValue({
      matches: true,
      addEventListener,
      removeEventListener: vi.fn(),
    } as unknown as MediaQueryList)
    sessionStorage.setItem(
      'sessionInfo',
      JSON.stringify({ authEnabled: true, cacheToken: '', requireLogin: true }),
    )
    const store = useAppSettingsStore()
    store.setupAppSettings()
    await nextTick()
    expect(document.body.getAttribute('theme-mode')).toBe('dark')
    expect(store.sessionInfo.requireLogin).toBe(true)
    const listener = addEventListener.mock.calls[0]![1]
    listener({ matches: false })
    await nextTick()
    expect(document.body.getAttribute('theme-mode')).toBe('light')
    expect(store.sessionInfo.cacheToken).toBe('')
  })
  it('handles aborted view-transition promises and ignores an older theme callback', async () => {
    const callbacks: Array<() => void> = []
    document.startViewTransition = vi.fn((update) => {
      callbacks.push(update as () => void)
      return {
        ready: Promise.reject(new DOMException('Transition aborted', 'AbortError')),
        finished: Promise.reject(new DOMException('Transition aborted', 'AbortError')),
        updateCallbackDone: Promise.resolve(),
      } as ViewTransition
    })
    const store = useAppSettingsStore()
    store.setupAppSettings()
    store.app.theme = Theme.Dark
    await nextTick()
    callbacks.at(-1)!()
    callbacks[0]!()
    expect(document.body.getAttribute('theme-mode')).toBe('dark')
    await Promise.resolve()
  })
  it('still applies the latest theme when transition startup or its update callback fails', async () => {
    document.startViewTransition = vi.fn(() => {
      throw new DOMException('Not available', 'InvalidStateError')
    })
    const store = useAppSettingsStore()
    store.setupAppSettings()
    store.app.theme = Theme.Dark
    await nextTick()
    expect(document.body.getAttribute('theme-mode')).toBe('dark')
    document.startViewTransition = vi.fn(
      () =>
        ({
          ready: Promise.reject(new Error('aborted')),
          finished: Promise.reject(new Error('aborted')),
          updateCallbackDone: Promise.reject(new Error('aborted')),
        }) as ViewTransition,
    )
    store.app.theme = Theme.Light
    await nextTick()
    await Promise.resolve()
    expect(document.body.getAttribute('theme-mode')).toBe('light')
  })

  it('persists an empty page selection and feature changes through a browser reload', async () => {
    const store = useAppSettingsStore()
    store.setupAppSettings()
    store.app.pages = []
    store.app.debugUsePointer = false
    store.app.debugModalSideBySide = true
    store.app.kernel.realMemoryUsage = true
    await nextTick()
    await vi.advanceTimersByTimeAsync(600)
    expect(document.body.getAttribute('feature-use-pointer')).toBe('false')
    disposePinia(pinia)
    pinia = createPinia()
    setActivePinia(pinia)
    const reloaded = useAppSettingsStore()
    reloaded.setupAppSettings()
    expect(reloaded.app.pages).toEqual([])
    expect(reloaded.app.debugModalSideBySide).toBe(true)
    expect(reloaded.app.debugUsePointer).toBe(false)
    expect(reloaded.app.kernel.realMemoryUsage).toBe(true)
    // A user can restore a page even when the previous selection was empty.
    reloaded.app.pages = ['Plugins']
    await nextTick()
    await vi.advanceTimersByTimeAsync(600)
    expect(JSON.parse(localStorage.getItem('appSettings')!).pages).toEqual(['Plugins'])
  })

  it('filters malformed page entries and rejects non-boolean feature values', () => {
    localStorage.setItem(
      'appSettings',
      JSON.stringify({
        pages: ['Plugins', 'Plugins', 'Settings', 'missing', null],
        debugOutline: 'true',
        debugNoAnimation: 1,
        debugNoRounded: [],
        debugUsePointer: 'false',
        debugModalSideBySide: {},
        kernel: { realMemoryUsage: 'true' },
      }),
    )
    const store = useAppSettingsStore()
    store.setupAppSettings()
    expect(store.app.pages).toEqual(['Plugins'])
    expect(store.app.debugOutline).toBe(false)
    expect(store.app.debugNoAnimation).toBe(false)
    expect(store.app.debugNoRounded).toBe(false)
    expect(store.app.debugUsePointer).toBe(false)
    expect(store.app.debugModalSideBySide).toBe(false)
    expect(store.app.kernel.realMemoryUsage).toBe(false)
  })
})
