import { create, fromBinary, toBinary } from '@bufbuild/protobuf'
import { describe, expect, it, vi } from 'vitest'

import { ProfileSchema } from '../../../gen/profile/v1/profile_pb'

vi.mock('@/lang', () => ({ default: { global: { t: (key: string) => key } } }))
vi.mock('@/utils', () => ({ sampleID: () => 'test-id' }))
vi.mock('../others', () => ({
  deepClone: <T>(value: T): T => JSON.parse(JSON.stringify(value)),
  sampleID: () => 'test-id',
}))

import { DefaultDns, DefaultExperimental } from '@/constant/profile'
import { iProfileToProto, protoProfileToIProfile } from '../profileRpc'

describe('profile DNS cache settings', () => {
  it('uses the core default for new profiles without deprecated settings', () => {
    expect(DefaultExperimental().cache_file).toEqual({
      enabled: true,
      path: 'cache.db',
      cache_id: 'test-id',
      store_fakeip: true,
      store_dns: false,
    })
    expect(DefaultDns()).not.toHaveProperty('independent_cache')
    expect(protoProfileToIProfile(undefined).experimental.cache_file.store_dns).toBe(false)
  })

  it.each([true, false])('preserves store_dns=%s through the protobuf wire format', (storeDns) => {
    const profile = protoProfileToIProfile(undefined)
    profile.experimental.cache_file.store_dns = storeDns

    const message = create(ProfileSchema, iProfileToProto(profile))
    expect(message.experimental?.cacheFile?.storeDns).toBe(storeDns)
    expect(message.experimental?.cacheFile).not.toHaveProperty('store_dns')

    const decoded = fromBinary(ProfileSchema, toBinary(ProfileSchema, message))
    const restored = protoProfileToIProfile(decoded)
    expect(restored.experimental.cache_file).toEqual(profile.experimental.cache_file)
    expect(restored.experimental.cache_file).not.toHaveProperty('storeDns')
    expect(restored.dns).not.toHaveProperty('independent_cache')
  })

  it('reads an omitted protobuf DNS cache setting as false', () => {
    const message = create(ProfileSchema, {
      experimental: { cacheFile: { enabled: true, path: 'existing.db', storeFakeip: true } },
    })
    const decoded = fromBinary(ProfileSchema, toBinary(ProfileSchema, message))
    const restored = protoProfileToIProfile(decoded)

    expect(restored.experimental.cache_file.store_dns).toBe(false)
    expect(restored.experimental.cache_file.path).toBe('existing.db')
    expect(restored.experimental.cache_file.store_fakeip).toBe(true)
  })

  it('lets protobuf ignore deprecated cache fields without enabling store_dns', () => {
    const profile = protoProfileToIProfile(undefined)
    Object.assign(profile.experimental.cache_file, { store_rdrc: true, rdrc_timeout: '7d' })
    Object.assign(profile.dns, { independent_cache: true })

    const message = create(ProfileSchema, iProfileToProto(profile))
    const decoded = fromBinary(ProfileSchema, toBinary(ProfileSchema, message))
    const restored = protoProfileToIProfile(decoded)

    expect(restored.experimental.cache_file.store_dns).toBe(false)
    expect(restored.experimental.cache_file).not.toHaveProperty('store_rdrc')
    expect(restored.experimental.cache_file).not.toHaveProperty('rdrc_timeout')
    expect(restored.dns).not.toHaveProperty('independent_cache')
  })
})
