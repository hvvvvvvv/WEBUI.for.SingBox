import { create, fromBinary, toBinary } from '@bufbuild/protobuf'
import { describe, expect, it, vi } from 'vitest'

import { ProfileSchema } from '@gen/profile/v1/profile_pb'
import { DefaultDns } from '@/constant/profile'
import { iProfileToProto, protoProfileToIProfile } from '@/utils/profileRpc'

vi.mock('@/lang', () => ({ default: { global: { t: (value: string) => value } } }))

describe('optimistic DNS cache profile conversion', () => {
  it('initializes old profiles without the field to disabled and the core default age', () => {
    const loaded = protoProfileToIProfile(create(ProfileSchema, { dns: {} }))
    expect(loaded.dns.optimistic).toEqual({ enabled: false, timeout: '3d' })
    expect(DefaultDns().optimistic).toEqual({ enabled: false, timeout: '3d' })
  })

  it.each([
    { enabled: true, timeout: '12h' },
    { enabled: false, timeout: '7d' },
  ])('preserves $enabled with age $timeout through the protobuf transport', (optimistic) => {
    const profile = protoProfileToIProfile(create(ProfileSchema, { dns: {} }))
    profile.dns.optimistic = optimistic
    const encoded = toBinary(ProfileSchema, create(ProfileSchema, iProfileToProto(profile)))
    const loaded = protoProfileToIProfile(fromBinary(ProfileSchema, encoded))
    expect(loaded.dns.optimistic).toEqual(optimistic)
  })

  it('fills an omitted timeout without disabling an enabled cache', () => {
    const loaded = protoProfileToIProfile(
      create(ProfileSchema, { dns: { optimistic: { enabled: true } } }),
    )
    expect(loaded.dns.optimistic).toEqual({ enabled: true, timeout: '3d' })
  })
})
