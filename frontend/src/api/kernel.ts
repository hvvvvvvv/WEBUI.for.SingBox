import { Code, ConnectError, createClient } from '@connectrpc/connect'
import { createGrpcWebTransport } from '@connectrpc/connect-web'

import { checkAuthToken, loadAuthToken, recoverAuthToken } from '@/bridge/http'
import { useAppSettingsStore } from '@/stores'
import { StartedService } from '../../gen/native/daemon/started_service_pb'
import type {
  ClashMode,
  ClashModeStatus,
  Connection,
  Groups,
  OutboundList,
  Log,
  Status,
} from '../../gen/native/daemon/started_service_pb'
import type { NativeConnectionSnapshot } from '@/types/kernel'
import { NativeConnectionsState, NATIVE_INTERVAL } from './nativeConnections'
import { NativeSubscription } from './nativeSubscription'

/** HTTP 401 belongs to the WebUI session; gRPC Unauthenticated belongs to the kernel. */
class WebUISessionError extends ConnectError {
  constructor() {
    super('WebUI authentication required', Code.Unauthenticated, {
      'x-webui-auth-recovery': 'failed',
    })
  }
}
class WebUISessionRecovered extends ConnectError {
  constructor() {
    super('WebUI session recovered; reconnecting stream', Code.Unavailable, {
      'x-webui-auth-recovery': 'recovered',
    })
  }
}
// ConnectError's structural Symbol.hasInstance is inherited by subclasses; use our marker.
const webUIAuthRecovery = (error: unknown) =>
  ConnectError.from(error).metadata.get('x-webui-auth-recovery')

export const authenticatedKernelFetch: typeof fetch = async (input, init) => {
  const settings = useAppSettingsStore()
  if (!settings.sessionInfo.cacheToken && !(await loadAuthToken())) {
    throw new WebUISessionError()
  }
  const send = () => {
    const headers = new Headers(init?.headers)
    headers.set('Authorization', `Bearer ${settings.sessionInfo.cacheToken}`)
    return fetch(input, { ...init, headers })
  }
  let response = await send()
  if (response.status === 401 && !init?.signal?.aborted && (await recoverAuthToken())) {
    response = await send()
  }
  if (response.status === 401) throw new WebUISessionError()
  return response
}

const nativeClient = createClient(
  StartedService,
  createGrpcWebTransport({
    baseUrl: '/api/kernel',
    useBinaryFormat: true,
    fetch: authenticatedKernelFetch,
  }),
)

type NativeInstance = { pid: number; generation: number; controller: AbortController }
let instance: NativeInstance | undefined
let generation = 0

export const getNativeApiGeneration = () => generation
const requestOptions = () => ({ signal: instance?.controller.signal, timeoutMs: 10000 })

const handleAuthenticationError = async (error: unknown) => {
  if (webUIAuthRecovery(error) === 'failed') return false
  if (ConnectError.from(error).code === Code.Unauthenticated && !(await checkAuthToken())) {
    return recoverAuthToken()
  }
  return false
}

const unary = async <T>(
  operation: (options: ReturnType<typeof requestOptions>) => Promise<T>,
): Promise<T> => {
  const current = instance
  const options = requestOptions()
  let response: T
  try {
    response = await operation(options)
  } catch (error) {
    if (options.signal?.aborted || !(await handleAuthenticationError(error))) throw error
    response = await operation(options)
  }
  if (current !== instance || options.signal?.aborted) {
    throw new ConnectError('Kernel instance changed', Code.Canceled)
  }
  return response
}

async function* authenticatedStream<T>(
  open: () => AsyncIterable<T>,
  signal: AbortSignal,
): AsyncGenerator<T> {
  try {
    yield* open()
    if (!signal.aborted) throw new ConnectError('Kernel stream ended', Code.Unavailable)
  } catch (error) {
    if (!signal.aborted && webUIAuthRecovery(error) !== 'failed' && !(await checkAuthToken())) {
      if (await recoverAuthToken()) {
        throw new WebUISessionRecovered()
      }
      throw new WebUISessionError()
    }
    throw error
  }
}

const groups = new NativeSubscription((signal) =>
  authenticatedStream(() => nativeClient.subscribeGroups({}, { signal }), signal),
)
const outbounds = new NativeSubscription((signal) =>
  authenticatedStream(() => nativeClient.subscribeOutbounds({}, { signal }), signal),
)
const modes = new NativeSubscription((signal) =>
  authenticatedStream(() => nativeClient.subscribeClashMode({}, { signal }), signal),
)
const status = new NativeSubscription((signal) =>
  authenticatedStream(
    () => nativeClient.subscribeStatus({ interval: NATIVE_INTERVAL }, { signal }),
    signal,
  ),
)
// Logs are event batches, so replaying the last batch would duplicate history.
const logs = new NativeSubscription((signal) =>
  authenticatedStream(() => nativeClient.subscribeLog({}, { signal }), signal),
)
const connectionState = new NativeConnectionsState()
const connections = new NativeSubscription(async function* (signal) {
  connectionState.clear()
  for await (const batch of authenticatedStream(
    () => nativeClient.subscribeConnections({ interval: NATIVE_INTERVAL }, { signal }),
    signal,
  )) {
    if (signal.aborted) return
    yield connectionState.apply(batch)
  }
})
const channels = [groups, outbounds, modes, status, logs, connections]

export const startNativeApi = (pid: number) => {
  if (instance?.pid === pid && !instance.controller.signal.aborted) return
  stopNativeApi()
  instance = { pid, generation: ++generation, controller: new AbortController() }
  for (const channel of channels) channel.setInstance(instance.controller.signal)
}

export const stopNativeApi = () => {
  generation++
  instance?.controller.abort()
  instance = undefined
  connectionState.clear()
  for (const channel of channels) channel.setInstance()
}

export const onGroups = (handler: (data: Groups) => void) => groups.subscribe(handler)
export const onOutbounds = (handler: (data: OutboundList) => void, replay = true) =>
  outbounds.subscribe(handler, replay)
export const onClashMode = (handler: (data: ClashMode) => void) => modes.subscribe(handler)
export const onStatus = (handler: (data: Status) => void) => status.subscribe(handler)
export const onLogs = (handler: (data: Log) => void) => logs.subscribe(handler, false)
export const onConnections = (handler: (data: NativeConnectionSnapshot) => void) =>
  connections.subscribe(handler)
export const clearClosedConnections = () => connections.publish(connectionState.clearClosed())
export const getGroupsSnapshot = () => groups.first()
export const getOutboundsSnapshot = () => outbounds.first()
export const getClashModeStatus = () =>
  unary((options) => nativeClient.getClashModeStatus({}, options))
export const setClashMode = async (mode: string): Promise<ClashModeStatus> => {
  const current = instance
  await unary((options) => nativeClient.setClashMode({ mode }, options))
  const result = await getClashModeStatus()
  if (current !== instance) throw new ConnectError('Kernel instance changed', Code.Canceled)
  if (result.currentMode.toLowerCase() !== mode.toLowerCase()) {
    throw new ConnectError(
      'Kernel mode readback does not match the requested mode',
      Code.FailedPrecondition,
    )
  }
  return result
}
export const selectOutbound = async (groupTag: string, outboundTag: string) => {
  const current = instance
  const response = await unary((options) =>
    nativeClient.selectOutbound({ groupTag, outboundTag }, options),
  )
  await groups.waitFor((value) =>
    value.group.some((group) => group.tag === groupTag && group.selected === outboundTag),
  )
  if (current !== instance) throw new ConnectError('Kernel instance changed', Code.Canceled)
  return response
}
export const urlTest = (outboundTag: string) =>
  unary((options) => nativeClient.uRLTest({ outboundTag }, options))
export const closeConnection = (id: string) =>
  unary((options) => nativeClient.closeConnection({ id }, options))
export const closeAllConnections = () =>
  unary((options) => nativeClient.closeAllConnections({}, options))

/** The first response is an authoritative snapshot. Cancel immediately after consuming it. */
export const getFreshConnectionsSnapshot = async (): Promise<Connection[]> => {
  const controller = new AbortController()
  const current = instance
  const abort = () => controller.abort()
  current?.controller.signal.addEventListener('abort', abort, { once: true })
  if (current?.controller.signal.aborted) controller.abort()
  try {
    for (let attempt = 0; attempt < 2; attempt++) {
      try {
        for await (const batch of authenticatedStream(
          () =>
            nativeClient.subscribeConnections(
              { interval: NATIVE_INTERVAL },
              { signal: controller.signal, timeoutMs: 10000 },
            ),
          controller.signal,
        )) {
          if (current !== instance || controller.signal.aborted) {
            throw new ConnectError('Kernel instance changed', Code.Canceled)
          }
          if (!batch.reset) continue
          return batch.events.flatMap((event) =>
            event.connection && event.connection.closedAt === 0n && event.closedAt === 0n
              ? [event.connection]
              : [],
          )
        }
      } catch (error) {
        if (webUIAuthRecovery(error) === 'recovered' && attempt === 0 && !controller.signal.aborted)
          continue
        throw error
      }
    }
    throw new ConnectError('Native API ended before the connection snapshot', Code.Unavailable)
  } finally {
    controller.abort()
    current?.controller.signal.removeEventListener('abort', abort)
  }
}
