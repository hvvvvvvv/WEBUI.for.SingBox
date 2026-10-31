// @vitest-environment jsdom
import { createPinia, disposePinia, setActivePinia, type Pinia } from 'pinia'
import { nextTick } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('@/lang', () => ({ default: { global: { locale: { value: 'en' } } } }))
vi.mock('@/utils', async () => {
  const { debounce, deepClone } = await import('@/utils/others')
  return { debounce, deepClone }
})
import { DefaultVisiblePages } from '@/constant/app'
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
  })

  it('migrates existing settings without hiding navigation or changing theme and font', () => {
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
    expect(store.app.debugUsePointer).toBe(true)
    expect(store.app.debugModalSideBySide).toBe(false)
    expect(store.app.kernel.realMemoryUsage).toBe(false)
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
        debugUsePointer: 'false',
        debugModalSideBySide: {},
        kernel: { realMemoryUsage: 'true' },
      }),
    )
    const store = useAppSettingsStore()
    store.setupAppSettings()
    expect(store.app.pages).toEqual(['Plugins'])
    expect(store.app.debugUsePointer).toBe(true)
    expect(store.app.debugModalSideBySide).toBe(false)
    expect(store.app.kernel.realMemoryUsage).toBe(false)
  })
})
