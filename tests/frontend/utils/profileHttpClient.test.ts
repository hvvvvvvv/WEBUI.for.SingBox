import { create, fromBinary, toBinary } from '@bufbuild/protobuf'
import { describe, expect, it, vi } from 'vitest'

import { ProfileSchema } from '@gen/profile/v1/profile_pb'
import { DefaultRoute } from '@/constant/profile'
import { iProfileToProto, protoProfileToIProfile } from '@/utils/profileRpc'

vi.mock('@/lang', () => ({ default: { global: { t: (value: string) => value } } }))

describe('default rule-set HTTP client profile transport', () => {
  it('uses the selected proxy for new profiles and lets all six rule sets inherit it', () => {
    const route = DefaultRoute()
    expect(route.default_http_client).toBe('outbound-select')
    expect(route.rule_set).toHaveLength(6)
    expect(route.rule_set.every((rule) => rule.download_detour === '')).toBe(true)
  })

  it.each(['proxy-id', 'direct-id', ''])('persists and reloads the outbound ID %j', (id) => {
    const profile = protoProfileToIProfile(create(ProfileSchema, { route: {} }))
    profile.route.default_http_client = id
    const encoded = toBinary(ProfileSchema, create(ProfileSchema, iProfileToProto(profile)))
    const loaded = protoProfileToIProfile(fromBinary(ProfileSchema, encoded))
    expect(loaded.route.default_http_client).toBe(id)
  })

  it('keeps old profiles automatic and preserves their explicit per-rule-set route', () => {
    const loaded = protoProfileToIProfile(create(ProfileSchema, {
      route: { final: 'fallback-id', ruleSet: [{ id: 'old', type: 3, downloadDetour: 'direct-id' }] },
    }))
    expect(loaded.route.default_http_client).toBe('')
    expect(loaded.route.final).toBe('fallback-id')
    expect(loaded.route.rule_set[0]?.download_detour).toBe('direct-id')
  })
})
