import { Request } from '@/api/request'
import { WebSockets } from '@/api/websocket'
import { useProfilesStore, useAppSettingsStore } from '@/stores'

import type {
  CoreApiConfig,
  CoreApiProxies,
  CoreApiConnections,
  CoreApiWsDataMap,
} from '@/types/kernel'

type WsKey = keyof CoreApiWsDataMap
type WsChannel<K extends WsKey> = {
  url: string
  params?: Recordable
  handlers: Array<(data: CoreApiWsDataMap[K]) => void>
  isActive: boolean
  connect?: () => void
  disconnect?: () => void
}

export enum Api {
  Configs = '/configs',
  Memory = '/memory',
  Proxies = '/proxies',
  ProxyDelay = '/proxies/{0}/delay',
  Connections = '/connections',
  Traffic = '/traffic',
  Logs = '/logs',
}

const normalizeKernelAddress = (controller: string) => {
  const fallback = '127.0.0.1:20123'
  const raw = String(controller || '').trim()
  if (!raw) return fallback

  // Accept forms like:
  // - 127.0.0.1:20123
  // - :20123
  // - http://127.0.0.1:20123
  // - https://0.0.0.0:20123
  if (raw.startsWith(':')) {
    return `127.0.0.1${raw}`
  }

  if (raw.includes('://')) {
    try {
      const u = new URL(raw)
      if (u.port) return `127.0.0.1:${u.port}`
      return fallback
    } catch {
      return fallback
    }
  }

  // Host-only values are invalid for kernel proxy target, keep default port.
  if (!raw.includes(':')) {
    return fallback
  }

  const port = raw.split(':').pop()
  if (!port || !/^\d+$/.test(port)) return fallback
  return `127.0.0.1:${port}`
}


const setupCoreApi = (protocol: 'http' | 'ws') => {
  const { currentProfile: profile } = useProfilesStore()
  const appSettings = useAppSettingsStore()

  let kernelAddress = '127.0.0.1:20123'
  let kernelBearer = ''

  if (profile) {
    // Keep backward compatibility for both legacy snake_case and generated camelCase shapes.
    const experimental = (profile as any).experimental
    const clashApi = experimental?.clash_api ?? experimental?.clashApi
    const controller = clashApi?.external_controller ?? clashApi?.externalController ?? ''
    kernelAddress = normalizeKernelAddress(controller)
    kernelBearer = clashApi?.secret ?? ''
  }

  if (protocol === 'http') {
    request.base = '/api/kernel'
    request.bearer = appSettings.sessionInfo.cacheToken
    request.customHeaders = {
      'X-Kernel-Target': kernelAddress,
      'X-Kernel-Bearer': kernelBearer,
    }
  } else {
    const wsProto = location.protocol === 'https:' ? 'wss:' : 'ws:'
    websocket.base = `${wsProto}//${location.host}/ws/kernel`
    websocket.bearer = kernelBearer
    websocket.customParams = {
      target: kernelAddress,
      auth: appSettings.sessionInfo.cacheToken,
    }
  }
}

const request = new Request({ beforeRequest: () => setupCoreApi('http'), timeout: 60 * 1000 })
const websocket = new WebSockets({ beforeConnect: () => setupCoreApi('ws') })

const wsChannels: {
  [K in WsKey]: WsChannel<K>
} = {
  logs: { url: Api.Logs, isActive: false, handlers: [], params: { level: 'debug' } },
  memory: { url: Api.Memory, isActive: false, handlers: [] },
  traffic: { url: Api.Traffic, isActive: false, handlers: [] },
  connections: { url: Api.Connections, isActive: false, handlers: [] },
}

const createCoreWSHandlerRegister = <K extends WsKey>(key: K) => {
  const channel = wsChannels[key]

  return (cb: (data: CoreApiWsDataMap[K]) => void) => {
    channel.handlers.push(cb)

    if (!channel.isActive && channel.connect) {
      channel.connect()
      channel.isActive = true
    }

    const unregister = () => {
      const idx = channel.handlers.indexOf(cb)
      idx !== -1 && channel.handlers.splice(idx, 1)
      if (channel.isActive && channel.disconnect && channel.handlers.length === 0) {
        channel.disconnect()
        channel.isActive = false
      }
    }
    return unregister
  }
}

// restful api
export const getConfigs = () => request.get<CoreApiConfig>(Api.Configs)
export const setConfigs = (body = {}) => request.patch<null>(Api.Configs, body)
export const getProxies = () => request.get<CoreApiProxies>(Api.Proxies)
export const getConnections = () => request.get<CoreApiConnections>(Api.Connections)
export const deleteConnection = (id: string) => request.delete<null>(Api.Connections + '/' + id)
export const useProxy = (group: string, proxy: string) => {
  return request.put<null>(Api.Proxies + '/' + group, { name: proxy })
}
export const getProxyDelay = (proxy: string, url: string, timeout: number) => {
  return request.get<Record<string, number>>(Api.ProxyDelay.replace('{0}', proxy), {
    url,
    timeout,
  })
}

// websocket api
export const onLogs = createCoreWSHandlerRegister('logs')
export const onMemory = createCoreWSHandlerRegister('memory')
export const onTraffic = createCoreWSHandlerRegister('traffic')
export const onConnections = createCoreWSHandlerRegister('connections')
export const initWebsocket = () => {
  Object.values(wsChannels).forEach((channel) => {
    channel.disconnect?.()
    const { connect, disconnect } = websocket.createWS({
      url: channel.url,
      params: channel.params,
      cb: (data) => channel.handlers.forEach((cb) => cb(data)),
    })
    channel.connect = connect
    channel.disconnect = disconnect
    channel.isActive = false
    if (channel.handlers.length > 0) {
      channel.connect()
      channel.isActive = true
    }
  })
}
export const destroyWebsocket = () => {
  Object.values(wsChannels).forEach((channel) => {
    channel.disconnect?.()
    channel.connect = undefined
    channel.disconnect = undefined
    channel.isActive = false
  })
}
