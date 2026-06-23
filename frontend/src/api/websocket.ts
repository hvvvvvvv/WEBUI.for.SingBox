type WebSocketsOptions = {
  base?: string
  bearer?: string
  beforeConnect?: () => void
  checkAuthToken?: () => Promise<boolean>
  onUnauthorized?: () => Promise<boolean>
}

type Options = { url: string; cb: (data: any) => void; params?: Record<string, any> }

export class WebSockets {
  public base: string
  public bearer: string
  public customParams: Record<string, string> = {}
  public beforeConnect: () => void
  public checkAuthToken: () => Promise<boolean>
  public onUnauthorized: () => Promise<boolean>

  constructor(options: WebSocketsOptions) {
    this.base = options.base || ''
    this.bearer = options.bearer || ''
    this.beforeConnect = options.beforeConnect || (() => 0)
    this.checkAuthToken = options.checkAuthToken || (async () => true)
    this.onUnauthorized = options.onUnauthorized || (async () => false)
  }

  public createWS(options: Options) {
    let isManualClose = false
    let ws: WebSocket | null = null
    let reconnectTimer: ReturnType<typeof setTimeout> | undefined
    let isCheckingAuth = false

    const clearReconnectTimer = () => {
      if (reconnectTimer) {
        clearTimeout(reconnectTimer)
        reconnectTimer = undefined
      }
    }

    const getUrl = () => {
      this.beforeConnect()
      const params = { ...options.params, ...this.customParams }
      if (this.bearer) {
        params.token = this.bearer
      }
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

    const handleClose = async () => {
      if (isManualClose || reconnectTimer || isCheckingAuth) return

      isCheckingAuth = true
      const isTokenValid = await this.checkAuthToken().catch(() => true)
      if (isManualClose) {
        isCheckingAuth = false
        return
      }

      if (isTokenValid) {
        isCheckingAuth = false
        scheduleReconnect()
        return
      }

      const recovered = await this.onUnauthorized()
      isCheckingAuth = false
      if (recovered && !isManualClose) {
        connect()
      }
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
        handleClose()
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
