import { create, fromBinary, toBinary } from '@bufbuild/protobuf'
import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { ProfileSchema } from '../../../gen/profile/v1/profile_pb'

const mocks = vi.hoisted(() => ({
  rpc: {
    getCurrentProfile: vi.fn(),
    stopCore: vi.fn(),
    startCoreWithProfile: vi.fn(),
  },
  getConfigs: vi.fn(),
  getProxies: vi.fn(),
  toProto: vi.fn(),
  fromProto: vi.fn(),
}))

vi.mock('@/bridge', () => ({ createRpcClient: () => mocks.rpc, EventsOn: vi.fn() }))
vi.mock('@/api/kernel', () => ({
  getConfigs: mocks.getConfigs,
  getProxies: mocks.getProxies,
  setConfigs: vi.fn(),
  onLogs: vi.fn(),
  onMemory: vi.fn(),
  onConnections: vi.fn(),
  onTraffic: vi.fn(),
  initWebsocket: vi.fn(),
  destroyWebsocket: vi.fn(),
}))
vi.mock('@/stores', () => ({
  useProfilesStore: () => ({}),
  useLogsStore: () => ({ clearKernelLog: vi.fn() }),
  useAppConfigStore: () => ({ config: {} }),
}))
vi.mock('@/utils', () => ({
  sampleID: () => 'test-id',
  iProfileToProto: mocks.toProto,
  protoProfileToIProfile: mocks.fromProto,
}))
vi.mock('@/lang', () => ({ default: { global: { t: (key: string) => key } } }))
vi.mock('@/utils/others', () => ({
  deepClone: <T>(value: T): T => JSON.parse(JSON.stringify(value)),
  sampleID: () => 'test-id',
}))

import { iProfileToProto, protoProfileToIProfile } from '@/utils/profileRpc'
import { useKernelApiStore } from '../kernelApi'

describe('home TUN shortcuts', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.resetAllMocks()
    mocks.toProto.mockImplementation(iProfileToProto)
    mocks.fromProto.mockImplementation(protoProfileToIProfile)
    mocks.getConfigs.mockResolvedValue({})
    mocks.getProxies.mockResolvedValue({ proxies: {} })
    mocks.rpc.stopCore.mockResolvedValue({})
    mocks.rpc.startCoreWithProfile.mockResolvedValue({ pid: 42 })
  })

  it.each(['disabled', 'native', 'hijack'] as const)(
    'preserves %s when toggling TUN, changing stack, and changing interfaces',
    async (mode) => {
      const profile = protoProfileToIProfile(undefined)
      const tunInbound = profile.inbounds.find((inbound) => inbound.type === 'tun')!
      tunInbound.tun!.dns_mode = mode
      mocks.rpc.getCurrentProfile.mockResolvedValue({
        profile: create(ProfileSchema, iProfileToProto(profile)),
      })

      const store = useKernelApiStore()
      await store.refreshConfig()
      await store.updateRuntimeInboundEnable(tunInbound.id, true)
      await store.updateConfigs([
        { field: 'tun-stack', value: { stack: 'system' } },
        { field: 'tun-device', value: { device: 'tun-test' } },
        { field: 'interface-name', value: { interface_name: 'eth-test' } },
      ])
      await store.updateRuntimeInboundEnable(tunInbound.id, false)

      expect(mocks.rpc.startCoreWithProfile).toHaveBeenCalledTimes(3)
      for (const [request] of mocks.rpc.startCoreWithProfile.mock.calls) {
        const encoded = create(ProfileSchema, request.profile)
        const restored = protoProfileToIProfile(
          fromBinary(ProfileSchema, toBinary(ProfileSchema, encoded)),
        )
        expect(restored.inbounds.find((inbound) => inbound.type === 'tun')!.tun!.dns_mode).toBe(
          mode,
        )
      }
      const finalTun = store.runtimeInbounds.find((inbound) => inbound.type === 'tun')!
      expect(finalTun.enable).toBe(false)
      expect(finalTun.tun!.dns_mode).toBe(mode)
      expect(finalTun.tun!.stack).toBe('system')
      expect(finalTun.tun!.interface_name).toBe('tun-test')
      expect(store.config['interface-name']).toBe('eth-test')
    },
  )
})
