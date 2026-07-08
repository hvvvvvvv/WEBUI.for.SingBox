import * as Defaults from '@/constant/profile'

import { deepClone, sampleID } from './others'

import type { Profile } from '../../gen/profile/v1/profile_pb'

const LOG_LEVEL: Record<string, number> = {
  trace: 1, debug: 2, info: 3, warn: 4, error: 5, fatal: 6, panic: 7,
}
const INBOUND_TYPE: Record<string, number> = { mixed: 1, socks: 2, http: 3, tun: 4, direct: 5 }
const INBOUND_NETWORK: Record<string, number> = { tcp: 1, udp: 2 }
const OUTBOUND_TYPE: Record<string, number> = { direct: 1, block: 2, selector: 3, urltest: 4 }
const TUN_STACK: Record<string, number> = { system: 1, gvisor: 2, mixed: 3 }
const RULESET_TYPE: Record<string, number> = { inline: 1, local: 2, remote: 3 }
const RULESET_FORMAT: Record<string, number> = { source: 1, binary: 2 }
const RULE_TYPE: Record<string, number> = {
  inbound: 1, network: 2, protocol: 3, domain: 4, domain_suffix: 5, domain_keyword: 6,
  domain_regex: 7, source_ip_cidr: 8, ip_cidr: 9, ip_is_private: 10,
  source_port: 11, source_port_range: 12, port: 13, port_range: 14,
  process_name: 15, process_path: 16, process_path_regex: 17, clash_mode: 18,
  rule_set: 19, ip_accept_any: 20, inline: 21, InsertionPoint: 22,
}
const STRATEGY: Record<string, number> = {
  default: 1, prefer_ipv4: 2, prefer_ipv6: 3, ipv4_only: 4, ipv6_only: 5,
}
const DNS_SERVER_TYPE: Record<string, number> = {
  local: 1, hosts: 2, tcp: 3, udp: 4, tls: 5, https: 6, quic: 7, h3: 8, dhcp: 9, fakeip: 10, tailscale: 11,
}
const RULE_ACTION: Record<string, number> = {
  route: 1, 'route-options': 2, reject: 3, 'hijack-dns': 4, sniff: 5, resolve: 6,
}
const DNS_RULE_ACTION: Record<string, number> = {
  route: 1, 'route-options': 2, reject: 3, predefined: 4,
}
const MIXIN_PRIORITY: Record<string, number> = { mixin: 1, gui: 2 }
const MIXIN_FORMAT: Record<string, number> = { json: 1, yaml: 2 }

const protoFieldNames: Record<string, string> = {
  auto_detect_interface: 'autoDetectInterface',
  auto_route: 'autoRoute',
  cache_file: 'cacheFile',
  cache_id: 'cacheId',
  client_subnet: 'clientSubnet',
  default_interface: 'defaultInterface',
  default_domain_resolver: 'defaultDomainResolver',
  disable_cache: 'disableCache',
  disable_expire: 'disableExpire',
  domain_resolver: 'domainResolver',
  download_detour: 'downloadDetour',
  endpoint_independent_nat: 'endpointIndependentNat',
  find_process: 'findProcess',
  hosts_path: 'hostsPath',
  independent_cache: 'independentCache',
  inet4_range: 'inet4Range',
  inet6_range: 'inet6Range',
  interface_name: 'interfaceName',
  interrupt_exist_connections: 'interruptExistConnections',
  listen_port: 'listenPort',
  rdrc_timeout: 'rdrcTimeout',
  route_address: 'routeAddress',
  route_exclude_address: 'routeExcludeAddress',
  rule_set: 'ruleSet',
  server_port: 'serverPort',
  store_fakeip: 'storeFakeip',
  store_rdrc: 'storeRdrc',
  strict_route: 'strictRoute',
  tcp_fast_open: 'tcpFastOpen',
  tcp_multi_path: 'tcpMultiPath',
  udp_fragment: 'udpFragment',
  update_interval: 'updateInterval',
}

const frontendFieldNames = Object.entries(protoFieldNames).reduce<Record<string, string>>(
  (result, [frontendKey, protoKey]) => {
    result[protoKey] = frontendKey
    return result
  },
  {},
)

const toNum = (map: Record<string, number>, v: string | number | undefined): number => {
  if (typeof v === 'number') return v
  return v != null ? (map[v] ?? 0) : 0
}

const toProtoFieldNames = (value: any, parentKey = ''): any => {
  if (Array.isArray(value)) {
    return value.map((item) => toProtoFieldNames(item, parentKey))
  }
  if (!value || typeof value !== 'object') {
    return value
  }
  if (parentKey === 'predefined') {
    return value
  }

  return Object.entries(value).reduce<Recordable>((result, [key, item]) => {
    if (key.startsWith('$')) return result
    const protoKey = protoFieldNames[key] ?? key
    result[protoKey] = toProtoFieldNames(item, protoKey)
    return result
  }, {})
}

const fromProtoFieldNames = (value: any, parentKey = ''): any => {
  if (Array.isArray(value)) {
    return value.map((item) => fromProtoFieldNames(item, parentKey))
  }
  if (!value || typeof value !== 'object') {
    return value
  }
  if (parentKey === 'predefined') {
    return value
  }

  return Object.entries(value).reduce<Recordable>((result, [key, item]) => {
    if (key.startsWith('$')) return result
    const frontendKey = frontendFieldNames[key] ?? key
    result[frontendKey] = fromProtoFieldNames(item, frontendKey)
    return result
  }, {})
}

export function iProfileToProto(p: IProfile): Profile {
  const profile = toProtoFieldNames(deepClone(p)) as any
  if (profile.log) profile.log.level = toNum(LOG_LEVEL, profile.log.level)
  for (const inbound of profile.inbounds ?? []) {
    inbound.type = toNum(INBOUND_TYPE, inbound.type)
    if (inbound.direct) inbound.direct.network = toNum(INBOUND_NETWORK, inbound.direct.network)
    if (inbound.tun) inbound.tun.stack = toNum(TUN_STACK, inbound.tun.stack)
  }
  for (const outbound of profile.outbounds ?? []) {
    outbound.type = toNum(OUTBOUND_TYPE, outbound.type)
  }
  for (const rs of profile.route?.ruleSet ?? []) {
    rs.type = toNum(RULESET_TYPE, rs.type)
    rs.format = toNum(RULESET_FORMAT, rs.format)
  }
  for (const rule of profile.route?.rules ?? []) {
    rule.type = toNum(RULE_TYPE, rule.type)
    rule.action = toNum(RULE_ACTION, rule.action)
    if (rule.strategy) rule.strategy = toNum(STRATEGY, rule.strategy)
  }
  for (const server of profile.dns?.servers ?? []) {
    server.type = toNum(DNS_SERVER_TYPE, server.type)
  }
  for (const rule of profile.dns?.rules ?? []) {
    rule.type = toNum(RULE_TYPE, rule.type)
    rule.action = toNum(DNS_RULE_ACTION, rule.action)
    if (rule.strategy) rule.strategy = toNum(STRATEGY, rule.strategy)
  }
  if (profile.dns?.strategy) profile.dns.strategy = toNum(STRATEGY, profile.dns.strategy)
  if (profile.mixin) {
    profile.mixin.priority = toNum(MIXIN_PRIORITY, profile.mixin.priority)
    profile.mixin.format = toNum(MIXIN_FORMAT, profile.mixin.format)
  }
  return profile
}

export const protoProfileToIProfile = (profile: Profile | undefined): IProfile => {
  return normalizeLoadedProfile(fromProtoFieldNames(deepClone(profile || {})))
}

const normalizeLoadedProfile = (raw: any): IProfile => {
  const template: IProfile = {
    id: sampleID(),
    name: '',
    log: Defaults.DefaultLog(),
    experimental: Defaults.DefaultExperimental(),
    inbounds: Defaults.DefaultInbounds(),
    outbounds: Defaults.DefaultOutbounds(),
    route: Defaults.DefaultRoute(),
    dns: Defaults.DefaultDns(),
    mixin: Defaults.DefaultMixin(),
    script: Defaults.DefaultScript(),
  }

  const profile: any = {
    ...template,
    ...raw,
    log: { ...template.log, ...(raw?.log || {}) },
    experimental: normalizeExperimental(raw?.experimental, template.experimental),
    inbounds: normalizeInbounds(raw?.inbounds, template.inbounds),
    outbounds: normalizeOutbounds(raw?.outbounds, template.outbounds),
    route: normalizeRoute(raw?.route, template.route),
    dns: normalizeDns(raw?.dns, template.dns),
    mixin: normalizeMixin(raw?.mixin, template.mixin),
    script: normalizeScript(raw?.script, template.script),
  }

  profile.log.level = normalizeLogLevel(profile.log.level)
  return profile as IProfile
}

const normalizeExperimental = (exp: any, fallback: IExperimental): IExperimental => {
  const cacheFileRaw = exp?.cache_file || exp?.cachefile || {}

  return {
    cache_file: {
      ...fallback.cache_file,
      ...cacheFileRaw,
      cache_id: cacheFileRaw.cache_id ?? cacheFileRaw.cacheid ?? fallback.cache_file.cache_id,
      store_fakeip: cacheFileRaw.store_fakeip ?? cacheFileRaw.storefakeip ?? fallback.cache_file.store_fakeip,
      store_rdrc: cacheFileRaw.store_rdrc ?? cacheFileRaw.storerdrc ?? fallback.cache_file.store_rdrc,
      rdrc_timeout: cacheFileRaw.rdrc_timeout ?? cacheFileRaw.rdrctimeout ?? fallback.cache_file.rdrc_timeout,
    },
  }
}

const normalizeInbounds = (inbounds: any[], fallback: IInbound[]): IInbound[] => {
  if (!Array.isArray(inbounds)) return fallback
  return inbounds.map((item) => {
    const type = normalizeInboundType(item?.type)
    const mixed = item?.mixed || {}
    const socks = item?.socks || {}
    const http = item?.http || {}
    const direct = item?.direct || {}
    const tun = item?.tun || {}

    return {
      ...item,
      type,
      mixed: normalizeInboundUser(mixed),
      socks: normalizeInboundUser(socks),
      http: normalizeInboundUser(http),
      direct: normalizeInboundDirect(direct),
      tun: tun
        ? {
          ...tun,
          interface_name: tun.interface_name ?? tun.interfacename ?? '',
          auto_route: tun.auto_route ?? tun.autoroute ?? false,
          strict_route: tun.strict_route ?? tun.strictroute ?? false,
          route_address: tun.route_address ?? tun.routeaddress ?? [],
          route_exclude_address: tun.route_exclude_address ?? tun.routeexcludeaddress ?? [],
          endpoint_independent_nat:
            tun.endpoint_independent_nat ?? tun.endpointindependentnat ?? false,
          stack: normalizeTunStack(tun.stack),
        }
        : undefined,
    }
  })
}

const normalizeInboundDirect = (raw: any) => {
  const normalized = normalizeInboundUser(raw)
  if (!normalized || typeof normalized !== 'object') return normalized
  return {
    ...normalized,
    network: normalizeInboundNetwork(normalized.network),
  }
}

const normalizeInboundUser = (raw: any) => {
  if (!raw || typeof raw !== 'object') return raw
  const listen = raw.listen || {}
  return {
    ...raw,
    listen: {
      ...listen,
      listen_port: listen.listen_port ?? listen.listenport ?? 0,
      tcp_fast_open: listen.tcp_fast_open ?? listen.tcpfastopen ?? false,
      tcp_multi_path: listen.tcp_multi_path ?? listen.tcpmultipath ?? false,
      udp_fragment: listen.udp_fragment ?? listen.udpfragment ?? false,
    },
  }
}

const normalizeOutbounds = (outbounds: any[], fallback: IOutbound[]): IOutbound[] => {
  if (!Array.isArray(outbounds)) return fallback
  return outbounds.map((item) => ({
    ...item,
    type: normalizeOutboundType(item?.type),
    interrupt_exist_connections:
      item?.interrupt_exist_connections ?? item?.interruptexistconnections ?? false,
  }))
}

const normalizeRoute = (route: any, fallback: IRoute): IRoute => {
  const r = route || {}
  const ruleSet = r.rule_set || r.ruleset || []
  return {
    ...fallback,
    ...r,
    rule_set: Array.isArray(ruleSet)
      ? ruleSet.map((item: any) => ({
        ...item,
        type: normalizeRulesetType(item?.type),
        format: normalizeRulesetFormat(item?.format),
        download_detour: item?.download_detour ?? item?.downloaddetour ?? '',
        update_interval: item?.update_interval ?? item?.updateinterval ?? '',
      }))
      : fallback.rule_set,
    rules: Array.isArray(r.rules)
      ? r.rules.map((item: any) => ({
        ...item,
        type: normalizeRuleType(item?.type),
        action: normalizeRuleAction(item?.action),
        strategy: normalizeStrategy(item?.strategy),
      }))
      : fallback.rules,
    auto_detect_interface: r.auto_detect_interface ?? r.autodetectinterface ?? fallback.auto_detect_interface,
    default_interface: r.default_interface ?? r.defaultinterface ?? fallback.default_interface,
    find_process: r.find_process ?? r.findprocess ?? fallback.find_process,
    default_domain_resolver: {
      ...fallback.default_domain_resolver,
      ...(r.default_domain_resolver || r.defaultdomainresolver || {}),
      client_subnet:
        (r.default_domain_resolver || r.defaultdomainresolver || {}).client_subnet ??
        (r.default_domain_resolver || r.defaultdomainresolver || {}).clientsubnet ??
        fallback.default_domain_resolver.client_subnet,
    },
  }
}

const normalizeDns = (dns: any, fallback: IDNS): IDNS => {
  const d = dns || {}
  return {
    ...fallback,
    ...d,
    disable_cache: d.disable_cache ?? d.disablecache ?? fallback.disable_cache,
    disable_expire: d.disable_expire ?? d.disableexpire ?? fallback.disable_expire,
    independent_cache: d.independent_cache ?? d.independentcache ?? fallback.independent_cache,
    client_subnet: d.client_subnet ?? d.clientsubnet ?? fallback.client_subnet,
    strategy: normalizeStrategy(d.strategy),
    servers: Array.isArray(d.servers)
      ? d.servers.map((item: any) => ({
        ...item,
        type: normalizeDnsServerType(item?.type),
        domain_resolver: item?.domain_resolver ?? item?.domainresolver ?? '',
        hosts_path: item?.hosts_path ?? item?.hostspath ?? [],
        server_port: item?.server_port ?? item?.serverport ?? '',
        inet4_range: item?.inet4_range ?? item?.inet4range ?? '',
        inet6_range: item?.inet6_range ?? item?.inet6range ?? '',
      }))
      : fallback.servers,
    rules: Array.isArray(d.rules)
      ? d.rules.map((item: any) => ({
        ...item,
        type: normalizeRuleType(item?.type),
        action: normalizeDnsRuleAction(item?.action),
        strategy: normalizeStrategy(item?.strategy),
        disable_cache: item?.disable_cache ?? item?.disablecache ?? false,
        client_subnet: item?.client_subnet ?? item?.clientsubnet ?? '',
      }))
      : fallback.rules,
  }
}

const normalizeMixin = (mixin: any, fallback: IMixin): IMixin => ({
  ...fallback,
  ...(mixin || {}),
  priority: normalizeMixinPriority(mixin?.priority),
  format: normalizeMixinFormat(mixin?.format),
})

const normalizeScript = (script: any, fallback: IScript): IScript => ({
  ...fallback,
  ...(script || {}),
})

const mapEnum = (value: any, mapping: Record<number, string>, fallback: string) => {
  if (typeof value === 'number' && mapping[value]) return mapping[value]
  if (typeof value === 'string' && value !== '') return value
  return fallback
}

const normalizeLogLevel = (v: any) =>
  mapEnum(v, { 1: 'trace', 2: 'debug', 3: 'info', 4: 'warn', 5: 'error', 6: 'fatal', 7: 'panic' }, 'info')
const normalizeInboundType = (v: any) =>
  mapEnum(v, { 1: 'mixed', 2: 'socks', 3: 'http', 4: 'tun', 5: 'direct' }, 'mixed')
const normalizeInboundNetwork = (v: any) => mapEnum(v, { 1: 'tcp', 2: 'udp' }, 'udp')
const normalizeOutboundType = (v: any) =>
  mapEnum(v, { 1: 'direct', 2: 'block', 3: 'selector', 4: 'urltest' }, 'selector')
const normalizeTunStack = (v: any) => mapEnum(v, { 1: 'system', 2: 'gvisor', 3: 'mixed' }, 'mixed')
const normalizeRulesetType = (v: any) => mapEnum(v, { 1: 'inline', 2: 'local', 3: 'remote' }, 'inline')
const normalizeRulesetFormat = (v: any) => mapEnum(v, { 1: 'source', 2: 'binary' }, 'source')
const normalizeRuleAction = (v: any) =>
  mapEnum(v, { 1: 'route', 2: 'route-options', 3: 'reject', 4: 'hijack-dns', 5: 'sniff', 6: 'resolve' }, 'route')
const normalizeDnsRuleAction = (v: any) =>
  mapEnum(v, { 1: 'route', 2: 'route-options', 3: 'reject', 4: 'predefined' }, 'route')
const normalizeStrategy = (v: any) =>
  mapEnum(v, { 1: 'default', 2: 'prefer_ipv4', 3: 'prefer_ipv6', 4: 'ipv4_only', 5: 'ipv6_only' }, 'default')
const normalizeDnsServerType = (v: any) =>
  mapEnum(v, { 1: 'local', 2: 'hosts', 3: 'tcp', 4: 'udp', 5: 'tls', 6: 'https', 7: 'quic', 8: 'h3', 9: 'dhcp', 10: 'fakeip', 11: 'tailscale' }, 'local')
const normalizeMixinPriority = (v: any) => mapEnum(v, { 1: 'mixin', 2: 'gui' }, 'mixin')
const normalizeMixinFormat = (v: any) => mapEnum(v, { 1: 'json', 2: 'yaml' }, 'yaml')

const normalizeRuleType = (v: any) =>
  mapEnum(
    v,
    {
      1: 'inbound',
      2: 'network',
      3: 'protocol',
      4: 'domain',
      5: 'domain_suffix',
      6: 'domain_keyword',
      7: 'domain_regex',
      8: 'source_ip_cidr',
      9: 'ip_cidr',
      10: 'ip_is_private',
      11: 'source_port',
      12: 'source_port_range',
      13: 'port',
      14: 'port_range',
      15: 'process_name',
      16: 'process_path',
      17: 'process_path_regex',
      18: 'clash_mode',
      19: 'rule_set',
      20: 'ip_accept_any',
      21: 'inline',
      22: 'InsertionPoint',
    },
    'inline',
  )
