type EventCallback = (...data: any[]) => void

const listeners = new Map<string, Set<EventCallback>>()
let ws: WebSocket | null = null
let reconnectTimer: ReturnType<typeof setTimeout> | null = null
let bearerToken: string | null = null

const getWsUrl = () => {
  const protocol = location.protocol === 'https:' ? 'wss:' : 'ws:'
  let url = `${protocol}//${location.host}/ws`
  if (bearerToken) {
    url += `?auth=${bearerToken}`
  }
  return url
}

const connect = () => {
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
    if (reconnectTimer) clearTimeout(reconnectTimer)
    reconnectTimer = setTimeout(connect, 2000)
  }

  ws.onerror = () => {
    ws?.close()
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
