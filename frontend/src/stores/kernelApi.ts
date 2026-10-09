import { defineStore } from 'pinia'
import { computed, ref, shallowRef } from 'vue'

import { Code, ConnectError } from '@connectrpc/connect'

import {
  getClashModeStatus,
  getGroupsSnapshot,
  getOutboundsSnapshot,
  getNativeApiGeneration,
  onGroups,
  onOutbounds,
  onClashMode,
  onLogs,
  onStatus,
  onConnections,
  clearClosedConnections,
  startNativeApi,
  stopNativeApi,
} from '@/api/kernel'
import { createRpcClient, EventsOn } from '@/bridge'
import { useProfilesStore, useLogsStore, useAppConfigStore } from '@/stores'
import { iProfileToProto, protoProfileToIProfile } from '@/utils'
import { deepClone } from '@/utils/others'
import { KernelRuntimeService } from '../../gen/kernel/v1/kernel_runtime_service_pb'
import { CoreStatus } from '../../gen/kernel/v1/kernel_pb'

import type { RuntimeKernelConfig, Group, GroupItem } from '@/types/kernel'

type CoreOperation = 'start' | 'stop' | 'restart'

const coreErrorReasonHeader = 'X-Core-Error-Reason'
const coreAPIUnavailable = 'core-api-not-ready'

const normalizeCoreError = (error: unknown, operation: CoreOperation): string => {
  if (
    error instanceof ConnectError &&
    error.metadata.get(coreErrorReasonHeader) === coreAPIUnavailable
  ) {
    if (operation === 'start') return 'kernel.startFailedCheckLogs'
    if (operation === 'restart') return 'kernel.restartFailedCheckLogs'
  }

  if (typeof error === 'string') {
    return error
  }

  if (error && typeof error === 'object') {
    const obj = error as Record<string, any>
    if (typeof obj.rawMessage === 'string' && obj.rawMessage.trim() !== '') {
      return obj.rawMessage
    }
    if (typeof obj.message === 'string' && obj.message.trim() !== '') {
      return obj.message
    }
  }

  return 'Unknown core error'
}

export const useKernelApiStore = defineStore('kernelApi', () => {
  const logsStore = useLogsStore()
  const profilesStore = useProfilesStore()
  const appConfigStore = useAppConfigStore()
  const kernelService = createRpcClient(KernelRuntimeService)

  /** Runtime settings and native API state */
  const config = ref<RuntimeKernelConfig>({
    mode: '',
    modeList: [],
  })

  let runtimeProfile: IProfile | undefined

  const groups = shallowRef<Group[]>([])
  const outbounds = shallowRef<GroupItem[]>([])
  onClashMode((value) => {
    config.value.mode = value.mode
  })
  const subscribeGroups = () => {
    const unregisterGroups = onGroups((value) => {
      groups.value = value.group
    })
    const unregisterOutbounds = onOutbounds((value) => {
      outbounds.value = value.outbounds
    })
    return () => {
      unregisterGroups()
      unregisterOutbounds()
    }
  }
  const runtimeInbounds = ref<IInbound[]>([])

  const syncRuntimeInbounds = () => {
    // Switch edits must not change the profile used for restart rollback.
    runtimeInbounds.value = deepClone(runtimeProfile?.inbounds || [])
  }

  const refreshConfig = async () => {
    const generation = getNativeApiGeneration()
    const mode = await getClashModeStatus()
    if (generation !== getNativeApiGeneration()) return
    config.value.mode = mode.currentMode
    config.value.modeList = mode.modeList
    if (!runtimeProfile) {
      const { profile } = await kernelService.getCurrentProfile({})
      if (generation !== getNativeApiGeneration()) return
      runtimeProfile = protoProfileToIProfile(profile)
    }
    syncRuntimeInbounds()
  }

  let configMutationQueue = Promise.resolve()
  const serializeProfileMutation = (operation: () => Promise<void>) => {
    const requestedGeneration = getNativeApiGeneration()
    const result = configMutationQueue.then(async () => {
      if (requestedGeneration !== getNativeApiGeneration())
        throw new ConnectError('Kernel instance changed', Code.Canceled)
      const previousProfile = runtimeProfile ? deepClone(runtimeProfile) : undefined
      const generation = getNativeApiGeneration()
      try {
        await operation()
      } catch (error) {
        // A failed preflight leaves the old process running; restore its visible settings.
        if (generation === getNativeApiGeneration() && previousProfile) {
          runtimeProfile = previousProfile
          syncRuntimeInbounds()
        }
        throw error
      }
    })
    configMutationQueue = result.catch(() => undefined)
    return result
  }
  const updateRuntimeInboundEnable = (inboundId: string, enable: boolean) =>
    serializeProfileMutation(async () => {
      const inbound = runtimeProfile?.inbounds.find((v) => v.id === inboundId)
      if (!inbound) throw 'home.overview.needInbound'

      inbound.enable = enable
      await restartCore(true)
      syncRuntimeInbounds()
    })

  const refreshProviderProxies = async () => {
    const generation = getNativeApiGeneration()
    const [groupSnapshot, outboundSnapshot] = await Promise.all([
      getGroupsSnapshot(),
      getOutboundsSnapshot(),
    ])
    if (generation !== getNativeApiGeneration()) return
    groups.value = groupSnapshot.group
    outbounds.value = outboundSnapshot.outbounds
  }

  /* Bridge API */
  const corePid = ref(-1)
  const running = ref(false)
  const starting = ref(false)
  const stopping = ref(false)
  const localRestarting = ref(false)
  const backendRestarting = ref(false)
  const restarting = computed(() => localRestarting.value || backendRestarting.value)
  const needRestart = ref(false)
  const coreStateLoading = ref(true)
  const coreStatus = ref(CoreStatus.STOPPED)
  let pendingRuntimeProfile: IProfile | undefined
  let coreStateQueue = Promise.resolve()

  const applyCoreState = async (
    status: CoreStatus,
    pid: number,
    restartRequired = false,
    restartInProgress = false,
  ) => {
    const normalizedPID = status === CoreStatus.RUNNING && pid > 0 ? pid : -1
    const wasRunning = running.value
    const previousPID = corePid.value
    const stateChanged = coreStatus.value !== status || previousPID !== normalizedPID

    coreStatus.value = status
    corePid.value = normalizedPID
    running.value = normalizedPID > 0
    starting.value = status === CoreStatus.STARTING
    stopping.value = status === CoreStatus.STOPPING
    backendRestarting.value = restartInProgress
    needRestart.value = restartRequired

    if (running.value) {
      if (pendingRuntimeProfile && (!wasRunning || previousPID !== normalizedPID)) {
        runtimeProfile = pendingRuntimeProfile
      } else if (!wasRunning || previousPID !== normalizedPID) {
        runtimeProfile = undefined
      }
      if (!wasRunning || previousPID !== normalizedPID) {
        config.value.mode = ''
        config.value.modeList = []
        groups.value = []
        outbounds.value = []
        syncRuntimeInbounds()
        startNativeApi(normalizedPID)
        const nativeGeneration = getNativeApiGeneration()
        // Initial snapshots are finite; persistent streams never hold up state transitions.
        void Promise.all([refreshConfig(), refreshProviderProxies()]).catch((error) => {
          if (getNativeApiGeneration() === nativeGeneration)
            console.error('Native API initialization:', error)
        })
      }
      return
    }

    stopNativeApi()
    config.value.mode = ''
    config.value.modeList = []
    groups.value = []
    outbounds.value = []
    if (status === CoreStatus.STOPPED || status === CoreStatus.CRASHED) {
      runtimeProfile = undefined
      syncRuntimeInbounds()
    } else if (stateChanged) {
      runtimeInbounds.value = []
    }
  }

  const enqueueCoreState = (
    status: CoreStatus,
    pid: number,
    restartRequired = false,
    restartInProgress = false,
  ) => {
    const applying = coreStateQueue.then(() =>
      applyCoreState(status, pid, restartRequired, restartInProgress),
    )
    coreStateQueue = applying.catch((error) => {
      console.error('applyCoreState: ', error)
    })
    return applying
  }

  const refreshCoreState = async () => {
    const {
      status,
      pid,
      restartRequired,
      restarting: restartInProgress,
    } = await kernelService.getCoreStatus({})
    await enqueueCoreState(status, pid, restartRequired, restartInProgress)
  }

  const reconcileCoreStateAfterFailure = async (error: unknown): Promise<never> => {
    try {
      await refreshCoreState()
    } catch (refreshError) {
      console.error('refreshCoreState: ', refreshError)
    }
    throw error
  }

  const callCoreMutation = async <T>(operation: () => Promise<T>): Promise<T> => {
    try {
      return await operation()
    } catch (error) {
      return await reconcileCoreStateAfterFailure(error)
    }
  }

  const initCoreState = async () => {
    let state: {
      status: CoreStatus
      pid: number
      restartRequired: boolean
      restarting: boolean
    }
    try {
      state = await kernelService.getCoreStatus({})
    } catch {
      await enqueueCoreState(CoreStatus.STOPPED, -1)
      coreStateLoading.value = false
      return
    }

    try {
      await enqueueCoreState(state.status, state.pid, state.restartRequired, state.restarting)
    } catch (error) {
      console.error('applyCoreState: ', error)
    } finally {
      coreStateLoading.value = false
    }
  }

  EventsOn(
    'kernelStateChanged',
    (state?: {
      status?: CoreStatus
      pid?: number
      restartRequired?: boolean
      restarting?: boolean
    }) => {
      if (typeof state?.status !== 'number') return
      void enqueueCoreState(
        state.status,
        typeof state.pid === 'number' ? state.pid : -1,
        state.restartRequired === true,
        state.restarting === true,
      ).catch(() => undefined)
    },
  )

  const startCore = async () => {
    if (running.value) throw 'The core is already running'

    logsStore.clearKernelLog()

    const { profile: profileID } = appConfigStore.config
    const profile = profilesStore.getProfileById(profileID)
    if (!profile) throw 'Choose a profile first'

    runtimeProfile = undefined
    syncRuntimeInbounds()

    starting.value = true
    try {
      const { pid } = await callCoreMutation(() =>
        kernelService.startCore({ profileId: profile.id }),
      )
      await enqueueCoreState(CoreStatus.RUNNING, pid)
    } catch (error) {
      throw normalizeCoreError(error, 'start')
    } finally {
      starting.value = false
    }
  }

  const stopCore = async () => {
    if (!running.value) throw 'The core is not running'

    stopping.value = true
    try {
      await callCoreMutation(() => kernelService.stopCore({}))
      await enqueueCoreState(CoreStatus.STOPPED, -1)
    } catch (error) {
      throw normalizeCoreError(error, 'stop')
    } finally {
      stopping.value = false
    }
  }

  const restartCore = async (keepRuntimeProfile = false) => {
    localRestarting.value = true
    try {
      const profile = keepRuntimeProfile ? runtimeProfile : profilesStore.currentProfile
      if (!profile) throw 'Choose a profile first'
      if (keepRuntimeProfile) {
        pendingRuntimeProfile = profile
        const { pid } = await callCoreMutation(() =>
          kernelService.restartCore({ profile: iProfileToProto(profile) }),
        )
        await enqueueCoreState(CoreStatus.RUNNING, pid)
      } else {
        const { pid } = await callCoreMutation(() =>
          kernelService.restartCore({ profileId: profile.id }),
        )
        await enqueueCoreState(CoreStatus.RUNNING, pid)
      }
    } catch (error) {
      throw normalizeCoreError(error, 'restart')
    } finally {
      pendingRuntimeProfile = undefined
      localRestarting.value = false
    }
  }

  return {
    startCore,
    stopCore,
    restartCore,
    initCoreState,
    pid: corePid,
    running,
    starting,
    stopping,
    restarting,
    needRestart,
    coreStateLoading,
    config,
    groups,
    outbounds,
    subscribeGroups,
    runtimeInbounds,
    refreshConfig,
    updateRuntimeInboundEnable,
    refreshProviderProxies,

    onLogs,
    onStatus,
    clearClosedConnections,
    onConnections,
  }
})
