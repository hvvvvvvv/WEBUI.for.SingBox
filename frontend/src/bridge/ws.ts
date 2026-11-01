import { useAppSettingsStore } from '@/stores'

import { checkAuthToken, recoverAuthToken } from './http'

type EventCallback = (...data: any[]) => void
type ConnectionCallback = () => void

const listeners = new Map<string, Set<EventCallback>>()
const connectionListeners = new Set<ConnectionCallback>()
let ws: WebSocket | null = null
let reconnectTimer: ReturnType<typeof setTimeout> | null = null
let bearerToken: string | null = null
let connectionVersion = 0
let checkingAuthVersion: number | undefined

const getWsUrl = () => {
  const protocol = location.protocol === 'https:' ? 'wss:' : 'ws:'
  let url = `${protocol}//${location.host}/ws`
  if (bearerToken) {
    url += `?auth=${bearerToken}`
  }
  return url
}

const connect = () => {
  if (ws && (ws.readyState === WebSocket.CONNECTING || ws.readyState === WebSocket.OPEN)) {
    return
  }

  if (reconnectTimer) clearTimeout(reconnectTimer)
  reconnectTimer = null
  const socket = new WebSocket(getWsUrl())
  const version = ++connectionVersion
  ws = socket

  socket.onopen = () => {
    if (ws !== socket) return
    connectionListeners.forEach((callback) => callback())
  }

  socket.onmessage = (event) => {
    if (ws !== socket) return
    try {
      const msg = JSON.parse(event.data)
      const callbacks = listeners.get(msg.event)
      if (callbacks) {
        callbacks.forEach((cb) => cb(...(msg.data || [])))
      }
    } catch (e) {
      console.warn('WS message parse error:', e)
    }
  }

  socket.onclose = () => {
    if (ws !== socket) return
    ws = null
    void handleClose(version)
  }

  socket.onerror = () => {
    if (ws === socket) socket.close()
  }
}

const scheduleReconnect = (version: number) => {
  if (reconnectTimer) clearTimeout(reconnectTimer)
  reconnectTimer = setTimeout(() => {
    reconnectTimer = null
    if (version === connectionVersion) connect()
  }, 2000)
}

const handleClose = async (version: number) => {
  if (checkingAuthVersion === version) return
  checkingAuthVersion = version
  try {
    const isTokenValid = await checkAuthToken().catch(() => true)
    if (version !== connectionVersion) return
    if (isTokenValid) {
      scheduleReconnect(version)
      return
    }
    const recovered = await recoverAuthToken()
    if (version !== connectionVersion) return
    if (recovered) {
      bearerToken = useAppSettingsStore().sessionInfo.cacheToken
      connect()
    }
  } finally {
    if (checkingAuthVersion === version) checkingAuthVersion = undefined
  }
}

export const initWebSocket = (token?: string) => {
  bearerToken = token || bearerToken
  connect()
}

export const onWebSocketConnected = (callback: ConnectionCallback) => {
  connectionListeners.add(callback)
  return () => connectionListeners.delete(callback)
}

export const EventsOn = (event: string, callback: EventCallback) => {
  if (!listeners.has(event)) {
    listeners.set(event, new Set())
  }
  listeners.get(event)!.add(callback)
}

export const EventsOff = (event: string, ...additionalEventNames: string[]) => {
  listeners.delete(event)
  additionalEventNames.forEach((name) => listeners.delete(name))
}
