import { create, fromBinary, toBinary } from '@bufbuild/protobuf'
import { createPinia, setActivePinia } from 'pinia'
import { nextTick } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { CoreStatus } from '@gen/kernel/v1/kernel_pb'
import { ProfileSchema } from '@gen/profile/v1/profile_pb'

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
import { useKernelApiStore } from '@/stores/kernelApi'

const deferred = <T>() => {
  let resolve!: (value: T) => void
  let reject!: (reason: unknown) => void
  const promise = new Promise<T>((accept, fail) => {
    resolve = accept
    reject = fail
  })
  return { promise, resolve, reject }
}

const loadRunningProfile = async (profile: IProfile) => {
  mocks.rpc.getCurrentProfile.mockResolvedValue({
    profile: create(ProfileSchema, iProfileToProto(profile)),
  })
  const store = useKernelApiStore()
  await store.initCoreState()
  await store.refreshConfig()
  return store
}

const changeDisplayedInbound = async (
  store: ReturnType<typeof useKernelApiStore>,
  inbound: IInbound,
  enable: boolean,
) => {
  // Switch updates v-model synchronously, then emits change on the next tick.
  inbound.enable = enable
  await nextTick()
  return store.updateRuntimeInboundEnable(inbound.id, enable)
}

const decodeRestartProfile = (callIndex: number) => {
  const request = mocks.rpc.restartCore.mock.calls[callIndex]![0]
  const encoded = create(ProfileSchema, request.profile)
  return protoProfileToIProfile(fromBinary(ProfileSchema, toBinary(ProfileSchema, encoded)))
}

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

  it.each([false, true])(
    'restores the displayed TUN value %s after preflight failure without leaking it into the next restart',
    async (previousEnable) => {
      const profile = protoProfileToIProfile(undefined)
      const tun = profile.inbounds.find((inbound) => inbound.type === 'tun')!
      tun.enable = previousEnable
      tun.tun!.stack = 'system'
      tun.tun!.dns_mode = 'native'
      const store = await loadRunningProfile(profile)
      const displayedTun = store.runtimeInbounds.find((inbound) => inbound.id === tun.id)!
      const error = new Error('configuration preflight failed')
      mocks.rpc.restartCore.mockRejectedValueOnce(error)
      await expect(changeDisplayedInbound(store, displayedTun, !previousEnable)).rejects.toBe(
        'configuration preflight failed',
      )

      // A delayed view callback may still hold the object from before rollback.
      displayedTun.enable = previousEnable
      expect(store.running).toBe(true)
      expect(store.pid).toBe(42)
      expect
        .soft(store.runtimeInbounds.find((inbound) => inbound.id === tun.id)?.enable)
        .toBe(previousEnable)

      const displayedMixed = store.runtimeInbounds.find((inbound) => inbound.type === 'mixed')!
      const nextEnable = !displayedMixed.enable
      await changeDisplayedInbound(store, displayedMixed, nextEnable)
      expect(mocks.rpc.restartCore).toHaveBeenCalledTimes(2)
      const restartedProfile = decodeRestartProfile(1)
      const restartedTun = restartedProfile.inbounds.find((inbound) => inbound.id === tun.id)!
      expect.soft(restartedTun.enable).toBe(previousEnable)
      expect(restartedTun.tun!.stack).toBe('system')
      expect(restartedTun.tun!.dns_mode).toBe('native')
      expect(
        restartedProfile.inbounds.find((inbound) => inbound.id === displayedMixed.id)?.enable,
      ).toBe(nextEnable)
      expect
        .soft(store.runtimeInbounds.find((inbound) => inbound.id === tun.id)?.enable)
        .toBe(previousEnable)
      expect(store.runtimeInbounds.find((inbound) => inbound.id === tun.id)?.tun?.stack).toBe(
        'system',
      )
      expect(mocks.rpc.stopCore).not.toHaveBeenCalled()
    },
  )

  it.each(['stopped', 'replaced'] as const)(
    'does not restore or mutate obsolete inbound settings after a late failure when the core is %s',
    async (transition) => {
      const profile = protoProfileToIProfile(undefined)
      const tun = profile.inbounds.find((inbound) => inbound.type === 'tun')!
      tun.enable = false
      tun.tun!.stack = 'system'
      const store = await loadRunningProfile(profile)
      const displayedTun = store.runtimeInbounds.find((inbound) => inbound.id === tun.id)!
      const request = deferred<{ pid: number }>()
      mocks.rpc.restartCore.mockReturnValueOnce(request.promise)
      const changed = changeDisplayedInbound(store, displayedTun, true)
      const rejected = expect(changed).rejects.toBe('late restart failure')
      await vi.waitFor(() => expect(mocks.rpc.restartCore).toHaveBeenCalledTimes(1))

      const status = transition === 'stopped' ? CoreStatus.STOPPED : CoreStatus.RUNNING
      const pid = transition === 'stopped' ? -1 : 84
      mocks.rpc.getCoreStatus.mockResolvedValue({
        status,
        pid,
        restartRequired: false,
        restarting: false,
      })
      const handler = mocks.events.mock.calls.find(([name]) => name === 'kernelStateChanged')![1]
      handler({ status, pid })
      await vi.waitFor(() => expect(store.pid).toBe(pid))
      if (transition === 'stopped') {
        expect(store.runtimeInbounds).toEqual([])
      } else {
        // The RUNNING event adopts the profile submitted by the pending restart.
        expect(store.runtimeInbounds.find((inbound) => inbound.id === tun.id)?.enable).toBe(true)
      }

      request.reject(new Error('late restart failure'))
      await rejected
      displayedTun.enable = false
      expect(store.pid).toBe(pid)
      expect(store.running).toBe(transition === 'replaced')
      if (transition === 'stopped') {
        expect(store.runtimeInbounds).toEqual([])
      } else {
        const currentTun = store.runtimeInbounds.find((inbound) => inbound.id === tun.id)!
        expect(currentTun.enable).toBe(true)
        expect(currentTun.tun!.stack).toBe('system')
      }
    },
  )

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

  it('preserves TUN stack and DNS settings through successful displayed changes and protobuf serialization', async () => {
    const mode = 'native' as const
    const profile = protoProfileToIProfile(undefined)
    const tunInbound = profile.inbounds.find((inbound) => inbound.type === 'tun')!
    tunInbound.tun!.dns_mode = mode
    tunInbound.tun!.stack = 'system'

    const store = await loadRunningProfile(profile)
    for (const enable of [true, false]) {
      const displayedTun = store.runtimeInbounds.find((inbound) => inbound.id === tunInbound.id)!
      await changeDisplayedInbound(store, displayedTun, enable)
      expect(store.runtimeInbounds.find((inbound) => inbound.id === tunInbound.id)?.enable).toBe(
        enable,
      )
    }

    expect(mocks.rpc.stopCore).not.toHaveBeenCalled()
    expect(mocks.rpc.restartCore).toHaveBeenCalledTimes(2)
    for (const [index, enable] of [true, false].entries()) {
      const restoredTun = decodeRestartProfile(index).inbounds.find(
        (inbound) => inbound.id === tunInbound.id,
      )!
      expect(restoredTun.enable).toBe(enable)
      expect(restoredTun.tun!.dns_mode).toBe(mode)
      expect(restoredTun.tun!.stack).toBe('system')
    }
    const finalTun = store.runtimeInbounds.find((inbound) => inbound.type === 'tun')!
    expect(finalTun.enable).toBe(false)
    expect(finalTun.tun!.dns_mode).toBe(mode)
    expect(finalTun.tun!.stack).toBe('system')
  })
})
