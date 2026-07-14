import { defineStore } from 'pinia'
import { ref, watch } from 'vue'

import {
  getProxies,
  getConfigs,
  setConfigs,
  onLogs,
  onMemory,
  onConnections,
  onTraffic,
  initWebsocket,
  destroyWebsocket,
} from '@/api/kernel'
import { createRpcClient, EventsOn } from '@/bridge'
import { DefaultInboundHttp, DefaultInboundMixed, DefaultInboundSocks } from '@/constant/profile'
import { Inbound, RulesetType, TunStack } from '@/enums/kernel'
import {
  useProfilesStore,
  useLogsStore,
  useSubscribesStore,
  useRulesetsStore,
  useAppConfigStore,
} from '@/stores'
import { iProfileToProto, protoProfileToIProfile, message, eventBus } from '@/utils'
import { KernelRuntimeService } from '../../gen/kernel/v1/kernel_runtime_service_pb'
import { CoreStatus } from '../../gen/kernel/v1/kernel_pb'

import type { CoreApiConfig, CoreApiProxy } from '@/types/kernel'

export type ProxyType = 'mixed' | 'http' | 'socks'
export type RuntimeConfigField =
  | 'mode'
  | 'inbound'
  | 'http'
  | 'socks'
  | 'mixed'
  | 'allow-lan'
  | 'tun'
  | 'tun-stack'
  | 'tun-device'
  | 'interface-name'
export type RuntimeConfigChange = {
  field: RuntimeConfigField
  value: any
}

const normalizeCoreError = (error: unknown): string => {
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
  const subscribesStore = useSubscribesStore()
  const rulesetsStore = useRulesetsStore()
  const appConfigStore = useAppConfigStore()
  const kernelService = createRpcClient(KernelRuntimeService)

  /** RESTful API */
  const config = ref<CoreApiConfig>({
    port: 0,
    'mixed-port': 0,
    'socks-port': 0,
    'interface-name': '',
    'allow-lan': false,
    mode: '',
    tun: {
      enable: false,
      stack: '',
      device: '',
    },
  })

  let runtimeProfile: IProfile | undefined

  const proxies = ref<Record<string, CoreApiProxy>>({})
  const runtimeInbounds = ref<IInbound[]>([])

  const syncRuntimeInbounds = () => {
    runtimeInbounds.value = runtimeProfile?.inbounds || []
  }

  const syncConfigFromRuntimeProfile = () => {
    if (!runtimeProfile) {
      syncRuntimeInbounds()
      return
    }

    const mixed = runtimeProfile.inbounds.find((v) => v.type === Inbound.Mixed && v.enable)
    const http = runtimeProfile.inbounds.find((v) => v.type === Inbound.Http && v.enable)
    const socks = runtimeProfile.inbounds.find((v) => v.type === Inbound.Socks && v.enable)
    const tun = runtimeProfile.inbounds.find((v) => v.type === Inbound.Tun)

    config.value['mixed-port'] = mixed?.mixed?.listen.listen_port || 0
    config.value['port'] = http?.http?.listen.listen_port || 0
    config.value['socks-port'] = socks?.socks?.listen.listen_port || 0
    config.value['allow-lan'] = [
      mixed?.mixed?.listen.listen,
      http?.http?.listen.listen,
      socks?.socks?.listen.listen,
    ].some((address) => address === '0.0.0.0' || address === '::')

    config.value.tun.enable = !!tun?.enable
    config.value.tun.device = tun?.tun?.interface_name || ''
    config.value.tun.stack = tun?.tun?.stack || ''
    config.value['interface-name'] = runtimeProfile.route.default_interface
    syncRuntimeInbounds()
  }

  const refreshConfig = async () => {
    const _config = await getConfigs()

    config.value = {
      ..._config,
      tun: config.value.tun,
    }

    if (!runtimeProfile) {
      const { profile } = await kernelService.getCurrentProfile({})
      runtimeProfile = protoProfileToIProfile(profile)
    }

    syncConfigFromRuntimeProfile()
  }

  const updateConfigs = async (changes: RuntimeConfigChange[]) => {
    if (changes.length === 0) return
    const patchInbound = () => {
      if (!runtimeProfile) return
      const inbound = runtimeProfile.inbounds.find(
        (v) =>
          (v.type === Inbound.Mixed && v.mixed?.listen.listen_port) ||
          (v.type === Inbound.Http && v.http?.listen.listen_port) ||
          (v.type === Inbound.Socks && v.socks?.listen.listen_port),
      )
      if (!inbound) {
        throw 'home.overview.needPort'
      }
      inbound.enable = true
    }

    const patchInboundPort = (type: 'mixed' | 'socks' | 'http', port: number) => {
      if (!runtimeProfile) return
      let inbound = runtimeProfile.inbounds.find((v) => v.type === type)
      if (inbound) {
        inbound[type]!.listen.listen_port = port
      } else {
        const inboundTemplateMap = {
          [Inbound.Mixed]: DefaultInboundMixed,
          [Inbound.Http]: DefaultInboundHttp,
          [Inbound.Socks]: DefaultInboundSocks,
        }
        const _type = inboundTemplateMap[type]()!
        _type.listen.listen_port = port
        inbound = {
          id: type + '-in',
          tag: type + '-in',
          type: type,
          enable: true,
          [type]: _type,
        }
        runtimeProfile.inbounds.push(inbound)
      }
      inbound.enable = port !== 0
    }

    const patchInboundAddress = (allowLan: boolean) => {
      if (!runtimeProfile) return
      runtimeProfile.inbounds.forEach((inbound) => {
        if (inbound.type === Inbound.Tun) return
        inbound[inbound.type]!.listen.listen = allowLan ? '0.0.0.0' : '127.0.0.1'
      })
    }

    const patchInboundTun = (options: {
      enable?: boolean
      stack?: string
      device?: string
      interface_name?: string
    }) => {
      if (!runtimeProfile) return
      const inbound = runtimeProfile.inbounds.find((v) => v.type === Inbound.Tun)
      if (!inbound) throw 'home.overview.needTun'
      options = { ...config.value.tun, ...options }
      inbound.enable = !!options.enable
      inbound.tun!.stack = options.stack || TunStack.Mixed
      inbound.tun!.interface_name = options.device || ''
      runtimeProfile.route.default_interface = options.interface_name || ''
      runtimeProfile.route.auto_detect_interface = !options.interface_name
    }

    let shouldRestart = false
    const tunOptions = { ...config.value.tun, interface_name: config.value['interface-name'] }

    const fieldHandlerMap: Record<RuntimeConfigField, (value: any) => Promise<void> | void> = {
      mode: async (value) => {
        await setConfigs({ mode: value })
        await refreshConfig()
      },
      inbound: () => patchInbound(),
      http: (value) => patchInboundPort(Inbound.Http, value),
      socks: (value) => patchInboundPort(Inbound.Socks, value),
      mixed: (value) => patchInboundPort(Inbound.Mixed, value),
      'allow-lan': (value) => patchInboundAddress(value),
      tun: (value) => {
        Object.assign(tunOptions, value)
        patchInboundTun(tunOptions)
      },
      'tun-stack': (value) => {
        Object.assign(tunOptions, value)
        patchInboundTun(tunOptions)
      },
      'tun-device': (value) => {
        Object.assign(tunOptions, value)
        patchInboundTun(tunOptions)
      },
      'interface-name': (value) => {
        Object.assign(tunOptions, value)
        patchInboundTun(tunOptions)
      },
    }

    for (const { field, value } of changes) {
      await fieldHandlerMap[field]?.(value)
      if (field !== 'mode') {
        shouldRestart = true
      }
    }

    if (shouldRestart) {
      await restartCore(undefined, true)
      syncConfigFromRuntimeProfile()
    }
  }

  const updateConfig = async (field: RuntimeConfigField, value: any) =>
    updateConfigs([{ field, value }])

  const updateRuntimeInboundEnable = async (inboundId: string, enable: boolean) => {
    const inbound = runtimeProfile?.inbounds.find((v) => v.id === inboundId)
    if (!inbound) throw 'home.overview.needInbound'

    inbound.enable = enable
    await restartCore(undefined, true)
    syncConfigFromRuntimeProfile()
  }

  const refreshProviderProxies = async () => {
    const { proxies: b } = await getProxies()
    proxies.value = b
  }

  const getCurrentCoreMemory = async () => {
    const { rss } = await kernelService.getCurrentCoreMemory({})
    return Number(rss)
  }

  /* Bridge API */
  const corePid = ref(-1)
  const running = ref(false)
  const starting = ref(false)
  const stopping = ref(false)
  const restarting = ref(false)
  const needRestart = ref(false)
  const coreStateLoading = ref(true)
  const coreStatus = ref(CoreStatus.STOPPED)
  let pendingRuntimeProfile: IProfile | undefined
  let coreStateQueue = Promise.resolve()

  const applyCoreState = async (status: CoreStatus, pid: number) => {
    const normalizedPID = status === CoreStatus.RUNNING && pid > 0 ? pid : -1
    const wasRunning = running.value
    const previousPID = corePid.value
    const stateChanged = coreStatus.value !== status || previousPID !== normalizedPID

    coreStatus.value = status
    corePid.value = normalizedPID
    running.value = normalizedPID > 0

    if (running.value) {
      if (pendingRuntimeProfile) {
        runtimeProfile = pendingRuntimeProfile
      }
      if (!wasRunning || previousPID !== normalizedPID) {
        needRestart.value = false
        initWebsocket()
        await Promise.all([refreshConfig(), refreshProviderProxies()])
      }
      return
    }

    destroyWebsocket()
    if (status === CoreStatus.STOPPED || status === CoreStatus.CRASHED) {
      needRestart.value = false
      runtimeProfile = undefined
      syncRuntimeInbounds()
    } else if (stateChanged) {
      runtimeInbounds.value = []
    }
  }

  const enqueueCoreState = (status: CoreStatus, pid: number) => {
    const applying = coreStateQueue.then(() => applyCoreState(status, pid))
    coreStateQueue = applying.catch((error) => {
      console.error('applyCoreState: ', error)
    })
    return applying
  }

  const refreshCoreState = async () => {
    const { status, pid } = await kernelService.getCoreStatus({})
    await enqueueCoreState(status, pid)
  }

  const reconcileCoreStateAfterFailure = async (error: unknown): Promise<never> => {
    try {
      await refreshCoreState()
    } catch (refreshError) {
      console.error('refreshCoreState: ', refreshError)
    }
    throw normalizeCoreError(error)
  }

  const callCoreMutation = async <T>(operation: () => Promise<T>): Promise<T> => {
    try {
      return await operation()
    } catch (error) {
      return await reconcileCoreStateAfterFailure(error)
    }
  }

  const initCoreState = async () => {
    let state: { status: CoreStatus; pid: number }
    try {
      state = await kernelService.getCoreStatus({})
    } catch {
      await enqueueCoreState(CoreStatus.STOPPED, -1)
      coreStateLoading.value = false
      return
    }

    try {
      await enqueueCoreState(state.status, state.pid)
    } catch (error) {
      console.error('applyCoreState: ', error)
    } finally {
      coreStateLoading.value = false
    }
  }

  EventsOn('kernelStateChanged', (state?: { status?: CoreStatus; pid?: number }) => {
    if (typeof state?.status !== 'number') return
    void enqueueCoreState(state.status, typeof state.pid === 'number' ? state.pid : -1).catch(
      () => undefined,
    )
  })

  const startCore = async (_profile?: IProfile) => {
    if (running.value) throw 'The core is already running'

    logsStore.clearKernelLog()

    const { profile: profileID } = appConfigStore.config
    const profile = _profile || profilesStore.getProfileById(profileID)
    if (!profile) throw 'Choose a profile first'

    if (!_profile) {
      runtimeProfile = undefined
      syncRuntimeInbounds()
    }

    starting.value = true
    try {
      const { pid } = await callCoreMutation(() =>
        kernelService.startCore({ profileId: profile.id }),
      )
      await enqueueCoreState(CoreStatus.RUNNING, pid)
    } catch (error) {
      throw normalizeCoreError(error)
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
      throw normalizeCoreError(error)
    } finally {
      stopping.value = false
    }
  }

  const restartCore = async (cleanupTask?: () => Promise<any>, keepRuntimeProfile = false) => {
    restarting.value = true
    try {
      await cleanupTask?.()
      const profile = keepRuntimeProfile ? runtimeProfile : profilesStore.currentProfile
      if (!profile) throw 'Choose a profile first'
      if (keepRuntimeProfile) {
        pendingRuntimeProfile = profile
        await callCoreMutation(() => kernelService.stopCore({}))
        await enqueueCoreState(CoreStatus.STOPPED, -1)
        const { pid } = await callCoreMutation(() =>
          kernelService.startCoreWithProfile({
            profile: iProfileToProto(profile),
          }),
        )
        await enqueueCoreState(CoreStatus.RUNNING, pid)
      } else {
        const { pid } = await callCoreMutation(() =>
          kernelService.restartCore({ profileId: profile.id }),
        )
        await enqueueCoreState(CoreStatus.RUNNING, pid)
      }
    } catch (error) {
      throw normalizeCoreError(error)
    } finally {
      pendingRuntimeProfile = undefined
      needRestart.value = false
      restarting.value = false
    }
  }

  const getProxyPort = ():
    | {
        port: number
        proxyType: ProxyType
      }
    | undefined => {
    const { port, 'socks-port': socksPort, 'mixed-port': mixedPort } = config.value

    if (mixedPort) {
      return {
        port: mixedPort,
        proxyType: 'mixed',
      }
    }
    if (port) {
      return {
        port,
        proxyType: 'http',
      }
    }
    if (socksPort) {
      return {
        port: socksPort,
        proxyType: 'socks',
      }
    }
    return undefined
  }

  eventBus.on('profileChange', ({ id }) => {
    if (running.value && id === appConfigStore.config.profile) {
      needRestart.value = true
    }
  })

  eventBus.on('subscriptionChange', ({ id }) => {
    if (running.value && profilesStore.currentProfile) {
      const inUse = profilesStore.currentProfile.outbounds.some(({ outbounds }) =>
        outbounds.some((outbound) => outbound.type === 'Subscription' && outbound.id === id),
      )
      if (inUse) {
        needRestart.value = true
      }
    }
  })

  eventBus.on('subscriptionsChange', () => {
    if (running.value && profilesStore.currentProfile) {
      const enabledSubs = subscribesStore.subscribes.flatMap((v) => (v.disabled ? [] : v.id))
      const inUse = profilesStore.currentProfile.outbounds.some(({ outbounds }) =>
        outbounds.some(
          (outbound) => outbound.type === 'Subscription' && enabledSubs.includes(outbound.id),
        ),
      )
      if (inUse) {
        needRestart.value = true
      }
    }
  })

  const collectRulesetIDs = () => {
    if (!profilesStore.currentProfile) return []
    const l1 = profilesStore.currentProfile.route.rule_set.flatMap((ruleset) =>
      ruleset.type === RulesetType.Local ? ruleset.path : [],
    )
    return l1
  }

  eventBus.on('rulesetChange', ({ id }) => {
    if (running.value && profilesStore.currentProfile) {
      const inUse = profilesStore.currentProfile.route.rule_set.some(
        (ruleset) => ruleset.type === RulesetType.Local && ruleset.path === id,
      )
      if (inUse) {
        needRestart.value = true
      }
    }
  })

  eventBus.on('rulesetsChange', () => {
    if (running.value && profilesStore.currentProfile) {
      const enabledRulesets = rulesetsStore.rulesets.flatMap((v) => (v.disabled ? [] : v.id))
      const inUse = collectRulesetIDs().some((v) => enabledRulesets.includes(v))
      if (inUse) {
        needRestart.value = true
      }
    }
  })

  watch(needRestart, (v) => {
    if (v && appConfigStore.config.autoRestartKernel) {
      restartCore()
    }
  })

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
    proxies,
    runtimeInbounds,
    refreshConfig,
    updateConfig,
    updateConfigs,
    updateRuntimeInboundEnable,
    refreshProviderProxies,
    getProxyPort,
    getCurrentCoreMemory,

    onLogs,
    onMemory,
    onTraffic,
    onConnections,
  }
})
