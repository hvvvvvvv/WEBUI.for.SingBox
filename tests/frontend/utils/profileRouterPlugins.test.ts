import { create, fromBinary, toBinary } from '@bufbuild/protobuf'
import { describe, expect, it, vi } from 'vitest'

import { ProfileSchema } from '@gen/profile/v1/profile_pb'
import { iProfileToProto, protoProfileToIProfile } from '@/utils/profileRpc'
import {
  moveRouterPlugin,
  isNewerRouterPluginVersion,
  parseRouterPluginManifest,
  updateRouterPluginManifest,
  validateProfileRouterPlugins,
} from '@/utils/profileRouterPlugins'

vi.mock('@/lang', () => ({ default: { global: { t: (value: string) => value } } }))

const manifest = () => ({
  apiVersion: 'webui.router/v1',
  id: 'community.devices',
  name: 'Devices',
  description: 'Route selected devices',
  version: '1.0.0',
  kind: 'source-routing',
  config: { inbound: '', source_ip_cidr: [] as string[], outbound: '' },
})

const profileWithPlugin = (enabled = false) => {
  const profile = protoProfileToIProfile(create(ProfileSchema))
  profile.router_plugins = [
    { id: 'instance-one', enabled, manifest_json: JSON.stringify(manifest()) },
  ]
  return profile
}

describe('declarative router plugin profiles', () => {
  it('round trips exact manifest snapshots, instance order and enabled flags through protobuf', () => {
    const profile = profileWithPlugin()
    const json = '{\n  "config": { "source_ip_cidr": ["192.0.2.0/24"] }, "note": "keep raw JSON"\n}'
    profile.router_plugins!.push({ id: 'second-instance', enabled: true, manifest_json: json })
    const encoded = toBinary(ProfileSchema, create(ProfileSchema, iProfileToProto(profile)))
    const loaded = protoProfileToIProfile(fromBinary(ProfileSchema, encoded))
    expect(loaded.router_plugins).toEqual(profile.router_plugins)
    expect(loaded.router_plugins![1]!.manifest_json).toBe(json)
    expect(protoProfileToIProfile(create(ProfileSchema)).router_plugins).toEqual([])
  })

  it('allows disabled drafts but requires real enabled TUN and outbound references before enabling', () => {
    const profile = profileWithPlugin()
    expect(validateProfileRouterPlugins(profile)).toBeUndefined()
    profile.router_plugins![0]!.enabled = true
    expect(validateProfileRouterPlugins(profile)?.key).toContain('missingInbound')
    profile.inbounds = [{ id: 'tun-id', tag: 'lan-tun', type: 'tun', enable: true } as IInbound]
    profile.outbounds = [{ id: 'outbound-id', tag: 'proxy', type: 'selector' } as IOutbound]
    const config = {
      ...manifest(),
      config: { inbound: 'lan-tun', source_ip_cidr: [] as string[], outbound: 'proxy' },
    }
    profile.router_plugins![0]!.manifest_json = JSON.stringify(config)
    expect(validateProfileRouterPlugins(profile)?.key).toContain('missingSource')
    config.config.source_ip_cidr = ['192.0.2.10/32', '2001:db8::/64']
    config.config.outbound = 'missing'
    profile.router_plugins![0]!.manifest_json = JSON.stringify(config)
    expect(validateProfileRouterPlugins(profile)?.key).toContain('missingOutbound')
    config.config.outbound = 'proxy'
    profile.router_plugins![0]!.manifest_json = JSON.stringify(config)
    expect(validateProfileRouterPlugins(profile)).toBeUndefined()
    profile.inbounds[0]!.enable = false
    expect(validateProfileRouterPlugins(profile)?.key).toContain('missingInbound')
  })

  it('only imports supported JSON data, rejecting scripts, unknown capabilities, invalid addresses and oversized packages', () => {
    expect(() => parseRouterPluginManifest('export const onRun = () => alert(1)')).toThrow(
      'invalidJson',
    )
    expect(() => parseRouterPluginManifest({ ...manifest(), apiVersion: 'desktop/v1' })).toThrow(
      'unsupportedApi',
    )
    expect(() => parseRouterPluginManifest({ ...manifest(), kind: 'shell' })).toThrow('unknownKind')
    expect(() => parseRouterPluginManifest({ ...manifest(), script: 'rm -rf /' })).toThrow(
      'invalidField',
    )
    expect(() =>
      parseRouterPluginManifest({
        ...manifest(),
        config: { ...manifest().config, command: 'reboot' },
      }),
    ).toThrow('invalidField')
    for (const address of [
      '192.0.2.1',
      '999.1.1.1/24',
      '::/129',
      '192.0.2.1/33',
      '::1/abc',
      '010.0.0.1/8',
    ]) {
      expect(() =>
        parseRouterPluginManifest({
          ...manifest(),
          config: { ...manifest().config, source_ip_cidr: [address] },
        }),
      ).toThrow('invalidField')
    }
    expect(() => parseRouterPluginManifest(' '.repeat(16 * 1024 + 1))).toThrow('tooLarge')
  })

  it('updates only compatible metadata while retaining user settings and independent snapshots', () => {
    const installed = parseRouterPluginManifest({
      ...manifest(),
      config: { inbound: 'lan', source_ip_cidr: ['192.0.2.0/24'], outbound: 'direct' },
    })
    const available = parseRouterPluginManifest({
      ...manifest(),
      version: '1.1.0',
      name: 'New title',
    })
    const result = updateRouterPluginManifest(installed, available)
    expect(result.version).toBe('1.1.0')
    expect(result.name).toBe('New title')
    expect(result.config).toEqual(installed.config)
    expect(result.config).not.toBe(installed.config)
    expect(available.config.inbound).toBe('')
    expect(installed.version).toBe('1.0.0')
    const incompatible = parseRouterPluginManifest({
      ...manifest(),
      kind: 'dns-hijack',
      config: { inbound: '' },
    })
    expect(() => updateRouterPluginManifest(installed, incompatible)).toThrow('incompatibleUpdate')
  })

  it('reorders independent instances without dropping entries at either boundary', () => {
    const a = { id: 'a', enabled: false, manifest_json: JSON.stringify(manifest()) }
    const b = { ...a, id: 'b' }
    const original = [a, b]
    expect(moveRouterPlugin(original, 0, 1)).toEqual([b, a])
    expect(moveRouterPlugin(original, 1, -1)).toEqual([b, a])
    expect(moveRouterPlugin(original, 0, -1)).toEqual(original)
    expect(moveRouterPlugin(original, 1, 1)).toEqual(original)
    expect(original).toEqual([a, b])
  })

  it('matches semantic versions including prereleases and build metadata without offering downgrades', () => {
    for (const version of ['1.0.0', '1.2.3-alpha.1+build.001', '0.0.1+router-arm64']) {
      expect(parseRouterPluginManifest({ ...manifest(), version }).version).toBe(version)
    }
    for (const version of ['01.0.0', '1.02.3', '1.2.03', '1.0.0-01', '1.0.0-a..b', '1.0.0+']) {
      expect(() => parseRouterPluginManifest({ ...manifest(), version })).toThrow('invalidField')
    }
    expect(isNewerRouterPluginVersion('1.10.0', '1.9.0')).toBe(true)
    expect(isNewerRouterPluginVersion('1.0.0', '1.0.0-rc.1')).toBe(true)
    expect(isNewerRouterPluginVersion('1.0.0-rc.10', '1.0.0-rc.2')).toBe(true)
    expect(isNewerRouterPluginVersion('1.0.0-rc', '1.0.0')).toBe(false)
    expect(isNewerRouterPluginVersion('1.0.0+build.2', '1.0.0+build.1')).toBe(false)
    expect(isNewerRouterPluginVersion('1.0.0', '2.0.0')).toBe(false)
  })
})
