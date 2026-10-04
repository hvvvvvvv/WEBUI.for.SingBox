import { createPinia, setActivePinia } from 'pinia'
import { afterEach, describe, expect, it, vi } from 'vitest'

vi.mock('@/lang', () => ({ default: { global: { locale: { value: 'en' } } } }))
vi.mock('@/utils', () => ({
  deepClone: <T>(value: T): T => JSON.parse(JSON.stringify(value)),
  debounce: (fn: (...args: any[]) => any) => Object.assign(fn, { cancel: () => {} }),
}))

import { useAppSettingsStore } from '../appSettings'

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('native API preference migration', () => {
  it('removes obsolete kernel preferences from storage while preserving remaining settings', () => {
    const storage = new Map<string, string>([
      [
        'appSettings',
        JSON.stringify({
          lang: 'zh',
          kernel: {
            autoClose: false,
            cardColumns: 3,
            realMemoryUsage: true,
            testUrl: 'https://test.invalid/',
            testTimeout: 500,
            concurrencyLimit: 10,
          },
          connections: {
            visibility: { 'metadata.host': false },
            order: ['upload', 'metadata.host'],
          },
        }),
      ],
    ])
    vi.stubGlobal('localStorage', {
      getItem: (key: string) => storage.get(key) ?? null,
      setItem: (key: string, value: string) => storage.set(key, value),
    })
    vi.stubGlobal('sessionStorage', { getItem: () => null, setItem: vi.fn() })
    vi.stubGlobal('window', { matchMedia: () => ({ matches: false, addEventListener: vi.fn() }) })
    vi.stubGlobal('document', {
      body: { setAttribute: vi.fn(), style: {} },
      documentElement: { style: { setProperty: vi.fn() } },
    })
    setActivePinia(createPinia())
    const settings = useAppSettingsStore()
    settings.setupAppSettings()

    const saved = JSON.parse(storage.get('appSettings')!)
    expect(saved.lang).toBe('zh')
    expect(saved.kernel.autoClose).toBe(false)
    expect(saved.kernel.cardColumns).toBe(3)
    for (const key of ['realMemoryUsage', 'testUrl', 'testTimeout', 'concurrencyLimit']) {
      expect(saved.kernel).not.toHaveProperty(key)
      expect(settings.app.kernel).not.toHaveProperty(key)
    }
    expect(saved.connections.visibility['connection.domain']).toBe(false)
    expect(saved.connections.order.slice(0, 2)).toEqual(['uplinkTotal', 'connection.domain'])
  })
})
