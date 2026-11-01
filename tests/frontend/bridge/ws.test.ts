// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

const auth = vi.hoisted(() => ({ check: vi.fn(), recover: vi.fn() }))
vi.mock('@/bridge/http', () => ({ checkAuthToken: auth.check, recoverAuthToken: auth.recover }))
vi.mock('@/stores', () => ({
  useAppSettingsStore: () => ({ sessionInfo: { cacheToken: 'recovered' } }),
}))

class Socket {
  static CONNECTING = 0
  static OPEN = 1
  static sockets: Socket[] = []
  readyState = Socket.CONNECTING
  onopen = () => {}
  onclose = () => {}
  onerror = () => {}
  onmessage = (_event: { data: string }) => {}
  close = vi.fn(() => {
    this.readyState = 2
  })
  constructor(readonly url: string) {
    Socket.sockets.push(this)
  }
}

beforeEach(() => {
  vi.resetModules()
  vi.resetAllMocks()
  vi.useFakeTimers()
  Socket.sockets = []
  vi.stubGlobal('WebSocket', Socket)
  auth.check.mockResolvedValue(true)
})
afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

it('ignores delayed events from a closing socket after a replacement connects', async () => {
  const { initWebSocket, EventsOn, onWebSocketConnected } = await import('@/bridge/ws')
  const event = vi.fn()
  const connected = vi.fn()
  EventsOn('resourceChanged', event)
  onWebSocketConnected(connected)
  initWebSocket('old')
  const old = Socket.sockets[0]!
  old.readyState = 2
  initWebSocket('new')
  const current = Socket.sockets[1]!
  current.readyState = Socket.OPEN
  current.onopen()

  old.onerror()
  old.onclose()
  old.onopen()
  old.onmessage({ data: JSON.stringify({ event: 'resourceChanged', data: ['old'] }) })
  await vi.advanceTimersByTimeAsync(3000)
  expect(current.close).not.toHaveBeenCalled()
  expect(Socket.sockets).toHaveLength(2)
  expect(auth.check).not.toHaveBeenCalled()
  expect(connected).toHaveBeenCalledOnce()
  expect(event).not.toHaveBeenCalled()

  current.onmessage({ data: JSON.stringify({ event: 'resourceChanged', data: ['current'] }) })
  expect(event).toHaveBeenCalledWith('current')
  current.onclose()
  await vi.advanceTimersByTimeAsync(2000)
  expect(Socket.sockets).toHaveLength(3)
  expect(Socket.sockets[2]!.url).toContain('auth=new')
})

it('discards an old auth check without blocking reconnect of the replacement socket', async () => {
  const { initWebSocket } = await import('@/bridge/ws')
  const checking = Promise.withResolvers<boolean>()
  auth.check.mockReturnValueOnce(checking.promise)
  initWebSocket('old')
  Socket.sockets[0]!.onclose()
  initWebSocket('new')
  Socket.sockets[1]!.onclose()
  await vi.advanceTimersByTimeAsync(2000)
  expect(Socket.sockets).toHaveLength(3)
  checking.resolve(false)
  await vi.advanceTimersByTimeAsync(3000)
  expect(auth.recover).not.toHaveBeenCalled()
  expect(Socket.sockets).toHaveLength(3)
})
