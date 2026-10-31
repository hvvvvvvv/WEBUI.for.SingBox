import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { nextTick } from 'vue'
import { create } from '@bufbuild/protobuf'
import { AppConfigSchema } from '@gen/app/v1/app_pb'

const rpc = vi.hoisted(() => ({
  getAppConfig: vi.fn(),
  saveAppConfig: vi.fn(),
  testNetworkRequest: vi.fn(),
}))
vi.mock('@/bridge', () => ({ createRpcClient: () => rpc }))
vi.mock('@/utils', () => ({
  deepClone: (value: unknown) => JSON.parse(JSON.stringify(value)),
  debounce: (fn: (...args: any[]) => Promise<any>) => Object.assign(fn, { cancel: vi.fn() }),
}))

import { useAppConfigStore } from '@/stores/appConfig'

describe('core log retention persistence', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.resetAllMocks()
    rpc.getAppConfig.mockResolvedValue({ config: create(AppConfigSchema) })
  })

  it('restores the authoritative saved value on failure', async () => {
    const store = useAppConfigStore()
    await store.setupAppConfig()
    store.config.coreLogDays = 7
    rpc.saveAppConfig.mockRejectedValue(new Error('save failed'))
    rpc.getAppConfig.mockResolvedValue({
      config: create(AppConfigSchema, { coreLogDays: 3 }),
    })
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

  it('round-trips backend network fields and probes through RPC', async () => {
    const settings = {
      requestProxyMode: 'custom',
      customProxy: 'socks5://127.0.0.1:1080',
      githubDownloadMirror: 'https://mirror.invalid/{url}',
      userAgent: 'router/test',
      githubApiToken: 'api-token',
    }
    rpc.getAppConfig.mockResolvedValue({
      config: create(AppConfigSchema, settings),
    })
    rpc.saveAppConfig.mockImplementation(async ({ config }) => ({ config }))
    rpc.testNetworkRequest.mockResolvedValue({
      statusCode: 204,
      durationMs: 12n,
    })
    const store = useAppConfigStore()
    await store.setupAppConfig()
    expect(store.config).toMatchObject(settings)
    store.config.requestProxyMode = 'none'
    await store.saveNow()
    expect(rpc.saveAppConfig).toHaveBeenCalledWith({
      config: expect.objectContaining({
        ...settings,
        requestProxyMode: 'none',
      }),
    })
    await expect(store.testNetworkRequest('https://probe.invalid/')).resolves.toEqual({
      statusCode: 204,
      durationMs: 12n,
    })
    expect(rpc.testNetworkRequest).toHaveBeenCalledWith({
      url: 'https://probe.invalid/',
    })
  })
})
