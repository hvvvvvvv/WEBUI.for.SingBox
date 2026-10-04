import type { Connection, Log, NativeConnectionSnapshot, NativeConnectionRow } from '@/types/kernel'

export const sameKernelMode = (a: string, b: string) => a.toLowerCase() === b.toLowerCase()

export const nativeModeOptions = (modes: string[]) => {
  const seen = new Set<string>()
  return modes.flatMap((value) => {
    // Native lists can include both the default "Rule" and a configured "rule".
    const normalized = value.toLowerCase()
    if (seen.has(normalized)) return []
    seen.add(normalized)
    const known = ['global', 'rule', 'direct'].includes(normalized)
    return [
      {
        value,
        label: known ? `kernel.${normalized}` : value,
        desc: known ? `kernel.${normalized}Desc` : '',
      },
    ]
  })
}

// Native endpoints already include ports and bracket IPv6 hosts.
export const endpointHost = (endpoint: string) => {
  const bracketed = /^\[([^\]]+)\](?::\d+)?$/.exec(endpoint)
  if (bracketed) return bracketed[1]!.split('%')[0]!
  const pieces = endpoint.split(':')
  if (pieces.length === 2 && /^\d+$/.test(pieces[1]!)) return pieces[0]!
  return endpoint.split('%')[0]!
}

export const addressCIDR = (endpoint: string): string | undefined => {
  const host = endpointHost(endpoint)
  if (/^(?:\d{1,3}\.){3}\d{1,3}$/.test(host)) {
    if (host.split('.').every((part) => Number(part) <= 255)) return `${host}/32`
    return
  }
  if (host.includes(':')) {
    try {
      // URL validates compressed and IPv4-mapped IPv6 without node-only dependencies.
      new URL(`http://[${host}]/`)
      return `${host}/128`
    } catch {
      return
    }
  }
}

export const connectionRuleOptions = (connection: Connection) => {
  const options: {
    type: 'domain' | 'domain_suffix' | 'ip_cidr' | 'process_path'
    value: string
  }[] = []
  if (connection.domain && !addressCIDR(connection.domain)) {
    options.push({ type: 'domain', value: connection.domain })
    const parts = connection.domain.replace(/\.$/, '').split('.')
    if (parts.length > 2)
      options.push({ type: 'domain_suffix', value: '.' + parts.slice(1).join('.') })
  }
  const cidr = addressCIDR(connection.destination)
  if (cidr) options.push({ type: 'ip_cidr', value: cidr })
  if (connection.processInfo?.processPath) {
    options.push({ type: 'process_path', value: connection.processInfo.processPath })
  }
  return options
}

export const compareBigInt = (a: bigint, b: bigint) => (a > b ? 1 : a < b ? -1 : 0)

export const freezeConnectionSnapshot = (
  snapshot: NativeConnectionSnapshot,
): NativeConnectionSnapshot => {
  const clone = (row: NativeConnectionRow): NativeConnectionRow => ({
    ...row,
    connection: {
      ...row.connection,
      chainList: [...row.connection.chainList],
      processInfo: row.connection.processInfo && {
        ...row.connection.processInfo,
        packageNames: [...row.connection.processInfo.packageNames],
      },
    },
  })
  return { active: snapshot.active.map(clone), closed: snapshot.closed.map(clone) }
}

export type DisplayLog = { id: number; type: string; payload: string }

// Strip complete terminal CSI/OSC sequences while preserving the log text.
const nativeLogANSI =
  // oxlint-disable-next-line no-control-regex -- Matching terminal control codes is intentional.
  /(?:\x1b\[|\x9b)[0-?]*[ -/]*[@-~]|(?:\x1b\]|\x9d)[^\x07\x1b\x9c]*(?:\x07|\x1b\\|\x9c)/g

export class NativeLogBuffer {
  rows: DisplayLog[] = []
  private nextId = 0
  append(log: Log) {
    const levels = ['panic', 'fatal', 'error', 'warn', 'info', 'debug', 'trace']
    if (log.reset) this.rows = []
    const incoming = log.messages
      .map((entry) => ({
        id: ++this.nextId,
        type: levels[entry.level] || 'info',
        payload: entry.message.replace(nativeLogANSI, ''),
      }))
      .reverse()
    this.rows = [...incoming, ...this.rows].slice(0, 1000)
  }
  clear() {
    this.rows = []
  }
}

export const stringifyNative = (value: unknown) =>
  JSON.stringify(value, (_, item) => (typeof item === 'bigint' ? item.toString() : item), 2)

const legacyConnectionKeys: Record<string, string> = {
  'metadata.type': 'connection.inboundType',
  'metadata.processPath': 'connection.processInfo.processPath',
  'metadata.host': 'connection.domain',
  'metadata.sourceIP': 'connection.source',
  'metadata.destinationIP': 'connection.destination',
  rule: 'connection.rule',
  chains: 'connection.chainList',
  up: 'uplinkBps',
  down: 'downlinkBps',
  upload: 'uplinkTotal',
  download: 'downlinkTotal',
  start: 'connection.createdAt',
}

export const migrateConnectionColumns = (
  raw: { visibility?: unknown; order?: unknown },
  defaults: { visibility: Record<string, boolean>; order: string[] },
) => {
  const visibility = { ...defaults.visibility }
  const valid = (key: string) => Object.hasOwn(defaults.visibility, key)
  if (raw.visibility && typeof raw.visibility === 'object' && !Array.isArray(raw.visibility)) {
    for (const [oldKey, enabled] of Object.entries(raw.visibility)) {
      const key = legacyConnectionKeys[oldKey] ?? oldKey
      if (valid(key) && typeof enabled === 'boolean') visibility[key] = enabled
    }
  }
  const order: string[] = []
  if (Array.isArray(raw.order)) {
    for (const oldKey of raw.order) {
      if (typeof oldKey !== 'string') continue
      const key = legacyConnectionKeys[oldKey] ?? oldKey
      if (valid(key) && !order.includes(key)) order.push(key)
    }
  }
  return { visibility, order: [...order, ...defaults.order.filter((key) => !order.includes(key))] }
}
