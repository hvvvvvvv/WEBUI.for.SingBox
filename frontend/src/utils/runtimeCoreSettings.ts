import { DefaultInboundMixed } from '@/constant/profile'
import { ManagedNativeAPIListener } from '@/constant/kernel'
import { deepClone, sampleID } from './others'

export type RuntimeProxyType = 'mixed' | 'http' | 'socks'
export type RuntimeProxyDraft = {
  id: string
  type: RuntimeProxyType
  tag: string
  port: number
  listen: string
  dynamicPort: boolean
}
export type RuntimeCoreSettingsDraft = {
  proxies: RuntimeProxyDraft[]
  tunnels: { id: string; tag: string; stack: string; interfaceName: string }[]
  outboundInterface: string
  outboundMode: 'system' | 'auto' | 'interface'
}

const proxyTypes: RuntimeProxyType[] = ['mixed', 'http', 'socks']
const failure = (key: string): never => {
  throw new Error(`home.overview.runtimeSettings.${key}`)
}

export const canSelectTunStack = (version: string) => {
  const parsed = /^v?(\d+)\.(\d+)\./.exec(version.trim())
  return !!parsed && Number(parsed[1]) === 1 && Number(parsed[2]) < 15
}

const canonicalListen = (listen: string) => {
  if (!listen.includes(':')) return listen
  try {
    const value = new URL(`http://[${listen}]`).hostname.slice(1, -1)
    const mapped = /^::ffff:([\da-f]+):([\da-f]+)$/.exec(value)
    if (!mapped) return value
    const high = Number.parseInt(mapped[1]!, 16)
    const low = Number.parseInt(mapped[2]!, 16)
    return `${high >> 8}.${high & 255}.${low >> 8}.${low & 255}`
  } catch {
    return listen
  }
}

export const runtimeAllowsLAN = (listen: string) => {
  const address = canonicalListen(listen)
  return address !== '::1' && !address.startsWith('127.')
}

export const setRuntimeAllowLAN = (proxy: RuntimeProxyDraft, allow: boolean) => {
  proxy.listen = proxy.listen.includes(':')
    ? allow
      ? '::'
      : '::1'
    : allow
      ? '0.0.0.0'
      : '127.0.0.1'
}

export const runtimeCoreSettingsDraft = (profile: IProfile): RuntimeCoreSettingsDraft => ({
  proxies: proxyTypes.flatMap((type) => {
    const inbounds = profile.inbounds.filter((inbound) => inbound.type === type && inbound[type])
    return inbounds.length
      ? inbounds.map((inbound) => ({
          id: inbound.id,
          type,
          tag: inbound.tag,
          port: inbound.enable ? inbound[type]!.listen.listen_port : 0,
          listen: inbound[type]!.listen.listen,
          dynamicPort: inbound.enable && inbound[type]!.listen.listen_port === 0,
        }))
      : [{ id: '', type, tag: `${type}-in`, port: 0, listen: '127.0.0.1', dynamicPort: false }]
  }),
  tunnels: profile.inbounds.flatMap((inbound) =>
    inbound.type === 'tun' && inbound.tun
      ? [
          {
            id: inbound.id,
            tag: inbound.tag,
            stack: inbound.tun.stack,
            interfaceName: inbound.tun.interface_name,
          },
        ]
      : [],
  ),
  outboundInterface: profile.route.default_interface,
  outboundMode: profile.route.auto_detect_interface
    ? 'auto'
    : profile.route.default_interface
      ? 'interface'
      : 'system',
})

const validInterface = (value: string, platform: string) =>
  value !== '.' &&
  value !== '..' &&
  value.trim() === value &&
  !/[/\\]/u.test(value) &&
  !Array.from(value).some(
    (character) => character.charCodeAt(0) < 32 || character.charCodeAt(0) === 127,
  ) &&
  (platform !== 'linux' || !/\s/u.test(value)) &&
  new TextEncoder().encode(value).length <= (platform === 'linux' ? 15 : 63)

const listenOverlaps = (first: string, second: string) => {
  const left = canonicalListen(first)
  const right = canonicalListen(second)
  return (
    left === right ||
    !left ||
    !right ||
    left === '::' ||
    right === '::' ||
    (left === '0.0.0.0' && !right.includes(':')) ||
    (right === '0.0.0.0' && !left.includes(':'))
  )
}

// Patch only controls shown by the temporary settings panel; preserve users,
// DNS/direct listeners, routing rules, TUN routing options and stored profiles.
export const applyRuntimeCoreSettings = (
  original: IProfile,
  draft: RuntimeCoreSettingsDraft,
  options: { platform: string; stackEditable: boolean },
): IProfile => {
  const profile = deepClone(original)
  for (const proxy of draft.proxies) {
    if (!Number.isInteger(proxy.port) || proxy.port < 0 || proxy.port > 65535)
      failure('invalidPort')
    let inbound = proxy.id ? profile.inbounds.find((value) => value.id === proxy.id) : undefined
    if (proxy.id && (!inbound || inbound.type !== proxy.type || !inbound[proxy.type]))
      failure('changed')
    if (!inbound) {
      if (proxy.port === 0) continue
      let tag = `${proxy.type}-in`
      for (let suffix = 2; profile.inbounds.some((value) => value.tag === tag); suffix++)
        tag = `${proxy.type}-in-${suffix}`
      inbound = {
        id: sampleID(),
        tag,
        type: proxy.type,
        enable: true,
        [proxy.type]: DefaultInboundMixed(),
      }
      profile.inbounds.push(inbound)
    }
    const listener = inbound[proxy.type]!.listen
    // An unchanged custom bind address is retained. The LAN toggle only emits
    // explicit loopback or wildcard IPs, never an arbitrary host or URL.
    const oldInbound = original.inbounds.find((value) => value.id === proxy.id)
    const oldListener = oldInbound?.[proxy.type]?.listen.listen
    if (
      proxy.listen !== oldListener &&
      !['127.0.0.1', '0.0.0.0', '::1', '::'].includes(proxy.listen)
    )
      failure('invalidListen')
    listener.listen = proxy.listen
    const oldPort = oldInbound?.enable ? oldInbound[proxy.type]!.listen.listen_port : 0
    if (!oldInbound || proxy.port !== oldPort) {
      if (proxy.port !== 0) listener.listen_port = proxy.port
      inbound.enable = proxy.port !== 0
    }
  }
  for (const tunnel of draft.tunnels) {
    const tun = profile.inbounds.find(
      (value) => value.id === tunnel.id && value.type === 'tun',
    )?.tun
    if (!tun) return failure('changed')
    if (tunnel.interfaceName !== tun.interface_name) {
      if (!validInterface(tunnel.interfaceName, options.platform)) failure('invalidInterface')
      tun.interface_name = tunnel.interfaceName
    }
    if (options.stackEditable && tunnel.stack !== tun.stack) {
      if (!['system', 'gvisor', 'mixed'].includes(tunnel.stack)) failure('invalidStack')
      tun.stack = tunnel.stack as NonNullable<IInbound['tun']>['stack']
    }
  }
  if (draft.outboundMode === 'interface') {
    if (!draft.outboundInterface || !validInterface(draft.outboundInterface, options.platform))
      failure('invalidInterface')
    if (
      profile.inbounds.some(
        (inbound) => inbound.enable && inbound.tun?.interface_name === draft.outboundInterface,
      )
    )
      failure('tunAsOutbound')
    profile.route.default_interface = draft.outboundInterface
    profile.route.auto_detect_interface = false
  } else if (draft.outboundMode === 'auto') {
    // Keep an inactive saved interface name so changing an unrelated port
    // does not rewrite it. The generator ignores it while detection is on.
    profile.route.auto_detect_interface = true
  } else if (draft.outboundMode === 'system') {
    profile.route.default_interface = ''
    profile.route.auto_detect_interface = false
  } else failure('invalidInterface')

  const listeners = profile.inbounds.flatMap((inbound) => {
    if (!inbound.enable || inbound.type === 'tun') return []
    if (inbound.type === 'direct' && inbound.direct?.network === 'udp') return []
    const listen = inbound[inbound.type]?.listen
    return listen ? [listen] : []
  })
  for (let i = 0; i < listeners.length; i++) {
    const left = listeners[i]!
    if (
      left.listen_port === ManagedNativeAPIListener.port &&
      listenOverlaps(left.listen, ManagedNativeAPIListener.listen)
    )
      failure('managedPort')
    if (
      left.listen_port !== 0 &&
      listeners
        .slice(i + 1)
        .some(
          (right) =>
            left.listen_port === right.listen_port && listenOverlaps(left.listen, right.listen),
        )
    )
      failure('portConflict')
  }
  return profile
}
