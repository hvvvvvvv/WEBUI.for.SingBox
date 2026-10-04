import { create } from '@bufbuild/protobuf'
import { Code, ConnectError } from '@connectrpc/connect'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import {
  ClashModeStatusSchema,
  ConnectionEventsSchema,
  ConnectionEventType,
  GroupsSchema,
  OutboundListSchema,
} from '../../../gen/native/daemon/started_service_pb'

const mocks = vi.hoisted(() => ({
  settings: { sessionInfo: { cacheToken: 'application-token' } },
  loadAuthToken: vi.fn(),
  recoverAuthToken: vi.fn(),
  checkAuthToken: vi.fn(),
  client: {
    getClashModeStatus: vi.fn(),
    setClashMode: vi.fn(),
    selectOutbound: vi.fn(),
    uRLTest: vi.fn(),
    closeConnection: vi.fn(),
    closeAllConnections: vi.fn(),
    subscribeConnections: vi.fn(),
    subscribeGroups: vi.fn(),
    subscribeOutbounds: vi.fn(),
  },
}))
vi.mock('@/stores', () => ({ useAppSettingsStore: () => mocks.settings }))
vi.mock('@/bridge/http', () => ({
  loadAuthToken: mocks.loadAuthToken,
  recoverAuthToken: mocks.recoverAuthToken,
  checkAuthToken: mocks.checkAuthToken,
}))
vi.mock('@connectrpc/connect', async (load) => ({
  ...(await load<typeof import('@connectrpc/connect')>()),
  createClient: () => mocks.client,
}))
import {
  authenticatedKernelFetch,
  getClashModeStatus,
  getFreshConnectionsSnapshot,
  onConnections,
  onGroups,
  onOutbounds,
  selectOutbound,
  setClashMode,
  startNativeApi,
  stopNativeApi,
  urlTest,
} from '../kernel'

const flush = async () => {
  for (let i = 0; i < 15; i++) await Promise.resolve()
}

beforeEach(() => {
  vi.resetAllMocks()
  mocks.settings.sessionInfo.cacheToken = 'application-token'
  mocks.loadAuthToken.mockResolvedValue(true)
  mocks.checkAuthToken.mockResolvedValue(true)
  mocks.recoverAuthToken.mockImplementation(async () => {
    mocks.settings.sessionInfo.cacheToken = 'recovered-token'
    return true
  })
})
afterEach(() => {
  stopNativeApi()
  vi.unstubAllGlobals()
  vi.useRealTimers()
})

describe('native transport and commands', () => {
  it('injects the WebUI token and retries an HTTP 401 once with a recovered token', async () => {
    const sent: Headers[] = []
    vi.stubGlobal(
      'fetch',
      vi.fn((_input, init: RequestInit) => {
        sent.push(new Headers(init.headers))
        return Promise.resolve(new Response('', { status: sent.length === 1 ? 401 : 200 }))
      }),
    )
    const response = await authenticatedKernelFetch(
      '/api/kernel/daemon.StartedService/GetVersion',
      {
        headers: { 'Content-Type': 'application/grpc-web+proto' },
        body: new Uint8Array([0]),
        method: 'POST',
      },
    )
    expect(response.status).toBe(200)
    expect(sent.map((headers) => headers.get('Authorization'))).toEqual([
      'Bearer application-token',
      'Bearer recovered-token',
    ])
    expect(sent[1]?.get('Content-Type')).toBe('application/grpc-web+proto')
    expect(mocks.recoverAuthToken).toHaveBeenCalledTimes(1)
  })

  it('does not request an application login for upstream gRPC authentication failures', async () => {
    const failure = new ConnectError('upstream secret mismatch', Code.Unauthenticated)
    mocks.client.getClashModeStatus.mockRejectedValue(failure)
    await expect(getClashModeStatus()).rejects.toBe(failure)
    expect(mocks.checkAuthToken).toHaveBeenCalledTimes(1)
    expect(mocks.recoverAuthToken).not.toHaveBeenCalled()
  })

  it('stops after a second HTTP 401 without starting a second recovery', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('', { status: 401 })))
    await expect(
      authenticatedKernelFetch('/api/kernel/daemon.StartedService/GetVersion'),
    ).rejects.toMatchObject({ code: Code.Unauthenticated })
    expect(fetch).toHaveBeenCalledTimes(2)
    expect(mocks.recoverAuthToken).toHaveBeenCalledTimes(1)
  })

  it('waits for selected-group readback rather than returning a cached old selection', async () => {
    let update!: (value: ReturnType<typeof create<typeof GroupsSchema>>) => void
    const pending = new Promise<ReturnType<typeof create<typeof GroupsSchema>>>((resolve) => {
      update = resolve
    })
    mocks.client.subscribeGroups.mockReturnValue(
      (async function* () {
        yield create(GroupsSchema, { group: [{ tag: 'group', selectable: true, selected: 'old' }] })
        yield await pending
        await new Promise(() => {})
      })(),
    )
    mocks.client.selectOutbound.mockResolvedValue({})
    const unregister = onGroups(vi.fn())
    startNativeApi(1)
    await flush()
    let resolved = false
    const selection = selectOutbound('group', 'new').then(() => {
      resolved = true
    })
    await flush()
    expect(resolved).toBe(false)
    update(create(GroupsSchema, { group: [{ tag: 'group', selectable: true, selected: 'new' }] }))
    await selection
    expect(resolved).toBe(true)
    unregister()
  })

  it('checks mode readback case-insensitively, and rejects mismatched readback', async () => {
    mocks.client.setClashMode.mockResolvedValue({})
    mocks.client.getClashModeStatus.mockResolvedValue(
      create(ClashModeStatusSchema, {
        currentMode: 'Custom Mode',
        modeList: ['Custom Mode', 'direct'],
      }),
    )
    await expect(setClashMode('custom mode')).resolves.toMatchObject({ currentMode: 'Custom Mode' })
    await expect(setClashMode('direct')).rejects.toMatchObject({ code: Code.FailedPrecondition })
  })

  it('replays the latest outbound snapshot to new consumers by default', async () => {
    const initial = create(OutboundListSchema, {
      outbounds: [{ tag: 'proxy', urlTestTime: 1n, urlTestDelay: 100 }],
    })
    mocks.client.subscribeOutbounds.mockReturnValue(
      (async function* () {
        yield initial
        await new Promise(() => {})
      })(),
    )
    const existing = vi.fn()
    const unregisterExisting = onOutbounds(existing)
    const newConsumer = vi.fn()
    let unregisterNew = () => {}
    try {
      startNativeApi(1)
      await flush()
      expect(existing).toHaveBeenCalledWith(initial)
      unregisterNew = onOutbounds(newConsumer)
      expect(newConsumer).toHaveBeenCalledExactlyOnceWith(initial)
      expect(mocks.client.subscribeOutbounds).toHaveBeenCalledTimes(1)
    } finally {
      unregisterNew()
      unregisterExisting()
    }
  })

  it('can skip outbound replay while receiving new frames until unsubscribed', async () => {
    type Snapshot = ReturnType<typeof create<typeof OutboundListSchema>>
    const initial = create(OutboundListSchema, {
      outbounds: [{ tag: 'proxy', urlTestTime: 1n, urlTestDelay: 100 }],
    })
    const next = create(OutboundListSchema, {
      outbounds: [{ tag: 'proxy', urlTestTime: 2n, urlTestDelay: 90 }],
    })
    const later = create(OutboundListSchema, {
      outbounds: [{ tag: 'proxy', urlTestTime: 3n, urlTestDelay: 80 }],
    })
    let update!: (value: Snapshot) => void
    let updateLater!: (value: Snapshot) => void
    const pending = new Promise<Snapshot>((resolve) => {
      update = resolve
    })
    const pendingLater = new Promise<Snapshot>((resolve) => {
      updateLater = resolve
    })
    mocks.client.subscribeOutbounds.mockReturnValue(
      (async function* () {
        yield initial
        yield await pending
        yield await pendingLater
        await new Promise(() => {})
      })(),
    )
    const existing = vi.fn()
    const unregisterExisting = onOutbounds(existing)
    const newConsumer = vi.fn()
    let unregisterNew = () => {}
    try {
      startNativeApi(1)
      await flush()
      expect(existing).toHaveBeenCalledExactlyOnceWith(initial)
      unregisterNew = onOutbounds(newConsumer, false)
      expect(newConsumer).not.toHaveBeenCalled()
      update(next)
      await flush()
      expect(newConsumer).toHaveBeenCalledExactlyOnceWith(next)
      unregisterNew()
      updateLater(later)
      await flush()
      expect(existing).toHaveBeenLastCalledWith(later)
      expect(newConsumer).toHaveBeenCalledTimes(1)
      expect(mocks.client.subscribeOutbounds).toHaveBeenCalledTimes(1)
    } finally {
      unregisterNew()
      unregisterExisting()
    }
  })

  it('requests asynchronous URL testing by tag without legacy URL or timeout fields', async () => {
    mocks.client.uRLTest.mockResolvedValue({})
    await urlTest('proxy-group')
    expect(mocks.client.uRLTest).toHaveBeenCalledWith(
      { outboundTag: 'proxy-group' },
      expect.objectContaining({ timeoutMs: 10000 }),
    )
  })

  it('reads only active connections from a fresh reset and closes that stream immediately', async () => {
    let signal!: AbortSignal
    mocks.client.subscribeConnections.mockImplementation((_request, options) => {
      signal = options.signal
      return (async function* () {
        yield create(ConnectionEventsSchema, {
          reset: true,
          events: [
            { connection: { id: 'active' } },
            { connection: { id: 'closed', closedAt: 1728000000000n } },
            {
              type: ConnectionEventType.CONNECTION_EVENT_CLOSED,
              closedAt: 1n,
              connection: { id: 'just-closed' },
            },
          ],
        })
      })()
    })
    expect((await getFreshConnectionsSnapshot()).map((connection) => connection.id)).toEqual([
      'active',
    ])
    expect(signal.aborted).toBe(true)
  })

  it('recovers an expired application session after a stream EOF and retries the snapshot once', async () => {
    mocks.checkAuthToken.mockResolvedValue(false)
    mocks.client.subscribeConnections
      .mockReturnValueOnce((async function* () {})())
      .mockReturnValueOnce(
        (async function* () {
          yield create(ConnectionEventsSchema, {
            reset: true,
            events: [{ connection: { id: 'active' } }],
          })
        })(),
      )
    expect((await getFreshConnectionsSnapshot())[0]?.id).toBe('active')
    expect(mocks.recoverAuthToken).toHaveBeenCalledTimes(1)
  })

  it('ignores a late connection frame before it can mutate the replacement process reducer', async () => {
    let oldResolve!: (value: ReturnType<typeof create<typeof ConnectionEventsSchema>>) => void
    let freshResolve!: (value: ReturnType<typeof create<typeof ConnectionEventsSchema>>) => void
    const old = new Promise<ReturnType<typeof create<typeof ConnectionEventsSchema>>>((resolve) => {
      oldResolve = resolve
    })
    const fresh = new Promise<ReturnType<typeof create<typeof ConnectionEventsSchema>>>(
      (resolve) => {
        freshResolve = resolve
      },
    )
    mocks.client.subscribeConnections
      .mockReturnValueOnce(
        (async function* () {
          yield await old
        })(),
      )
      .mockReturnValueOnce(
        (async function* () {
          yield await fresh
          await new Promise(() => {})
        })(),
      )
    const handler = vi.fn()
    const unregister = onConnections(handler)
    startNativeApi(1)
    startNativeApi(2)
    freshResolve(
      create(ConnectionEventsSchema, {
        reset: true,
        events: [{ connection: { id: 'fresh', domain: 'fresh.example' } }],
      }),
    )
    await flush()
    oldResolve(
      create(ConnectionEventsSchema, { reset: true, events: [{ connection: { id: 'stale' } }] }),
    )
    await flush()
    expect(handler).toHaveBeenCalledTimes(1)
    expect(handler.mock.calls[0]?.[0].active.map((row: any) => row.connection.id)).toEqual([
      'fresh',
    ])
    unregister()
  })
})
