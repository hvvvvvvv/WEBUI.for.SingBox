import { parse } from 'yaml'

import { ReadFile, WriteFile, createRpcClient } from '@/bridge'
import { CoreConfigFilePath } from '@/constant/kernel'
import { ProfileManagementService } from '../../gen/profile/v1/profile_management_service_pb'
import { ProfileService } from '../../gen/profile/v1/profile_service_pb'
import {
  DnsServer,
  Inbound,
  LogLevel,
  Outbound,
  RuleAction,
  RulesetType,
  RuleType,
  Strategy,
} from '@/enums/kernel'
import { Branch } from '@/enums/app'
import {
  useAppSettingsStore,
  useRulesetsStore,
  useSubscribesStore,
} from '@/stores'
import { deepAssign, deepClone, APP_TITLE, createTextMatcher } from '@/utils'

const _generateRule = (rule: IRule | IDNSRule, rule_set: IRuleSet[], inbounds: IInbound[]) => {
  const getInbound = (id: string) => inbounds.find((v) => v.id === id)?.tag
  const getRuleset = (id: string) => rule_set.find((v) => v.id === id)?.tag

  const extra: Recordable = { action: rule.action, invert: rule.invert ? true : undefined }
  if (rule.type === RuleType.Inline) {
    deepAssign(extra, JSON.parse(rule.payload))
  } else if (rule.type === RuleType.RuleSet) {
    extra[rule.type] = rule.payload.split(',').map((id) => getRuleset(id))
  } else if (rule.type === RuleType.Inbound) {
    extra[rule.type] = getInbound(rule.payload)
  } else if ([RuleType.IpIsPrivate, RuleType.IpAcceptAny].includes(rule.type as any)) {
    extra[rule.type] = rule.payload === 'true'
  } else if (rule.type === RuleType.ClashMode) {
    extra[rule.type] = rule.payload
  } else {
    extra[rule.type] = String(rule.payload)
      .split(',')
      .map((val) => {
        if ([RuleType.Port, RuleType.SourcePort].includes(rule.type as any)) {
          return Number(val)
        }
        return val
      })
    if (extra[rule.type].length === 1) {
      extra[rule.type] = extra[rule.type][0]
    }
  }
  return extra
}

const generateExperimental = (experimental: IExperimental, outbounds: IOutbound[]) => {
  const getOutbound = (id: string) => outbounds.find((v) => v.id === id)?.tag
  return {
    clash_api: {
      ...experimental.clash_api,
      external_ui_download_detour: getOutbound(experimental.clash_api.external_ui_download_detour),
    },
    cache_file: experimental.cache_file,
  }
}

const generateInbounds = (inbounds: IInbound[]) => {
  return inbounds.flatMap((inbound) => {
    if (!inbound.enable) return []
    if (inbound.type !== Inbound.Tun) {
      const users = inbound[inbound.type]!.users.map((user) => ({
        username: user.split(':')[0],
        password: user.split(':')[1],
      }))
      return {
        type: inbound.type,
        tag: inbound.tag,
        ...inbound[inbound.type]!.listen,
        users: users.length > 0 ? users : undefined,
      }
    }
    if (inbound.type === Inbound.Tun) {
      return {
        type: inbound.type,
        tag: inbound.tag,
        ...inbound.tun!,
        route_address: inbound.tun!.route_address?.length ? inbound.tun!.route_address : undefined,
        route_exclude_address: inbound.tun!.route_exclude_address?.length
          ? inbound.tun!.route_exclude_address
          : undefined,
      }
    }
  })
}

const generateOutbounds = async (outbounds: IOutbound[]) => {
  const result: Recordable[] = []
  const SubscriptionCache: Recordable<any[]> = {}
  const proxiesSet = new Set<any>()
  const builtInProxiesSet = new Set<string>()

  const subscribesStore = useSubscribesStore()

  for (const outbound of outbounds) {
    const _outbound: Recordable = {
      type: outbound.type,
      tag: outbound.tag,
    }
    if (outbound.type === Outbound.Urltest) {
      _outbound.url = outbound.url
      _outbound.interval = outbound.interval
      _outbound.tolerance = outbound.tolerance
    }
    if (outbound.type === Outbound.Selector || outbound.type === Outbound.Urltest) {
      _outbound.interrupt_exist_connections = outbound.interrupt_exist_connections
      _outbound.outbounds = []
      const isTagMatching = createTextMatcher(outbound.include, outbound.exclude)
      for (const proxy of outbound.outbounds) {
        if (proxy.type === 'Built-in') {
          if ([Outbound.Direct, Outbound.Block].includes(proxy.id as Outbound)) {
            builtInProxiesSet.add(proxy.id)
          }
          _outbound.outbounds.push(proxy.tag)
        } else {
          const subId = proxy.type === 'Subscription' ? proxy.id : proxy.type
          if (!SubscriptionCache[subId]) {
            const sub = subscribesStore.getSubscribeById(subId)
            if (sub) {
              const subStr = await ReadFile(sub.path)
              const proxies = JSON.parse(subStr)
              SubscriptionCache[subId] = proxies
            }
          }
          if (proxy.type === 'Subscription') {
            _outbound.outbounds.push(
              ...SubscriptionCache[subId]!.map((v) => v.tag).filter((tag) => isTagMatching(tag)),
            )
            SubscriptionCache[subId]!.forEach((v) => proxiesSet.add(v))
          } else {
            const _proxy = SubscriptionCache[subId]!.find((v) => v.tag === proxy.tag)
            if (_proxy && isTagMatching(_proxy.tag)) {
              _outbound.outbounds.push(_proxy.tag)
              proxiesSet.add(_proxy)
            }
          }
        }
      }
    }
    result.push(_outbound)
  }

  result.push(...proxiesSet)
  result.push(...Array.from(builtInProxiesSet).map((v) => ({ type: v, tag: v })))

  return result
}

const generateRoute = (route: IRoute, inbounds: IInbound[], outbounds: IOutbound[], dns: IDNS) => {
  const getOutbound = (id: string) => outbounds.find((v) => v.id === id)?.tag
  const getDnsServer = (id: string) => dns.servers.find((v) => v.id === id)?.tag
  const isInboundEnabled = (id: string) => inbounds.find((v) => v.id === id)?.enable

  const rulesetsStore = useRulesetsStore()

  const extra: Recordable = {}
  if (!route.auto_detect_interface) {
    extra.default_interface = route.default_interface
  }
  return {
    rules: route.rules.flatMap((rule) => {
      if (rule.type === RuleType.InsertionPoint || !rule.enable) {
        return []
      }
      if (rule.type === RuleType.Inbound && !isInboundEnabled(rule.payload)) {
        return []
      }
      const extra: Recordable = _generateRule(rule, route.rule_set, inbounds)

      if (rule.action === RuleAction.Route) {
        extra.outbound = getOutbound(rule.outbound)
      } else if (rule.action === RuleAction.RouteOptions) {
        deepAssign(extra, JSON.parse(rule.outbound))
      } else if (rule.action === RuleAction.Reject) {
        extra.method = rule.outbound
      } else if (rule.action === RuleAction.Sniff) {
        if (rule.sniffer.length) {
          extra.sniffer = rule.sniffer
        }
      } else if (rule.action === RuleAction.Resolve) {
        if (rule.strategy !== Strategy.Default) {
          extra.strategy = rule.strategy
        }
        extra.server = getDnsServer(rule.server)
      }
      if (rule.invert) {
        extra.invert = true
      }
      return extra
    }),
    rule_set: route.rule_set.map((ruleset) => {
      const extra: Recordable = {}
      if (ruleset.type === RuleType.Inline) {
        extra.rules = JSON.parse(ruleset.rules)
      } else if (ruleset.type === RulesetType.Local) {
        const _ruleset = rulesetsStore.getRulesetById(ruleset.path)
        extra.path = _ruleset?.path.replace('data/', '../')
        extra.format = ruleset.format
      } else if (ruleset.type === RulesetType.Remote) {
        extra.url = ruleset.url
        extra.format = ruleset.format
        extra.download_detour = getOutbound(ruleset.download_detour)
        if (ruleset.update_interval) {
          extra.update_interval = ruleset.update_interval
        }
      }
      return {
        tag: ruleset.tag,
        type: ruleset.type,
        ...extra,
      }
    }),
    auto_detect_interface: route.auto_detect_interface,
    find_process: route.find_process ? true : undefined,
    final: getOutbound(route.final),
    default_domain_resolver: {
      server: getDnsServer(route.default_domain_resolver.server),
    },
    ...extra,
  }
}

const generateDns = (
  dns: IDNS,
  rule_set: IRuleSet[],
  inbounds: IInbound[],
  outbounds: IOutbound[],
) => {
  const getOutbound = (id: string) => outbounds.find((v) => v.id === id)
  const getDnsServer = (id: string) => dns.servers.find((v) => v.id === id)?.tag
  const extra: Recordable = {}
  if (dns.strategy !== Strategy.Default) {
    extra.strategy = dns.strategy
  }
  if (dns.client_subnet) {
    extra.client_subnet = dns.client_subnet
  }
  return {
    servers: dns.servers.flatMap((server) => {
      const extra: Recordable = {}
      if (
        [
          DnsServer.Local,
          DnsServer.Tcp,
          DnsServer.Udp,
          DnsServer.Tls,
          DnsServer.Quic,
          DnsServer.Https,
          DnsServer.H3,
          DnsServer.Dhcp,
        ].includes(server.type as any)
      ) {
        if (server.detour) {
          const outbound = getOutbound(server.detour)
          if (outbound?.type !== Outbound.Direct) {
            extra.detour = outbound?.tag
          }
        }
        server.domain_resolver && (extra.domain_resolver = getDnsServer(server.domain_resolver))
        if (
          [
            DnsServer.Tcp,
            DnsServer.Udp,
            DnsServer.Tls,
            DnsServer.Quic,
            DnsServer.Https,
            DnsServer.H3,
          ].includes(server.type as any)
        ) {
          server.server_port && (extra.server_port = Number(server.server_port))
          extra.server = server.server
          if ([DnsServer.Https, DnsServer.H3].includes(server.type as any)) {
            server.path && (extra.path = server.path)
          }
        }
      }
      if (server.type === DnsServer.Hosts) {
        extra.path = server.hosts_path.reduce((p, c) => p.concat(c.split(',')), [] as string[])
        extra.predefined = Object.entries(server.predefined).reduce(
          (p, [k, v]) => ({ ...p, [k]: v.split(',') }),
          {},
        )
      } else if (server.type === DnsServer.Dhcp) {
        server.interface && (extra.interface = server.interface)
      } else if (server.type === DnsServer.FakeIP) {
        server.inet4_range && (extra.inet4_range = server.inet4_range)
        server.inet6_range && (extra.inet6_range = server.inet6_range)
      }
      return {
        tag: server.tag,
        type: server.type,
        ...extra,
      }
    }),
    rules: dns.rules.flatMap((rule) => {
      if (rule.type === RuleType.InsertionPoint || !rule.enable) {
        return []
      }
      const extra: Recordable = _generateRule(rule, rule_set, inbounds)
      if (rule.type === RuleType.Inline && rule.payload.includes('__is_fake_ip')) {
        if (!dns.servers.find((v) => v.type === DnsServer.FakeIP)) {
          return []
        }
        delete extra.__is_fake_ip
      }
      if ([RuleAction.Route, RuleAction.RouteOptions].includes(rule.action as any)) {
        rule.disable_cache && (extra.disable_cache = rule.disable_cache)
        rule.client_subnet && (extra.client_subnet = rule.client_subnet)
        if (rule.action === RuleAction.Route) {
          extra.server = getDnsServer(rule.server)
          if (rule.strategy !== Strategy.Default) {
            extra.strategy = rule.strategy
          }
        }
      }
      if ([RuleAction.RouteOptions, RuleAction.Predefined].includes(rule.action as any)) {
        deepAssign(extra, JSON.parse(rule.server))
      }
      if (rule.action === RuleAction.Reject) {
        extra.method = rule.server
      }
      return extra
    }),
    disable_cache: dns.disable_cache,
    disable_expire: dns.disable_expire,
    independent_cache: dns.independent_cache,
    final: getDnsServer(dns.final),
    ...extra,
  }
}

export const generateDnsServerURL = (dnsServer: IDNSServer) => {
  const { type, server_port, path, server, interface: _interface } = dnsServer
  let address = ''
  if (type == DnsServer.Https) {
    address = `https://${server}${server_port ? ':' + server_port : ''}${path ? path : ''}`
  } else if (type == DnsServer.H3) {
    address = `h3://${server}${server_port ? ':' + server_port : ''}${path ? path : ''}`
  } else if (type == DnsServer.Dhcp) {
    address = `dhcp://${_interface}`
  } else if (type == DnsServer.FakeIP) {
    address =
      'fake-ip://' +
      (dnsServer.inet4_range ? dnsServer.inet4_range : '') +
      (dnsServer.inet6_range ? (dnsServer.inet4_range ? ',' : '') + dnsServer.inet6_range : '')
  } else if (type === DnsServer.Hosts) {
    address = 'hosts'
  } else if (type === DnsServer.Local) {
    address = 'local'
  } else {
    address = `${type}://${server}${server_port ? ':' + server_port : ''}`
  }
  return address
}

const _adaptToStableBranch = (_: Recordable) => {}

type GenerateConfigOptions = {
  enableStableConfigCompat?: boolean
  enableMixinProcessing?: boolean
  enableScriptProcessing?: boolean
}

const resolveGenerateOptions = (options: GenerateConfigOptions = {}) => {
  if (typeof options === 'boolean') {
    options = { enableStableConfigCompat: options }
  }

  const appSettings = useAppSettingsStore()
  const isMainBranch = appSettings.app.kernel.branch === Branch.Main

  return {
    enableStableConfigCompat: options.enableStableConfigCompat ?? isMainBranch,
    enableMixinProcessing: options.enableMixinProcessing ?? true,
    enableScriptProcessing: options.enableScriptProcessing ?? true,
  }
}

// Converts IProfile string enums to proto numeric enums for RPC serialization.
const _LOG_LEVEL: Record<string, number> = {
  trace: 1, debug: 2, info: 3, warn: 4, error: 5, fatal: 6, panic: 7,
}
const _INBOUND_TYPE: Record<string, number> = { mixed: 1, socks: 2, http: 3, tun: 4 }
const _OUTBOUND_TYPE: Record<string, number> = { direct: 1, block: 2, selector: 3, urltest: 4 }
const _TUN_STACK: Record<string, number> = { system: 1, gvisor: 2, mixed: 3 }
const _RULESET_TYPE: Record<string, number> = { inline: 1, local: 2, remote: 3 }
const _RULESET_FORMAT: Record<string, number> = { source: 1, binary: 2 }
const _RULE_TYPE: Record<string, number> = {
  inbound: 1, network: 2, protocol: 3, domain: 4, domain_suffix: 5, domain_keyword: 6,
  domain_regex: 7, source_ip_cidr: 8, ip_cidr: 9, ip_is_private: 10,
  source_port: 11, source_port_range: 12, port: 13, port_range: 14,
  process_name: 15, process_path: 16, process_path_regex: 17, clash_mode: 18,
  rule_set: 19, ip_accept_any: 20, inline: 21, InsertionPoint: 22,
}
const _STRATEGY: Record<string, number> = {
  default: 1, prefer_ipv4: 2, prefer_ipv6: 3, ipv4_only: 4, ipv6_only: 5,
}
const _DNS_SERVER_TYPE: Record<string, number> = {
  local: 1, hosts: 2, tcp: 3, udp: 4, tls: 5, https: 6, quic: 7, h3: 8, dhcp: 9, fakeip: 10, tailscale: 11,
}
const _RULE_ACTION: Record<string, number> = {
  route: 1, 'route-options': 2, reject: 3, 'hijack-dns': 4, sniff: 5, resolve: 6,
}
const _DNS_RULE_ACTION: Record<string, number> = { route: 1, 'route-options': 2, reject: 3, predefined: 4 }
const _MIXIN_PRIORITY: Record<string, number> = { mixin: 1, gui: 2 }
const _MIXIN_FORMAT: Record<string, number> = { json: 1, yaml: 2 }

const protoFieldNames: Record<string, string> = {
  access_control_allow_origin: 'accessControlAllowOrigin',
  access_control_allow_private_network: 'accessControlAllowPrivateNetwork',
  auto_detect_interface: 'autoDetectInterface',
  auto_route: 'autoRoute',
  cache_file: 'cacheFile',
  cache_id: 'cacheId',
  clash_api: 'clashApi',
  client_subnet: 'clientSubnet',
  default_domain_resolver: 'defaultDomainResolver',
  default_interface: 'defaultInterface',
  default_mode: 'defaultMode',
  disable_cache: 'disableCache',
  disable_expire: 'disableExpire',
  domain_resolver: 'domainResolver',
  download_detour: 'downloadDetour',
  endpoint_independent_nat: 'endpointIndependentNat',
  external_controller: 'externalController',
  external_ui: 'externalUi',
  external_ui_download_detour: 'externalUiDownloadDetour',
  external_ui_download_url: 'externalUiDownloadUrl',
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
    const protoKey = protoFieldNames[key] ?? key
    result[protoKey] = toProtoFieldNames(item, protoKey)
    return result
  }, {})
}

function iProfileToProto(p: IProfile): any {
  const profile = toProtoFieldNames(deepClone(p)) as any
  if (profile.log) profile.log.level = toNum(_LOG_LEVEL, profile.log.level)
  for (const inbound of profile.inbounds ?? []) {
    inbound.type = toNum(_INBOUND_TYPE, inbound.type)
    if (inbound.tun) inbound.tun.stack = toNum(_TUN_STACK, inbound.tun.stack)
  }
  for (const outbound of profile.outbounds ?? []) {
    outbound.type = toNum(_OUTBOUND_TYPE, outbound.type)
  }
  for (const rs of profile.route?.ruleSet ?? []) {
    rs.type = toNum(_RULESET_TYPE, rs.type)
    rs.format = toNum(_RULESET_FORMAT, rs.format)
  }
  for (const rule of profile.route?.rules ?? []) {
    rule.type = toNum(_RULE_TYPE, rule.type)
    rule.action = toNum(_RULE_ACTION, rule.action)
    if (rule.strategy) rule.strategy = toNum(_STRATEGY, rule.strategy)
  }
  for (const server of profile.dns?.servers ?? []) {
    server.type = toNum(_DNS_SERVER_TYPE, server.type)
  }
  for (const rule of profile.dns?.rules ?? []) {
    rule.type = toNum(_RULE_TYPE, rule.type)
    rule.action = toNum(_DNS_RULE_ACTION, rule.action)
    if (rule.strategy) rule.strategy = toNum(_STRATEGY, rule.strategy)
  }
  if (profile.dns?.strategy) profile.dns.strategy = toNum(_STRATEGY, profile.dns.strategy)
  if (profile.mixin) {
    profile.mixin.priority = toNum(_MIXIN_PRIORITY, profile.mixin.priority)
    profile.mixin.format = toNum(_MIXIN_FORMAT, profile.mixin.format)
  }
  return profile
}

export const generateConfigViaRpcByProfile = async (
  profile: IProfile,
  options: GenerateConfigOptions = {},
): Promise<Recordable> => {
  const profileServiceClient = createRpcClient(ProfileService)
  const resolved = resolveGenerateOptions(options)

  const result = await profileServiceClient.generateConfig({
    profile: iProfileToProto(profile),
    options: {
      enableStableConfigCompat: resolved.enableStableConfigCompat,
      enableMixinProcessing: resolved.enableMixinProcessing,
      enableScriptProcessing: resolved.enableScriptProcessing,
    },
  })

  if (!result.config) throw new Error('Empty config response from server')
  return result.config as Recordable
}

export const generateConfigViaRpc = async (profileId: string): Promise<Recordable> => {
  const profileMgmtClient = createRpcClient(ProfileManagementService)
  const { profile } = await profileMgmtClient.getProfile({ id: profileId })
  if (!profile) throw new Error(`Profile "${profileId}" not found`)
  return generateConfigViaRpcByProfile(profile as any)
}

export const generateConfig = async (
  originalProfile: IProfile,
  options: GenerateConfigOptions = {},
) => {
  const {
    enableStableConfigCompat,
    enableMixinProcessing,
    enableScriptProcessing,
  } = resolveGenerateOptions(options)

  const profile = deepClone(originalProfile)
  // step 1
  let config: Recordable = {
    log: profile.log,
    experimental: generateExperimental(profile.experimental, profile.outbounds),
    inbounds: generateInbounds(profile.inbounds),
    outbounds: await generateOutbounds(profile.outbounds),
    route: generateRoute(profile.route, profile.inbounds, profile.outbounds, profile.dns),
    dns: generateDns(profile.dns, profile.route.rule_set, profile.inbounds, profile.outbounds),
  }

  // adapt to stable branch
  if (enableStableConfigCompat) {
    _adaptToStableBranch(config)
  }

  // step 2
  if (enableMixinProcessing) {
    const { priority, config: mixin } = originalProfile.mixin
    if (priority === 'mixin') {
      deepAssign(config, parse(mixin))
    } else if (priority === 'gui') {
      deepAssign(config, deepAssign(parse(mixin), config))
    }
  }

  // step 3
  if (enableScriptProcessing) {
    const fn = new window.AsyncFunction(
      'config',
      `${originalProfile.script.code}; return await onGenerate(config)`,
    )
    try {
      config = await fn(config)
    } catch (error: any) {
      throw error.message || error
    }

    if (typeof config !== 'object') {
      throw 'Wrong result'
    }
  }

  return config
}

export const generateConfigFile = async (
  profile: IProfile,
  beforeWrite: (config: any) => Promise<any>,
) => {
  const header = `DO NOT EDIT - Generated by ${APP_TITLE}`

  const _config = await generateConfigViaRpcByProfile(profile)
  const config = await beforeWrite(_config)

  config.log.disabled = false
  config.log.output = ''
  if (![LogLevel.Trace, LogLevel.Debug, LogLevel.Info].includes(config.log.level)) {
    config.log.level = LogLevel.Info
  }

  config.experimental.cache_file.path = 'cache.db'

  await WriteFile(CoreConfigFilePath, JSON.stringify({ $schema: header, ...config }, null, 2))
}
