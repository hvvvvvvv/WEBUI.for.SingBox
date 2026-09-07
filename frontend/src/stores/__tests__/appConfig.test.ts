import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { nextTick } from 'vue'
import { create } from '@bufbuild/protobuf'
import { AppConfigSchema } from '../../../gen/app/v1/app_pb'

const rpc = vi.hoisted(() => ({ getAppConfig: vi.fn(), saveAppConfig: vi.fn() }))
vi.mock('@/bridge', () => ({ createRpcClient: () => rpc }))
vi.mock('@/utils', () => ({
  deepClone: (value: unknown) => JSON.parse(JSON.stringify(value)),
  debounce: (fn: (...args: any[]) => Promise<any>) => Object.assign(fn, { cancel: vi.fn() }),
}))

import { useAppConfigStore } from '../appConfig'

describe('core log retention persistence', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.resetAllMocks()
    rpc.getAppConfig.mockResolvedValue({ config: create(AppConfigSchema) })
  })

  it('defaults to disabled and round-trips saved values', async () => {
    const store = useAppConfigStore()
    expect(store.config.coreLogDays).toBe(0)
    await store.setupAppConfig()
    store.config.coreLogDays = 7
    rpc.saveAppConfig.mockImplementation(async ({ config }) => ({ config }))
    await store.saveNow()
    await nextTick()
    expect(rpc.saveAppConfig).toHaveBeenCalledTimes(1)
    expect(rpc.saveAppConfig.mock.calls[0][0].config.coreLogDays).toBe(7)
    expect(store.config.coreLogDays).toBe(7)
  })

  it('restores the authoritative saved value on failure', async () => {
    const store = useAppConfigStore()
    await store.setupAppConfig()
    store.config.coreLogDays = 7
    rpc.saveAppConfig.mockRejectedValue(new Error('save failed'))
    rpc.getAppConfig.mockResolvedValue({ config: create(AppConfigSchema, { coreLogDays: 3 }) })
    await expect(store.saveNow()).rejects.toThrow('save failed')
    await nextTick()
    expect(store.config.coreLogDays).toBe(3)
    expect(rpc.saveAppConfig).toHaveBeenCalledTimes(1)
  })

  it('uses the last saved value if both save and reload fail', async () => {
    const store = useAppConfigStore()
    await store.setupAppConfig()
    store.config.coreLogDays = 7
    rpc.saveAppConfig.mockRejectedValue(new Error('offline'))
    rpc.getAppConfig.mockRejectedValue(new Error('offline'))
    await expect(store.saveNow()).rejects.toThrow('offline')
    await nextTick()
    expect(store.config.coreLogDays).toBe(0)
    expect(rpc.saveAppConfig).toHaveBeenCalledTimes(1)
  })
})
