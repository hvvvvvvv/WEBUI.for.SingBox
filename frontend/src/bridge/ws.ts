import { useAppSettingsStore } from '@/stores'

import { checkAuthToken, recoverAuthToken } from './http'

type EventCallback = (...data: any[]) => void

const listeners = new Map<string, Set<EventCallback>>()
let ws: WebSocket | null = null
let reconnectTimer: ReturnType<typeof setTimeout> | null = null
let bearerToken: string | null = null
let isCheckingAuth = false

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

  ws = new WebSocket(getWsUrl())

  ws.onmessage = (event) => {
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

  ws.onclose = () => {
    ws = null
    handleClose()
  }

  ws.onerror = () => {
    ws?.close()
  }
}

const scheduleReconnect = () => {
  if (reconnectTimer) clearTimeout(reconnectTimer)
  reconnectTimer = setTimeout(() => {
    reconnectTimer = null
    connect()
  }, 2000)
}

const handleClose = async () => {
  if (isCheckingAuth) return

  isCheckingAuth = true
  const isTokenValid = await checkAuthToken().catch(() => true)
  if (isTokenValid) {
    isCheckingAuth = false
    scheduleReconnect()
    return
  }

  const recovered = await recoverAuthToken()
  isCheckingAuth = false
  if (recovered) {
    bearerToken = useAppSettingsStore().sessionInfo.cacheToken
    connect()
  }
}

export const initWebSocket = (token?: string) => {
  bearerToken = token || bearerToken
  connect()
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

export const EventsEmit = (event: string, ...data: any[]) => {
  if (ws?.readyState === WebSocket.OPEN) {
    ws.send(JSON.stringify({ event, data }))
  }
}
