export const RouterPluginApiVersion = 'webui.router/v1'
export const MaxRouterPluginBytes = 16 * 1024
export const MaxRouterPlugins = 32

type ManifestBase = {
  apiVersion: typeof RouterPluginApiVersion
  id: string
  name: string
  description: string
  version: string
}

export type RouterPluginManifest = ManifestBase &
  (
    | {
        kind: 'tun-routing'
        config: {
          inbound: string
          auto_redirect: boolean
          strict_route: boolean
          include_interface: string[]
          exclude_address: string[]
        }
      }
    | {
        kind: 'source-routing'
        config: { inbound: string; source_ip_cidr: string[]; outbound: string }
      }
    | { kind: 'dns-hijack'; config: { inbound: string } }
  )

export type RouterPluginIssue = { key: string; field?: string; name?: string }

export class RouterPluginError extends Error {
  constructor(public issue: RouterPluginIssue) {
    super(issue.key)
  }
}

const fail = (key: string, field?: string): never => {
  throw new RouterPluginError({ key: `profile.routerPlugins.errors.${key}`, field })
}

const record = (value: unknown): value is Record<string, unknown> =>
  !!value && typeof value === 'object' && !Array.isArray(value)

const byteLength = (value: string) => new TextEncoder().encode(value).length

const textField = (value: unknown, field: string, max: number, empty = false) => {
  if (typeof value !== 'string' || (!empty && !value.trim()) || byteLength(value) > max) {
    fail('invalidField', field)
  }
}

const stringList = (value: unknown, field: string, max: number, itemMax = 128) => {
  if (!Array.isArray(value) || value.length > max) fail('invalidField', field)
  for (const item of value as unknown[]) textField(item, field, itemMax)
}

const knownKeys = (value: Record<string, unknown>, keys: string[], field: string) => {
  for (const key of Object.keys(value)) {
    if (!keys.includes(key)) fail('invalidField', `${field}${key}`)
  }
}

const isCIDR = (value: string) => {
  const parts = value.split('/')
  if (parts.length !== 2 || !/^\d{1,3}$/.test(parts[1]!)) return false
  const address = parts[0]!
  const prefix = Number(parts[1])
  if (address.includes(':')) {
    if (!/^[0-9a-fA-F:.]+$/.test(address) || prefix > 128) return false
    try {
      return new URL(`http://[${address}]/`).hostname.startsWith('[')
    } catch {
      return false
    }
  }
  const octets = address.split('.')
  return (
    prefix <= 32 &&
    octets.length === 4 &&
    octets.every((part) => /^(0|[1-9]\d{0,2})$/.test(part) && Number(part) <= 255)
  )
}

const cidrList = (value: unknown, field: string) => {
  stringList(value, field, 256)
  if ((value as string[]).some((item) => !isCIDR(item))) fail('invalidField', field)
}

/** Parse data only. Never import JavaScript or evaluate code from a plugin package. */
export const parseRouterPluginManifest = (input: unknown): RouterPluginManifest => {
  let value: unknown = input
  if (typeof input === 'string') {
    if (byteLength(input) > MaxRouterPluginBytes) fail('tooLarge')
    try {
      value = JSON.parse(input)
    } catch {
      fail('invalidJson')
    }
  }
  if (!record(value)) return fail('invalidManifest')
  if (byteLength(JSON.stringify(value)) > MaxRouterPluginBytes) fail('tooLarge')
  knownKeys(value, ['apiVersion', 'id', 'name', 'description', 'version', 'kind', 'config'], '')
  if (value.apiVersion !== RouterPluginApiVersion) fail('unsupportedApi')
  if (typeof value.id !== 'string' || !/^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$/.test(value.id)) {
    fail('invalidField', 'id')
  }
  textField(value.name, 'name', 128)
  if (/\p{Cc}/u.test(value.name as string)) fail('invalidField', 'name')
  textField(value.description, 'description', 1024, true)
  if ((value.description as string).includes('\0')) fail('invalidField', 'description')
  const version = typeof value.version === 'string' ? value.version : ''
  const versionMatch =
    /^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?(?:\+([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?$/.exec(
      version,
    )
  if (
    !versionMatch ||
    byteLength(version) > 64 ||
    versionMatch[4]
      ?.split('.')
      .some((part) => /^\d+$/.test(part) && part.length > 1 && part.startsWith('0'))
  ) {
    fail('invalidField', 'version')
  }
  if (!record(value.config)) return fail('invalidField', 'config')
  const config = value.config
  textField(config.inbound, 'config.inbound', 128, true)
  if (
    (config.inbound as string).trim() !== config.inbound ||
    /\p{Cc}/u.test(config.inbound as string)
  )
    fail('invalidField', 'config.inbound')
  switch (value.kind) {
    case 'tun-routing':
      knownKeys(
        config,
        ['inbound', 'auto_redirect', 'strict_route', 'include_interface', 'exclude_address'],
        'config.',
      )
      for (const key of ['auto_redirect', 'strict_route']) {
        if (typeof config[key] !== 'boolean') fail('invalidField', `config.${key}`)
      }
      stringList(config.include_interface, 'config.include_interface', 16, 15)
      if (
        (config.include_interface as string[]).some(
          (item) => !/^[A-Za-z0-9_.:-]+$/.test(item) || item === '.' || item === '..',
        ) ||
        new Set(config.include_interface as string[]).size !==
          (config.include_interface as string[]).length
      ) {
        fail('invalidField', 'config.include_interface')
      }
      cidrList(config.exclude_address, 'config.exclude_address')
      break
    case 'source-routing':
      knownKeys(config, ['inbound', 'source_ip_cidr', 'outbound'], 'config.')
      textField(config.outbound, 'config.outbound', 128, true)
      if (
        (config.outbound as string).trim() !== config.outbound ||
        /\p{Cc}/u.test(config.outbound as string)
      )
        fail('invalidField', 'config.outbound')
      cidrList(config.source_ip_cidr, 'config.source_ip_cidr')
      break
    case 'dns-hijack':
      knownKeys(config, ['inbound'], 'config.')
      break
    default:
      fail('unknownKind')
  }
  // Return an independent JSON value so editing a snapshot never changes the catalog.
  return JSON.parse(JSON.stringify(value)) as RouterPluginManifest
}

export const validateProfileRouterPlugins = (profile: IProfile): RouterPluginIssue | undefined => {
  const plugins = profile.router_plugins || []
  if (plugins.length > MaxRouterPlugins) return { key: 'profile.routerPlugins.errors.tooMany' }
  if (plugins.reduce((sum, item) => sum + byteLength(item.manifest_json), 0) > 256 * 1024) {
    return { key: 'profile.routerPlugins.errors.totalTooLarge' }
  }
  for (const plugin of plugins) {
    let manifest: RouterPluginManifest
    try {
      manifest = parseRouterPluginManifest(plugin.manifest_json)
    } catch (error) {
      return error instanceof RouterPluginError
        ? error.issue
        : { key: 'profile.routerPlugins.errors.invalidManifest' }
    }
    if (!plugin.enabled) continue
    const issue = (key: string) => ({
      key: `profile.routerPlugins.errors.${key}`,
      name: manifest.name,
    })
    if (
      !profile.inbounds.some(
        (inbound) =>
          inbound.enable && inbound.type === 'tun' && inbound.tag === manifest.config.inbound,
      )
    ) {
      return issue('missingInbound')
    }
    if (manifest.kind === 'source-routing') {
      if (!manifest.config.source_ip_cidr.length) return issue('missingSource')
      if (!profile.outbounds.some((outbound) => outbound.tag === manifest.config.outbound)) {
        return issue('missingOutbound')
      }
    }
  }
}

/** Explicit updates retain user configuration and never mutate the catalog or installed snapshot. */
export const updateRouterPluginManifest = (
  installed: RouterPluginManifest,
  available: RouterPluginManifest,
): RouterPluginManifest => {
  if (
    installed.id !== available.id ||
    installed.apiVersion !== available.apiVersion ||
    installed.kind !== available.kind
  ) {
    return fail('incompatibleUpdate')
  }
  return parseRouterPluginManifest({ ...available, config: installed.config })
}

/** SemVer ordering ignores build metadata and does not offer a cached downgrade. */
export const isNewerRouterPluginVersion = (candidate: string, current: string) => {
  const split = (version: string) => {
    const withoutBuild = version.split('+')[0]!
    const dash = withoutBuild.indexOf('-')
    return {
      core: (dash < 0 ? withoutBuild : withoutBuild.slice(0, dash)).split('.').map(BigInt),
      prerelease: dash < 0 ? [] : withoutBuild.slice(dash + 1).split('.'),
    }
  }
  const a = split(candidate)
  const b = split(current)
  for (let index = 0; index < 3; index++) {
    if (a.core[index] !== b.core[index]) return a.core[index]! > b.core[index]!
  }
  if (!a.prerelease.length || !b.prerelease.length)
    return !a.prerelease.length && !!b.prerelease.length
  for (let index = 0; index < Math.max(a.prerelease.length, b.prerelease.length); index++) {
    const first = a.prerelease[index]
    const second = b.prerelease[index]
    if (first === second) continue
    if (first === undefined || second === undefined) return second === undefined
    const firstNumeric = /^\d+$/.test(first)
    const secondNumeric = /^\d+$/.test(second)
    if (firstNumeric && secondNumeric) return BigInt(first) > BigInt(second)
    if (firstNumeric !== secondNumeric) return secondNumeric
    return first > second
  }
  return false
}

export const moveRouterPlugin = (plugins: IRouterPlugin[], index: number, direction: -1 | 1) => {
  const next = [...plugins]
  const target = index + direction
  if (index < 0 || index >= next.length || target < 0 || target >= next.length) return next
  ;[next[index], next[target]] = [next[target]!, next[index]!]
  return next
}
