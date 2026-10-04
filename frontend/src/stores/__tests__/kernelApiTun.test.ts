import { create, fromBinary, toBinary } from '@bufbuild/protobuf'
import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { CoreStatus } from '../../../gen/kernel/v1/kernel_pb'
import { ProfileSchema } from '../../../gen/profile/v1/profile_pb'

const mocks = vi.hoisted(() => ({
  rpc: {
    getCurrentProfile: vi.fn(),
    getCoreStatus: vi.fn(),
    stopCore: vi.fn(),
    restartCore: vi.fn(),
  },
  getClashModeStatus: vi.fn(),
  getGroupsSnapshot: vi.fn(),
  getOutboundsSnapshot: vi.fn(),
  setClashMode: vi.fn(),
  events: vi.fn(),
  nativeGeneration: 1,
  toProto: vi.fn(),
  fromProto: vi.fn(),
}))

vi.mock('@/bridge', () => ({ createRpcClient: () => mocks.rpc, EventsOn: mocks.events }))
vi.mock('@/api/kernel', () => ({
  getClashModeStatus: mocks.getClashModeStatus,
  getGroupsSnapshot: mocks.getGroupsSnapshot,
  getOutboundsSnapshot: mocks.getOutboundsSnapshot,
  setClashMode: mocks.setClashMode,
  getNativeApiGeneration: () => mocks.nativeGeneration,
  onGroups: vi.fn(),
  onOutbounds: vi.fn(),
  onClashMode: vi.fn(),
  onLogs: vi.fn(),
  onStatus: vi.fn(),
  onConnections: vi.fn(),
  clearClosedConnections: vi.fn(),
  startNativeApi: () => {
    mocks.nativeGeneration++
  },
  stopNativeApi: () => {
    mocks.nativeGeneration++
  },
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
    mocks.nativeGeneration = 1
    mocks.toProto.mockImplementation(iProfileToProto)
    mocks.fromProto.mockImplementation(protoProfileToIProfile)
    mocks.getClashModeStatus.mockResolvedValue({
      currentMode: 'Rule',
      modeList: ['Rule', 'Global', 'Direct'],
    })
    mocks.getGroupsSnapshot.mockResolvedValue({ group: [] })
    mocks.getOutboundsSnapshot.mockResolvedValue({ outbounds: [] })
    mocks.rpc.stopCore.mockResolvedValue({})
    mocks.rpc.restartCore.mockResolvedValue({ pid: 42 })
    mocks.rpc.getCoreStatus.mockResolvedValue({
      status: CoreStatus.RUNNING,
      pid: 42,
      restartRequired: false,
      restarting: false,
    })
  })

  it('keeps the previous TUN and interface settings after restart preflight fails', async () => {
    const profile = protoProfileToIProfile(undefined)
    const tun = profile.inbounds.find((inbound) => inbound.type === 'tun')!
    tun.enable = false
    tun.tun!.stack = 'system'
    profile.route.default_interface = 'old-interface'
    mocks.rpc.getCurrentProfile.mockResolvedValue({
      profile: create(ProfileSchema, iProfileToProto(profile)),
    })
    const store = useKernelApiStore()
    await store.initCoreState()
    await store.refreshConfig()
    const error = new Error('configuration preflight failed')
    mocks.rpc.restartCore.mockRejectedValue(error)
    await expect(store.updateRuntimeInboundEnable(tun.id, true)).rejects.toBe(
      'configuration preflight failed',
    )
    expect(store.running).toBe(true)
    expect(store.runtimeInbounds.find((inbound) => inbound.id === tun.id)?.enable).toBe(false)
    await expect(
      store.updateConfigs([
        { field: 'tun-stack', value: { stack: 'gvisor' } },
        { field: 'interface-name', value: { interface_name: 'new-interface' } },
      ]),
    ).rejects.toBe('configuration preflight failed')
    expect(store.config.tun.stack).toBe('system')
    expect(store.config['interface-name']).toBe('old-interface')
    expect(mocks.rpc.stopCore).not.toHaveBeenCalled()
  })

  it('does not block stop events behind unresolved initial subscriptions or apply late snapshots', async () => {
    let resolveMode!: (value: { currentMode: string; modeList: string[] }) => void
    let resolveGroups!: (value: { group: any[] }) => void
    mocks.getClashModeStatus.mockReturnValue(
      new Promise((resolve) => {
        resolveMode = resolve
      }),
    )
    mocks.getGroupsSnapshot.mockReturnValue(
      new Promise((resolve) => {
        resolveGroups = resolve
      }),
    )
    const store = useKernelApiStore()
    await store.initCoreState()
    expect(store.running).toBe(true)
    const handler = mocks.events.mock.calls.find(([name]) => name === 'kernelStateChanged')![1]
    handler({ status: CoreStatus.STOPPED, pid: -1 })
    for (let i = 0; i < 10; i++) await Promise.resolve()
    expect(store.running).toBe(false)
    resolveMode({ currentMode: 'stale', modeList: ['stale'] })
    resolveGroups({ group: [{ tag: 'stale' }] })
    for (let i = 0; i < 10; i++) await Promise.resolve()
    expect(store.config.mode).toBe('')
    expect(store.groups).toEqual([])
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

      expect(mocks.rpc.stopCore).not.toHaveBeenCalled()
      expect(mocks.rpc.restartCore).toHaveBeenCalledTimes(3)
      for (const [request] of mocks.rpc.restartCore.mock.calls) {
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
