// @vitest-environment jsdom
import { readFileSync, writeFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { create, fromBinary, toBinary, toJson } from '@bufbuild/protobuf'
import { createPinia, setActivePinia } from 'pinia'
import { createApp, defineComponent, h, type App } from 'vue'
import { afterEach, expect, it, vi } from 'vitest'

import { ProfileSchema } from '@gen/profile/v1/profile_pb'

const fixture = vi.hoisted(() => ({
  sequence: 0,
  store: undefined as any,
  config: { profile: '' },
  createProfile: vi.fn(),
  addSubscribe: vi.fn().mockResolvedValue(undefined),
  updateSubscribe: vi.fn().mockResolvedValue(undefined),
}))
vi.mock('@/lang', () => ({ default: { global: { t: (key: string) => key } } }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/bridge', () => ({ createRpcClient: () => ({ createProfile: fixture.createProfile }) }))
vi.mock('@/utils/others', async () => ({
  ...(await vi.importActual('@/utils/others')),
  sampleID: () => `fixture-${++fixture.sequence}`,
}))
vi.mock('@/utils', () => ({
  sampleID: () => 'default-profile-fixture',
  message: { success: vi.fn(), error: vi.fn() },
}))
vi.mock('@/stores', () => ({
  useAppConfigStore: () => ({ config: fixture.config }),
  useProfilesStore: () => fixture.store,
  useSubscribesStore: () => ({
    getSubscribeTemplate: () => ({ id: 'fixture-subscription' }),
    addSubscribe: fixture.addSubscribe,
    updateSubscribe: fixture.updateSubscribe,
  }),
}))

import { useProfilesStore } from '@/stores/profiles'
import QuickStart from '@/views/HomeView/components/QuickStart.vue'

let app: App | undefined
let host: HTMLDivElement | undefined
afterEach(() => {
  app?.unmount()
  host?.remove()
})

// The Go integration test consumes this exact protobuf JSON, rather than a
// second hand-written copy of the defaults. Only labels and random IDs are
// deterministic here; QuickStart, the profile store, defaults and RPC mapping
// are production code. Explicitly regenerate after intentional default changes:
// UPDATE_ROUTER_DEFAULT_FIXTURE=1 pnpm --dir frontend test DefaultProfilePipeline
it('exports the real QuickStart subscription bindings through the protobuf wire format', async () => {
  fixture.sequence = 0
  fixture.createProfile.mockResolvedValue({})
  setActivePinia(createPinia())
  fixture.store = useProfilesStore()
  host = document.createElement('div')
  document.body.append(host)
  app = createApp(QuickStart)
  app.provide('cancel', vi.fn())
  app.provide('submit', vi.fn())
  app.config.globalProperties.$t = (key: string) => key
  app.component('Input', defineComponent({ render: () => h('input') }))
  const component = app.mount(host) as any
  await component.modalSlots.submit().props.onClick()

  expect(fixture.addSubscribe).toHaveBeenCalledWith({ id: 'fixture-subscription' })
  expect(fixture.updateSubscribe).toHaveBeenCalledWith('fixture-subscription')
  expect(fixture.createProfile).toHaveBeenCalledTimes(1)
  const request = fixture.createProfile.mock.calls[0]![0]
  const profile = fromBinary(
    ProfileSchema,
    toBinary(ProfileSchema, create(ProfileSchema, request.profile)),
  )
  for (const id of ['outbound-select', 'outbound-urltest']) {
    expect(profile.outbounds.find((group) => group.id === id)?.outbounds).toEqual(
      expect.arrayContaining([
        expect.objectContaining({ type: 'Subscription', id: 'fixture-subscription' }),
      ]),
    )
  }
  expect(profile.route?.ruleSet).toHaveLength(6)
  expect(profile.route?.ruleSet.every((rule) => rule.updateInterval === '24h')).toBe(true)
  expect(profile.route?.final).toBe('outbound-fallback')
  expect(profile.dns?.servers.find((server) => server.id === 'Remote-DNS')?.detour).toBe(
    'outbound-select',
  )

  const serialized = JSON.stringify(toJson(ProfileSchema, profile), null, 2) + '\n'
  const path = resolve(import.meta.dirname, '../../fixtures/router-default-profile.json')
  if (process.env.UPDATE_ROUTER_DEFAULT_FIXTURE === '1') writeFileSync(path, serialized)
  expect(serialized).toBe(readFileSync(path, 'utf8').replace(/\r\n/g, '\n'))
})
