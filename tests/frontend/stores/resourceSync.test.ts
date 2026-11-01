import { create } from '@bufbuild/protobuf'
import { Code, ConnectError } from '@connectrpc/connect'
import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { ProfileSchema } from '@gen/profile/v1/profile_pb'

const fixture = vi.hoisted(() => ({
  store: undefined as any,
  events: new Map<string, (...args: any[]) => void>(),
  rpc: { listProfiles: vi.fn(), updateProfile: vi.fn() },
}))

vi.mock('@/bridge', () => ({
  createRpcClient: () => fixture.rpc,
  EventsOn: (name: string, handler: (...args: any[]) => void) => fixture.events.set(name, handler),
  Notify: vi.fn(),
  onWebSocketConnected: vi.fn(),
}))
vi.mock('@/lang', () => ({ default: { global: { t: (value: string) => value } } }))
vi.mock('@/utils', () => ({ message: { error: vi.fn() }, sampleID: () => 'test-id' }))
vi.mock('@/stores', async () => ({
  ...(await import('@/stores/resourceSync')),
  useAppConfigStore: () => ({ config: {} }),
  useProfilesStore: () => fixture.store,
  useLogsStore: () => ({}),
  useRulesetsStore: () => ({}),
  useScheduledTasksStore: () => ({}),
  useSubscribesStore: () => ({}),
}))

beforeEach(() => {
  vi.resetModules()
  vi.resetAllMocks()
  vi.useFakeTimers()
  fixture.events.clear()
  setActivePinia(createPinia())
})
afterEach(() => {
  vi.clearAllTimers()
  vi.useRealTimers()
  vi.restoreAllMocks()
})

it.each(['before', 'after'])(
  'receives another client update when its event arrives %s a newer local mutation response',
  async (eventOrder) => {
    const { useProfilesStore } = await import('@/stores/profiles')
    const { useBackendEvents } = await import('@/hooks/useBackendEvents')
    const store = useProfilesStore()
    fixture.store = store
    const profile = (id: string, name: string) => create(ProfileSchema, { id, name })
    const initial = [profile('a', 'a-old'), profile('b', 'b-old')]
    const latest = [profile('a', 'a-remote'), profile('b', 'b-local')]
    const state = (revision: bigint) => ({
      instanceId: 'instance',
      stateRevision: revision,
      orderRevision: 1n,
      itemRevisions: { a: revision, b: revision },
    })
    fixture.rpc.listProfiles.mockResolvedValueOnce({ profiles: initial, state: state(1n) })
    await store.setupProfiles()
    fixture.rpc.listProfiles.mockResolvedValue({ profiles: latest, state: state(3n) })
    useBackendEvents().setupBackendEvents()

    const emitRemoteChange = () =>
      fixture.events.get('resourceChanged')!({
        domain: 'profiles',
        operation: 'upsert',
        instanceId: 'instance',
        stateRevision: 2,
      })
    // A was edited remotely at revision 2, while our save of B returns revision 3.
    if (eventOrder === 'before') emitRemoteChange()
    fixture.rpc.updateProfile.mockResolvedValue({
      profile: latest[1],
      state: { ...state(3n), itemRevision: 3n },
    })
    await store.editProfile('b', { ...store.getProfileById('b')!, name: 'b-local' })
    if (eventOrder === 'after') emitRemoteChange()
    await vi.advanceTimersByTimeAsync(50)

    expect(store.getProfileById('a')?.name).toBe('a-remote')
    expect(store.getProfileById('b')?.name).toBe('b-local')
    expect(fixture.rpc.listProfiles).toHaveBeenCalledTimes(2)
  },
)

it('retries a failed event refresh without another event, merging newer revisions during backoff', async () => {
  vi.spyOn(console, 'error').mockImplementation(() => {})
  const { useProfilesStore } = await import('@/stores/profiles')
  const { useBackendEvents } = await import('@/hooks/useBackendEvents')
  const store = useProfilesStore()
  fixture.store = store
  const snapshot = (revision: bigint, name: string) => ({
    profiles: [create(ProfileSchema, { id: 'a', name })],
    state: {
      instanceId: 'instance',
      stateRevision: revision,
      orderRevision: 1n,
      itemRevisions: { a: revision },
    },
  })
  fixture.rpc.listProfiles.mockResolvedValueOnce(snapshot(1n, 'old'))
  await store.setupProfiles()
  useBackendEvents().setupBackendEvents()
  fixture.rpc.listProfiles.mockRejectedValueOnce(new Error('temporarily offline'))
  fixture.rpc.listProfiles.mockResolvedValue(snapshot(3n, 'newest'))
  const emit = (stateRevision: number) =>
    fixture.events.get('resourceChanged')!({
      domain: 'profiles',
      operation: 'upsert',
      instanceId: 'instance',
      stateRevision,
    })
  emit(2)
  await vi.advanceTimersByTimeAsync(50)
  expect(store.getProfileById('a')?.name).toBe('old')
  expect(fixture.rpc.listProfiles).toHaveBeenCalledTimes(2)
  emit(3)
  emit(2)
  await vi.advanceTimersByTimeAsync(499)
  expect(fixture.rpc.listProfiles).toHaveBeenCalledTimes(2)
  await vi.advanceTimersByTimeAsync(1)
  expect(store.getProfileById('a')?.name).toBe('newest')
  expect(fixture.rpc.listProfiles).toHaveBeenCalledTimes(3)
  await vi.advanceTimersByTimeAsync(30_000)
  expect(fixture.rpc.listProfiles).toHaveBeenCalledTimes(3)
})

it('backs off if snapshots are temporarily older than the event rather than polling every 50ms', async () => {
  const { useProfilesStore } = await import('@/stores/profiles')
  const { useBackendEvents } = await import('@/hooks/useBackendEvents')
  fixture.store = useProfilesStore()
  fixture.rpc.listProfiles.mockResolvedValue({
    profiles: [],
    state: { instanceId: 'instance', stateRevision: 1n, orderRevision: 1n, itemRevisions: {} },
  })
  await fixture.store.setupProfiles()
  useBackendEvents().setupBackendEvents()
  fixture.events.get('resourceChanged')!({
    domain: 'profiles',
    operation: 'upsert',
    instanceId: 'instance',
    stateRevision: 2,
  })
  await vi.advanceTimersByTimeAsync(5000)
  // Initial load, then refresh at 50, 550, 1550, 3550 ms.
  expect(fixture.rpc.listProfiles).toHaveBeenCalledTimes(5)
})

it.each(['restarted', 'unauthenticated', 'forbidden'])(
  'does not keep retrying an old event when the backend is %s',
  async (outcome) => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
    const { useProfilesStore } = await import('@/stores/profiles')
    const { useBackendEvents } = await import('@/hooks/useBackendEvents')
    fixture.store = useProfilesStore()
    const snapshot = (instanceId: string) => ({
      profiles: [],
      state: { instanceId, stateRevision: 1n, orderRevision: 1n, itemRevisions: {} },
    })
    fixture.rpc.listProfiles.mockResolvedValueOnce(snapshot('old'))
    await fixture.store.setupProfiles()
    if (outcome === 'restarted') fixture.rpc.listProfiles.mockResolvedValue(snapshot('new'))
    else
      fixture.rpc.listProfiles.mockRejectedValue(
        new ConnectError(
          'access refused',
          outcome === 'forbidden' ? Code.PermissionDenied : Code.Unauthenticated,
        ),
      )
    useBackendEvents().setupBackendEvents()
    fixture.events.get('resourceChanged')!({
      domain: 'profiles',
      operation: 'upsert',
      instanceId: 'old',
      stateRevision: 5,
    })
    await vi.advanceTimersByTimeAsync(60_000)
    expect(fixture.rpc.listProfiles).toHaveBeenCalledTimes(2)
    expect(vi.getTimerCount()).toBe(0)
  },
)

it('does not let a failed old-instance refresh replace a queued new-instance event', async () => {
  vi.spyOn(console, 'error').mockImplementation(() => {})
  const { useProfilesStore } = await import('@/stores/profiles')
  const { useBackendEvents } = await import('@/hooks/useBackendEvents')
  fixture.store = useProfilesStore()
  const snapshot = (instanceId: string, revision: bigint) => ({
    profiles: [],
    state: { instanceId, stateRevision: revision, orderRevision: 1n, itemRevisions: {} },
  })
  fixture.rpc.listProfiles.mockResolvedValueOnce(snapshot('old', 1n))
  await fixture.store.setupProfiles()
  const pending = Promise.withResolvers<unknown>()
  fixture.rpc.listProfiles
    .mockReturnValueOnce(pending.promise)
    .mockResolvedValueOnce(snapshot('new', 1n))
    .mockResolvedValue(snapshot('new', 2n))
  useBackendEvents().setupBackendEvents()
  const emit = (instanceId: string) =>
    fixture.events.get('resourceChanged')!({
      domain: 'profiles',
      operation: 'upsert',
      instanceId,
      stateRevision: 2,
    })
  emit('old')
  await vi.advanceTimersByTimeAsync(50)
  emit('new')
  pending.reject(new Error('old server stopped'))
  await vi.advanceTimersByTimeAsync(2000)
  expect(fixture.store.resourceState).toMatchObject({ instanceId: 'new', snapshotRevision: 2n })
  expect(fixture.rpc.listProfiles).toHaveBeenCalledTimes(4)
  expect(vi.getTimerCount()).toBe(0)
})
