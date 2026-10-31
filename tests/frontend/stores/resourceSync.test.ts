import { create } from '@bufbuild/protobuf'
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
afterEach(() => vi.useRealTimers())

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
