import { create, fromBinary, toBinary } from '@bufbuild/protobuf'
import { describe, expect, it, vi } from 'vitest'

import { ProfileSchema } from '@gen/profile/v1/profile_pb'
import { DefaultInbounds, DefaultRoute } from '@/constant/profile'
import { iProfileToProto, protoProfileToIProfile } from '@/utils/profileRpc'

vi.mock('@/lang', () => ({ default: { global: { t: (value: string) => value } } }))

describe('router profile defaults', () => {
  it('creates a transparent inbound and identifies domains before routing', () => {
    const profile = protoProfileToIProfile(undefined)
    profile.inbounds = DefaultInbounds()
    profile.route = DefaultRoute()
    const loaded = protoProfileToIProfile(
      fromBinary(ProfileSchema, toBinary(ProfileSchema, create(ProfileSchema, iProfileToProto(profile)))),
    )
    expect(loaded.inbounds.filter((inbound) => inbound.enable).map((inbound) => inbound.type)).toEqual(['tun'])
    expect(loaded.route.rules[0]).toMatchObject({ enable: true, action: 'sniff', inbound: [] })
    expect(loaded.route.rules[1]).toMatchObject({ enable: true, action: 'hijack-dns' })
    expect(loaded.route.rule_set).toHaveLength(6)
    expect(loaded.route.rule_set.every((rule) => rule.type === 'remote' && !!rule.url)).toBe(true)
  })

  it('preserves an existing explicit-proxy configuration and disabled sniffing', () => {
    const profile = protoProfileToIProfile(undefined)
    profile.inbounds = DefaultInbounds().map((inbound) => ({ ...inbound, enable: inbound.type === 'mixed' }))
    profile.route = DefaultRoute()
    profile.route.rules[0].enable = false
    const loaded = protoProfileToIProfile(create(ProfileSchema, iProfileToProto(profile)))
    expect(loaded.inbounds.filter((inbound) => inbound.enable).map((inbound) => inbound.type)).toEqual(['mixed'])
    expect(loaded.route.rules[0].enable).toBe(false)
  })
})
