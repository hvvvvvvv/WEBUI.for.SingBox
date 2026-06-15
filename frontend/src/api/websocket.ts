type WebSocketsOptions = {
  base?: string
  bearer?: string
  beforeConnect?: () => void
}

type Options = { url: string; cb: (data: any) => void; params?: Record<string, any> }

export class WebSockets {
  public base: string
  public bearer: string
  public customParams: Record<string, string> = {}
  public beforeConnect: () => void

  constructor(options: WebSocketsOptions) {
    this.base = options.base || ''
    this.bearer = options.bearer || ''
    this.beforeConnect = options.beforeConnect || (() => 0)
  }

  public createWS(options: Options) {
    let isManualClose = false
    let ws: WebSocket | null = null
    let reconnectTimer: ReturnType<typeof setTimeout> | undefined

    const clearReconnectTimer = () => {
      if (reconnectTimer) {
        clearTimeout(reconnectTimer)
        reconnectTimer = undefined
      }
    }

    const getUrl = () => {
      this.beforeConnect()
      const params = { ...options.params, ...this.customParams, token: this.bearer }
      const query = new URLSearchParams(params).toString()
      const url = query ? `${options.url}?${query}` : options.url
      return this.base + url
    }

    const scheduleReconnect = () => {
      if (isManualClose || reconnectTimer) return
      reconnectTimer = setTimeout(() => {
        reconnectTimer = undefined
        connect()
      }, 4000)
    }

    const connect = () => {
      if (ws && (ws.readyState === WebSocket.CONNECTING || ws.readyState === WebSocket.OPEN)) {
        return
      }

      isManualClose = false
      clearReconnectTimer()

      const socket = new WebSocket(getUrl())
      ws = socket

      socket.onmessage = (e) => options.cb(JSON.parse(e.data))
      socket.onclose = () => {
        if (ws === socket) {
          ws = null
        }
        scheduleReconnect()
      }
    }

    const disconnect = () => {
      isManualClose = true
      clearReconnectTimer()
      if (ws) {
        ws.onmessage = null
        ws.onclose = null
        ws.close()
        ws = null
      }
    }

    return { connect, disconnect }
  }
}
