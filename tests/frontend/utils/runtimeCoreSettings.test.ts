import { describe, expect, it, vi } from 'vitest'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { ManagedNativeAPIListener } from '@/constant/kernel'
import { DefaultInboundDirect, DefaultInboundMixed } from '@/constant/profile'
import { protoProfileToIProfile } from '@/utils/profileRpc'
import {
  applyRuntimeCoreSettings,
  canSelectTunStack,
  runtimeCoreSettingsDraft,
  runtimeAllowsLAN,
  setRuntimeAllowLAN,
} from '@/utils/runtimeCoreSettings'

vi.mock('@/lang', () => ({ default: { global: { t: (value: string) => value } } }))
const options = { platform: 'linux', stackEditable: true }
const fixture = () => {
  const profile = protoProfileToIProfile(undefined)
  profile.inbounds[0]!.enable = true
  profile.inbounds[0]!.mixed!.users = ['user:password']
  profile.inbounds.push(
    {
      id: 'mixed-second',
      type: 'mixed',
      tag: 'secondary',
      enable: true,
      mixed: DefaultInboundMixed(),
    },
    { id: 'dns', type: 'direct', tag: 'dns', enable: true, direct: DefaultInboundDirect() },
  )
  profile.inbounds[2]!.mixed!.listen.listen_port = 20222
  return profile
}

describe('temporary core settings', () => {
  it.each([
    { auto: false, name: '', mode: 'system' },
    { auto: true, name: '', mode: 'auto' },
    { auto: true, name: 'saved-eth0', mode: 'auto' },
    { auto: false, name: 'eth0', mode: 'interface' },
  ])(
    'preserves unedited route interface settings $auto/$name when another port changes',
    ({ auto, name, mode }) => {
      const profile = fixture()
      profile.route.auto_detect_interface = auto
      profile.route.default_interface = name
      const draft = runtimeCoreSettingsDraft(profile)
      expect(draft.outboundMode).toBe(mode)
      draft.proxies[0]!.port = 21001
      const changed = applyRuntimeCoreSettings(profile, draft, options)
      expect(changed.route).toEqual(profile.route)
    },
  )

  it('does not disable an existing dynamic listener or modify inactive TUN options during another edit', () => {
    const profile = fixture()
    profile.inbounds[0]!.mixed!.listen.listen_port = 0
    profile.inbounds[2]!.mixed!.listen.listen_port = 0
    profile.inbounds[1]!.enable = false
    profile.inbounds[1]!.tun!.interface_name = 'old Windows TUN device name'
    const draft = runtimeCoreSettingsDraft(profile)
    draft.outboundMode = 'system'
    const changed = applyRuntimeCoreSettings(profile, draft, options)
    expect(changed.inbounds).toEqual(profile.inbounds)
    expect(draft.proxies[0]!.dynamicPort).toBe(true)
    expect(changed.route.auto_detect_interface).toBe(false)
    expect(changed.route.default_interface).toBe('')
  })
  it('reserves the actual backend native API listener before stopping the running core', () => {
    const backend = readFileSync(resolve(process.cwd(), '../bridge/config/native_api.go'), 'utf8')
    expect(backend).toContain(
      `var CoreAPIController = "${ManagedNativeAPIListener.listen}:${ManagedNativeAPIListener.port}"`,
    )
    const profile = fixture()
    const draft = runtimeCoreSettingsDraft(profile)
    draft.proxies[0]!.port = ManagedNativeAPIListener.port
    for (const listen of ['127.0.0.1', '0.0.0.0', '::']) {
      draft.proxies[0]!.listen = listen
      expect(() => applyRuntimeCoreSettings(profile, draft, options)).toThrow('managedPort')
    }
  })
  it('updates each inbound by ID without losing credentials, DNS listeners or TUN routing options', () => {
    const profile = fixture()
    const before = JSON.stringify(profile)
    const draft = runtimeCoreSettingsDraft(profile)
    expect(draft.proxies.filter((proxy) => proxy.type === 'mixed')).toHaveLength(2)
    draft.proxies[1]!.port = 20223
    setRuntimeAllowLAN(draft.proxies[1]!, true)
    draft.outboundInterface = 'eth0'
    draft.outboundMode = 'interface'
    const changed = applyRuntimeCoreSettings(profile, draft, options)
    expect(JSON.stringify(profile)).toBe(before)
    expect(changed.inbounds[0]).toEqual(profile.inbounds[0])
    expect(changed.inbounds[1]).toEqual(profile.inbounds[1])
    expect(changed.inbounds[3]).toEqual(profile.inbounds[3])
    expect(changed.inbounds[2]!.mixed!.listen).toMatchObject({
      listen: '0.0.0.0',
      listen_port: 20223,
    })
    expect(changed.route.default_interface).toBe('eth0')
    expect(changed.route.auto_detect_interface).toBe(false)
    expect(changed.route.rules).toEqual(profile.route.rules)
    expect(changed.dns).toEqual(profile.dns)
  })

  it('keeps missing protocols absent at zero, adds one explicitly, and disables without losing the stored port', () => {
    const profile = fixture()
    const draft = runtimeCoreSettingsDraft(profile)
    draft.proxies[0]!.port = 0
    const http = draft.proxies.find((proxy) => proxy.type === 'http')!
    http.port = 8080
    const changed = applyRuntimeCoreSettings(profile, draft, options)
    expect(changed.inbounds[0]!.enable).toBe(false)
    expect(changed.inbounds[0]!.mixed!.listen.listen_port).toBe(20122)
    expect(changed.inbounds.find((inbound) => inbound.type === 'http')).toMatchObject({
      enable: true,
      http: { listen: { listen_port: 8080 }, users: [] },
    })
    expect(changed.inbounds.find((inbound) => inbound.type === 'socks')).toBeUndefined()
  })

  it.each([-1, 65536, 1.5, Number.NaN])('rejects invalid port %s before restart', (port) => {
    const profile = fixture()
    const draft = runtimeCoreSettingsDraft(profile)
    draft.proxies[0]!.port = port
    expect(() => applyRuntimeCoreSettings(profile, draft, options)).toThrow('invalidPort')
  })

  it('rejects overlapping TCP binds and outbound-to-TUN loops', () => {
    const profile = fixture()
    const draft = runtimeCoreSettingsDraft(profile)
    draft.proxies[1]!.port = draft.proxies[0]!.port
    draft.proxies[1]!.listen = '::'
    expect(() => applyRuntimeCoreSettings(profile, draft, options)).toThrow('portConflict')
    draft.proxies[1]!.port++
    draft.tunnels[0]!.interfaceName = 'webui-tun'
    draft.outboundInterface = 'webui-tun'
    draft.outboundMode = 'interface'
    expect(() => applyRuntimeCoreSettings(profile, draft, options)).toThrow('tunAsOutbound')
  })

  it('accepts Windows interface spaces and allows outbound interface selection without TUN', () => {
    const profile = fixture()
    profile.inbounds = profile.inbounds.filter((inbound) => inbound.type !== 'tun')
    const draft = runtimeCoreSettingsDraft(profile)
    draft.outboundInterface = 'Wi-Fi 2'
    draft.outboundMode = 'interface'
    expect(
      applyRuntimeCoreSettings(profile, draft, { platform: 'windows', stackEditable: true }).route
        .default_interface,
    ).toBe('Wi-Fi 2')
    expect(() => applyRuntimeCoreSettings(profile, draft, options)).toThrow('invalidInterface')
    draft.outboundInterface = '1234567890123456'
    expect(() => applyRuntimeCoreSettings(profile, draft, options)).toThrow('invalidInterface')
  })

  it('preserves the selected stack when unavailable or removed, including 1.15 prereleases', () => {
    expect(canSelectTunStack('1.14.4')).toBe(true)
    expect(canSelectTunStack('v1.15.0-alpha.1')).toBe(false)
    expect(canSelectTunStack('')).toBe(false)
    const profile = fixture()
    const draft = runtimeCoreSettingsDraft(profile)
    draft.tunnels[0]!.stack = 'gvisor'
    expect(
      applyRuntimeCoreSettings(profile, draft, { ...options, stackEditable: false }).inbounds[1],
    ).toEqual(profile.inbounds[1])
  })

  it('keeps custom bind addresses until LAN is changed and preserves IPv6 family', () => {
    expect(runtimeAllowsLAN('0:0:0:0:0:0:0:1')).toBe(false)
    expect(runtimeAllowsLAN('::ffff:127.0.0.2')).toBe(false)
    expect(runtimeAllowsLAN('::ffff:c0a8:801')).toBe(true)
    const profile = fixture()
    profile.inbounds[0]!.mixed!.listen.listen = '192.168.8.1'
    const draft = runtimeCoreSettingsDraft(profile)
    expect(applyRuntimeCoreSettings(profile, draft, options).inbounds[0]).toEqual(
      profile.inbounds[0],
    )
    const proxy = draft.proxies[0]!
    proxy.listen = '::1'
    expect(runtimeAllowsLAN(proxy.listen)).toBe(false)
    setRuntimeAllowLAN(proxy, true)
    expect(proxy.listen).toBe('::')
    setRuntimeAllowLAN(proxy, false)
    expect(proxy.listen).toBe('::1')
  })
})
