import { create, fromBinary, toBinary } from '@bufbuild/protobuf'
import { describe, expect, it, vi } from 'vitest'

import { ProfileSchema } from '@gen/profile/v1/profile_pb'
import { DefaultInbounds, DefaultRoute, DefaultRouteRuleset } from '@/constant/profile'
import { iProfileToProto, protoProfileToIProfile } from '@/utils/profileRpc'

vi.mock('@/lang', () => ({ default: { global: { t: (value: string) => value } } }))

describe('router profile defaults', () => {
  it('creates a transparent inbound and identifies domains before routing', () => {
    const profile = protoProfileToIProfile(undefined)
    profile.inbounds = DefaultInbounds()
    profile.route = DefaultRoute()
    const loaded = protoProfileToIProfile(
      fromBinary(
        ProfileSchema,
        toBinary(ProfileSchema, create(ProfileSchema, iProfileToProto(profile))),
      ),
    )
    expect(
      loaded.inbounds.filter((inbound) => inbound.enable).map((inbound) => inbound.type),
    ).toEqual(['tun'])
    expect(loaded.route.rules[0]).toMatchObject({ enable: true, action: 'sniff', inbound: [] })
    expect(loaded.route.rules[1]).toMatchObject({ enable: true, action: 'hijack-dns' })
    expect(loaded.route.rule_set).toHaveLength(6)
    expect(loaded.route.rule_set.every((rule) => rule.type === 'remote' && !!rule.url)).toBe(true)
    expect(loaded.route.rule_set.map((rule) => rule.update_interval)).toEqual(Array(6).fill('24h'))
    expect(DefaultRouteRuleset().update_interval).toBe('24h')
  })

  it('preserves an existing explicit-proxy configuration and disabled sniffing', () => {
    const profile = protoProfileToIProfile(undefined)
    profile.inbounds = DefaultInbounds().map((inbound) => ({
      ...inbound,
      enable: inbound.type === 'mixed',
    }))
    profile.route = DefaultRoute()
    profile.route.rules[0].enable = false
    const loaded = protoProfileToIProfile(create(ProfileSchema, iProfileToProto(profile)))
    expect(
      loaded.inbounds.filter((inbound) => inbound.enable).map((inbound) => inbound.type),
    ).toEqual(['mixed'])
    expect(loaded.route.rules[0].enable).toBe(false)
  })

  it('shows the implicit remote interval without changing custom intervals or local and inline data', () => {
    const profile = create(ProfileSchema, {
      route: {
        ruleSet: [
          { id: 'remote-default', type: 3 },
          { id: 'remote-custom', type: 3, updateInterval: '6h30m' },
          { id: 'local', type: 2, updateInterval: '', path: '/rules/local.srs' },
          { id: 'inline', type: 1, updateInterval: '', rules: '{"domain":["example.com"]}' },
        ],
      },
    })
    const loaded = protoProfileToIProfile(profile)
    expect(loaded.route.rule_set.map((rule) => rule.update_interval)).toEqual([
      '24h',
      '6h30m',
      '',
      '',
    ])
    const saved = fromBinary(
      ProfileSchema,
      toBinary(ProfileSchema, create(ProfileSchema, iProfileToProto(loaded))),
    )
    expect(saved.route?.ruleSet.map((rule) => rule.updateInterval)).toEqual([
      '24h',
      '6h30m',
      '',
      '',
    ])
    expect(saved.route?.ruleSet[2]?.path).toBe('/rules/local.srs')
    expect(saved.route?.ruleSet[3]?.rules).toBe('{"domain":["example.com"]}')
    // Reading the profile must not mutate the RPC message before an explicit save.
    expect(profile.route?.ruleSet[0]?.updateInterval).toBe('')
  })
})
