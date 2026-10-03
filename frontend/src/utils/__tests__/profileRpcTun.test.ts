import { create, fromBinary, toBinary } from '@bufbuild/protobuf'
import { describe, expect, it, vi } from 'vitest'

import { ProfileSchema } from '../../../gen/profile/v1/profile_pb'

vi.mock('@/lang', () => ({ default: { global: { t: (key: string) => key } } }))
vi.mock('@/utils', () => ({ sampleID: () => 'test-id' }))
vi.mock('../others', () => ({
  deepClone: <T>(value: T): T => JSON.parse(JSON.stringify(value)),
  sampleID: () => 'test-id',
}))

import { DefaultInboundTun } from '@/constant/profile'
import { iProfileToProto, protoProfileToIProfile } from '../profileRpc'

const getTun = (profile: IProfile) =>
  profile.inbounds.find((inbound) => inbound.type === 'tun')!.tun!

const wireRoundTrip = (profile: IProfile) =>
  protoProfileToIProfile(
    fromBinary(
      ProfileSchema,
      toBinary(ProfileSchema, create(ProfileSchema, iProfileToProto(profile))),
    ),
  )

describe('profile TUN DNS mode', () => {
  it('defaults new TUN inbounds to hijack without unsupported DNS or NAT fields', () => {
    const tun = DefaultInboundTun()
    expect(tun.dns_mode).toBe('hijack')
    expect(tun).not.toHaveProperty('dns_address')
    expect(tun).not.toHaveProperty('endpoint_independent_nat')
    expect(getTun(protoProfileToIProfile(undefined)).dns_mode).toBe('hijack')
  })

  it.each(['disabled', 'native', 'hijack'] as const)(
    'preserves %s through protobuf and frontend conversion',
    (mode) => {
      const profile = protoProfileToIProfile(undefined)
      getTun(profile).dns_mode = mode
      const request = iProfileToProto(profile)
      expect(request.inbounds.find((inbound) => inbound.tun)?.tun).toHaveProperty('dnsMode', mode)
      expect(request.inbounds.find((inbound) => inbound.tun)?.tun).not.toHaveProperty('dns_mode')
      const restored = getTun(wireRoundTrip(profile))
      expect(restored).toEqual(getTun(profile))
      expect(restored).not.toHaveProperty('dnsMode')
    },
  )

  it.each([undefined, ''])('defaults an omitted or empty protobuf mode (%s) to hijack', (mode) => {
    const message = create(ProfileSchema, {
      inbounds: [{ type: 4, tun: { dnsMode: mode, stack: 3 } }],
    })
    expect(protoProfileToIProfile(message).inbounds[0]!.tun!.dns_mode).toBe('hijack')
  })

  it.each([undefined, ''])('defaults an omitted or empty outgoing mode (%s) to hijack', (mode) => {
    const profile = protoProfileToIProfile(undefined)
    Object.assign(getTun(profile), { dns_mode: mode })
    expect(getTun(wireRoundTrip(profile)).dns_mode).toBe('hijack')
  })

  it.each([
    'endpoint_independent_nat',
    'endpointindependentnat',
    'endpointIndependentNat',
    'endpoint-independent-nat',
    'Endpoint-Independent-NAT',
    'Endpoint_Independent_Nat',
  ])('removes the legacy %s field on both input and outgoing requests', (legacyKey) => {
    const legacyProfile = {
      inbounds: [{ type: 'tun', tun: { [legacyKey]: true } }],
    }
    const loaded = protoProfileToIProfile(legacyProfile as any)
    expect(loaded.inbounds[0]!.tun!).not.toHaveProperty(legacyKey)
    expect(loaded.inbounds[0]!.tun!.dns_mode).toBe('hijack')

    const profile = protoProfileToIProfile(undefined)
    Object.assign(getTun(profile), { [legacyKey]: true })
    const requestTun = iProfileToProto(profile).inbounds.find((inbound) => inbound.tun)?.tun
    expect(requestTun).not.toHaveProperty(legacyKey)
    expect(getTun(wireRoundTrip(profile))).not.toHaveProperty(legacyKey)
    expect(getTun(profile)).toHaveProperty(legacyKey, true)
  })

  it.each(['dns_mode', 'dnsmode', 'dnsMode'])(
    'normalizes the %s DNS mode alias before loading and serialization',
    (alias) => {
      for (const mode of ['native', 'disabled', 'invalid', '']) {
        const expected = mode === '' ? 'hijack' : mode
        const loaded = protoProfileToIProfile({
          inbounds: [{ type: 'tun', tun: { [alias]: mode } }],
        } as any)
        expect(loaded.inbounds[0]!.tun!.dns_mode).toBe(expected)
        expect(loaded.inbounds[0]!.tun!).not.toHaveProperty('dnsmode')
        expect(loaded.inbounds[0]!.tun!).not.toHaveProperty('dnsMode')

        const profile = protoProfileToIProfile(undefined)
        delete (getTun(profile) as any).dns_mode
        Object.assign(getTun(profile), { [alias]: mode })
        const requestTun = iProfileToProto(profile).inbounds.find((inbound) => inbound.tun)?.tun
        expect(requestTun).toHaveProperty('dnsMode', expected)
        expect(requestTun).not.toHaveProperty('dnsmode')
        expect(requestTun).not.toHaveProperty('dns_mode')
        expect(getTun(wireRoundTrip(profile)).dns_mode).toBe(expected)
      }
    },
  )

  it('gives canonical and compact DNS mode keys precedence over camel-case keys', () => {
    const cases = [
      { dns_mode: 'native', dnsmode: 'disabled', dnsMode: 'hijack' },
      { dnsMode: 'hijack', dnsmode: 'disabled', dns_mode: 'native' },
      { dnsmode: 'native', dnsMode: 'disabled' },
      { dnsMode: 'disabled', dnsmode: 'native' },
    ]
    for (const tun of cases) {
      const loaded = protoProfileToIProfile({
        inbounds: [{ type: 'tun', tun }],
      } as any)
      expect(loaded.inbounds[0]!.tun!.dns_mode).toBe('native')
      const profile = protoProfileToIProfile(undefined)
      delete (getTun(profile) as any).dns_mode
      Object.assign(getTun(profile), tun)
      expect(getTun(wireRoundTrip(profile)).dns_mode).toBe('native')
    }
  })

  it('preserves invalid modes for validation instead of silently changing them', () => {
    const profile = protoProfileToIProfile(undefined)
    Object.assign(getTun(profile), { dns_mode: 'invalid' })
    expect(getTun(wireRoundTrip(profile)).dns_mode).toBe('invalid')
  })
})
