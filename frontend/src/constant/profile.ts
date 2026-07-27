import {
  LogLevel,
  Inbound,
  InboundNetwork,
  Outbound,
  TunStack,
  RulesetType,
  RulesetFormat,
  RuleType,
  RuleAction,
  Strategy,
  DnsServer,
  ClashMode,
} from '@/enums/kernel'
import i18n from '@/lang'
import { sampleID } from '@/utils'

import { DefaultTestURL } from './app'

const { t } = i18n.global

const DefaultOutboundIds = {
  Select: 'outbound-select',
  Urltest: 'outbound-urltest',
  Direct: 'outbound-direct',
  Block: 'outbound-block',
  Fallback: 'outbound-fallback',
  Global: 'outbound-global',
}

const DefaultInboundIds = {
  MixedIn: 'mixed-in',
  Tun: 'tun-in',
}

const DefaultRulesetIds = {
  CATEGORY_ADS: 'Category-Ads',
  GEOIP_CN: 'GeoIP-CN',
  GEOSITE_CN: 'GeoSite-CN',
  GEOLOCATION_NOT_CN: 'GeoLocation-!CN',
  GEOSITE_PRIVATE: 'GeoSite-Private',
  GEOIP_PRIVATE: 'GeoIP-Private',
}

const DefaultDnsServersIds = {
  LocalDns: 'Local-DNS',
  RemoteDns: 'Remote-DNS',
  FakeIP: 'Fake-IP',
  LocalDnsResolver: 'Local-DNS-Resolver',
  RemoteDnsResolver: 'Remote-DNS-Resolver',
}

export const DefaultLog = (): ILog => ({
  disabled: false,
  level: LogLevel.Info,
  output: '',
  timestamp: false,
})

export const DefaultExperimental = (): IExperimental => ({
  cache_file: {
    enabled: true,
    path: 'cache.db',
    cache_id: sampleID(),
    store_fakeip: true,
    store_rdrc: true,
    rdrc_timeout: '7d',
  },
})

export const DefaultInboundSocks = (): NonNullable<IInbound['socks']> => ({
  listen: {
    listen: '127.0.0.1',
    listen_port: 20120,
    tcp_fast_open: false,
    tcp_multi_path: false,
    udp_fragment: false,
  },
  users: [],
})

export const DefaultInboundHttp = (): NonNullable<IInbound['http']> => ({
  listen: {
    listen: '127.0.0.1',
    listen_port: 20121,
    tcp_fast_open: false,
    tcp_multi_path: false,
    udp_fragment: false,
  },
  users: [],
})

export const DefaultInboundMixed = (): NonNullable<IInbound['mixed']> => ({
  listen: {
    listen: '127.0.0.1',
    listen_port: 20122,
    tcp_fast_open: false,
    tcp_multi_path: false,
    udp_fragment: false,
  },
  users: [],
})

export const DefaultInboundDirect = (): NonNullable<IInbound['direct']> => ({
  listen: {
    listen: '127.0.0.1',
    listen_port: 53,
    tcp_fast_open: false,
    tcp_multi_path: false,
    udp_fragment: false,
  },
  network: InboundNetwork.Udp,
})

export const DefaultInboundTun = (): NonNullable<IInbound['tun']> => ({
  interface_name: '',
  address: ['172.18.0.1/30', 'fdfe:dcba:9876::1/126'],
  mtu: 0,
  auto_route: true,
  auto_redirect: false,
  iproute2_table_index: undefined,
  iproute2_rule_index: undefined,
  strict_route: true,
  route_address: [],
  route_exclude_address: [],
  endpoint_independent_nat: false,
  stack: TunStack.Mixed,
})

export const DefaultInbounds = (): IInbound[] => [
  {
    id: DefaultInboundIds.MixedIn,
    type: Inbound.Mixed,
    tag: 'mixed-in',
    enable: true,
    mixed: DefaultInboundMixed(),
  },
  {
    id: DefaultInboundIds.Tun,
    type: Inbound.Tun,
    tag: 'tun-in',
    enable: false,
    tun: DefaultInboundTun(),
  },
]

export const DefaultOutbound = (): IOutbound => ({
  id: sampleID(),
  tag: '',
  type: Outbound.Selector,
  outbounds: [],
  interrupt_exist_connections: true,
  url: DefaultTestURL,
  interval: '3m',
  tolerance: 150,
  include: '',
  exclude: '',
  icon: '',
  hidden: false,
  interface: '',
  bridge_name: '',
})

export const DefaultOutbounds = (): IOutbound[] => [
  {
    id: DefaultOutboundIds.Select,
    tag: t('outbound.select'),
    type: Outbound.Selector,
    outbounds: [{ id: DefaultOutboundIds.Urltest, type: 'Built-in', tag: t('outbound.urltest') }],
    interrupt_exist_connections: true,
    url: '',
    interval: '3m',
    tolerance: 150,
    include: '',
    exclude: '',
    icon: '',
    hidden: false,
    interface: '',
    bridge_name: '',
  },
  {
    id: DefaultOutboundIds.Urltest,
    tag: t('outbound.urltest'),
    type: Outbound.Urltest,
    outbounds: [],
    interrupt_exist_connections: true,
    url: DefaultTestURL,
    interval: '3m',
    tolerance: 150,
    include: '',
    exclude: '',
    icon: '',
    hidden: false,
    interface: '',
    bridge_name: '',
  },
  {
    id: DefaultOutboundIds.Direct,
    tag: t('outbound.direct'),
    type: Outbound.Selector,
    outbounds: [
      { id: 'direct', type: 'Built-in', tag: 'direct' },
      { id: 'block', type: 'Built-in', tag: 'block' },
    ],
    interrupt_exist_connections: true,
    url: '',
    interval: '3m',
    tolerance: 150,
    include: '',
    exclude: '',
    icon: '',
    hidden: false,
    interface: '',
    bridge_name: '',
  },
  {
    id: DefaultOutboundIds.Block,
    tag: t('outbound.block'),
    type: Outbound.Selector,
    outbounds: [
      { id: 'block', type: 'Built-in', tag: 'block' },
      { id: 'direct', type: 'Built-in', tag: 'direct' },
    ],
    interrupt_exist_connections: true,
    url: '',
    interval: '3m',
    tolerance: 150,
    include: '',
    exclude: '',
    icon: '',
    hidden: false,
    interface: '',
    bridge_name: '',
  },
  {
    id: DefaultOutboundIds.Fallback,
    tag: t('outbound.fallback'),
    type: Outbound.Selector,
    outbounds: [
      { id: DefaultOutboundIds.Select, type: 'Built-in', tag: t('outbound.select') },
      { id: DefaultOutboundIds.Direct, type: 'Built-in', tag: t('outbound.direct') },
    ],
    interrupt_exist_connections: true,
    url: '',
    interval: '3m',
    tolerance: 150,
    include: '',
    exclude: '',
    icon: '',
    hidden: false,
    interface: '',
    bridge_name: '',
  },
  {
    id: DefaultOutboundIds.Global,
    tag: 'GLOBAL',
    type: Outbound.Selector,
    outbounds: [
      { id: DefaultOutboundIds.Select, type: 'Built-in', tag: t('outbound.select') },
      { id: DefaultOutboundIds.Urltest, type: 'Built-in', tag: t('outbound.urltest') },
      { id: DefaultOutboundIds.Direct, type: 'Built-in', tag: t('outbound.direct') },
      { id: DefaultOutboundIds.Block, type: 'Built-in', tag: t('outbound.block') },
      { id: DefaultOutboundIds.Fallback, type: 'Built-in', tag: t('outbound.fallback') },
    ],
    interrupt_exist_connections: true,
    url: '',
    interval: '3m',
    tolerance: 150,
    include: '',
    exclude: '',
    icon: '',
    hidden: false,
    interface: '',
    bridge_name: '',
  },
]

export const DefaultRouteRule = (): IRule => ({
  id: sampleID(),
  enable: true,
  invert: false,
  action: RuleAction.Route,
  inbound: [],
  ip_version: 0,
  network: [],
  preferred_by: [],
  protocol: [],
  domain: [],
  domain_suffix: [],
  domain_keyword: [],
  domain_regex: [],
  ip_cidr: [],
  source_ip_cidr: [],
  source_ip_is_private: false,
  ip_is_private: false,
  source_port: [],
  source_port_range: [],
  port: [],
  port_range: [],
  process_name: [],
  process_path: [],
  process_path_regex: [],
  clash_mode: '',
  rule_set: [],
  action_options: DefaultActionOptions(),
  raw: '',
})

export const DefaultActionOptions = (): IActionOptions => ({
  outbound: '',
  override_address: '',
  override_port: 0,
  network_strategy: '',
  network_type: [],
  fallback_network_type: [],
  fallback_delay: '',
  udp_disable_domain_unmapping: false,
  udp_connect: false,
  udp_timeout: '',
  tls_fragment: false,
  tls_fragment_fallback_delay: '',
  tls_record_fragment: false,
  tls_spoof: '',
  tls_spoof_method: '',
  method: 'default',
  no_drop: false,
  sniffer: [],
  timeout: '',
  server: '',
  strategy: '',
  disable_cache: false,
  disable_optimistic_cache: false,
  client_subnet: '',
})

const createDefaultRouteRule = (
  fields: Partial<Omit<IRule, 'action_options'>> & { action_options?: Partial<IActionOptions> },
): IRule => {
  const rule = DefaultRouteRule()
  return {
    ...rule,
    ...fields,
    action_options: { ...rule.action_options, ...fields.action_options },
  }
}

export const DefaultRouteRuleset = (): IRuleSet => ({
  id: sampleID(),
  type: RulesetType.Local,
  tag: '',
  format: RulesetFormat.Binary,
  url: '',
  download_detour: '',
  update_interval: '',
  rules: '',
  path: '',
})

export const DefaultRoute = (): IRoute => ({
  rules: [
    createDefaultRouteRule({
      inbound: [DefaultInboundIds.Tun],
      enable: false,
      action: RuleAction.Sniff,
    }),
    createDefaultRouteRule({ protocol: ['dns'], action: RuleAction.HijackDNS }),
    createDefaultRouteRule({
      clash_mode: ClashMode.Direct,
      action_options: { outbound: DefaultOutboundIds.Direct },
    }),
    createDefaultRouteRule({
      clash_mode: ClashMode.Global,
      action_options: { outbound: DefaultOutboundIds.Global },
    }),
    createDefaultRouteRule({ id: RuleType.InsertionPoint }),
    createDefaultRouteRule({
      network: ['icmp'],
      action_options: { outbound: DefaultOutboundIds.Direct },
    }),
    createDefaultRouteRule({
      protocol: ['quic'],
      action_options: { outbound: DefaultOutboundIds.Block },
    }),
    createDefaultRouteRule({
      rule_set: [DefaultRulesetIds.CATEGORY_ADS],
      action_options: { outbound: DefaultOutboundIds.Block },
    }),
    createDefaultRouteRule({
      rule_set: [DefaultRulesetIds.GEOSITE_PRIVATE],
      action_options: { outbound: DefaultOutboundIds.Direct },
    }),
    createDefaultRouteRule({
      rule_set: [DefaultRulesetIds.GEOSITE_CN],
      action_options: { outbound: DefaultOutboundIds.Direct },
    }),
    createDefaultRouteRule({
      rule_set: [DefaultRulesetIds.GEOIP_PRIVATE],
      action_options: { outbound: DefaultOutboundIds.Direct },
    }),
    createDefaultRouteRule({
      rule_set: [DefaultRulesetIds.GEOIP_CN],
      action_options: { outbound: DefaultOutboundIds.Direct },
    }),
    createDefaultRouteRule({
      rule_set: [DefaultRulesetIds.GEOLOCATION_NOT_CN],
      action_options: { outbound: DefaultOutboundIds.Select },
    }),
  ],
  rule_set: [
    {
      id: DefaultRulesetIds.CATEGORY_ADS,
      type: RulesetType.Remote,
      tag: DefaultRulesetIds.CATEGORY_ADS,
      format: RulesetFormat.Binary,
      url: 'https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@sing/geo/geosite/category-ads-all.srs',
      download_detour: DefaultOutboundIds.Direct,
      update_interval: '',
      rules: '',
      path: '',
    },
    {
      id: DefaultRulesetIds.GEOIP_PRIVATE,
      type: RulesetType.Remote,
      tag: DefaultRulesetIds.GEOIP_PRIVATE,
      format: RulesetFormat.Binary,
      url: 'https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@sing/geo/geoip/private.srs',
      download_detour: DefaultOutboundIds.Direct,
      update_interval: '',
      rules: '',
      path: '',
    },
    {
      id: DefaultRulesetIds.GEOSITE_PRIVATE,
      type: RulesetType.Remote,
      tag: DefaultRulesetIds.GEOSITE_PRIVATE,
      format: RulesetFormat.Binary,
      url: 'https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@sing/geo/geosite/private.srs',
      download_detour: DefaultOutboundIds.Direct,
      update_interval: '',
      rules: '',
      path: '',
    },
    {
      id: DefaultRulesetIds.GEOIP_CN,
      type: RulesetType.Remote,
      tag: DefaultRulesetIds.GEOIP_CN,
      format: RulesetFormat.Binary,
      url: 'https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@sing/geo/geoip/cn.srs',
      download_detour: DefaultOutboundIds.Direct,
      update_interval: '',
      rules: '',
      path: '',
    },
    {
      id: DefaultRulesetIds.GEOSITE_CN,
      type: RulesetType.Remote,
      tag: DefaultRulesetIds.GEOSITE_CN,
      format: RulesetFormat.Binary,
      url: 'https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@sing/geo/geosite/cn.srs',
      download_detour: DefaultOutboundIds.Direct,
      update_interval: '',
      rules: '',
      path: '',
    },
    {
      id: DefaultRulesetIds.GEOLOCATION_NOT_CN,
      type: RulesetType.Remote,
      tag: DefaultRulesetIds.GEOLOCATION_NOT_CN,
      format: RulesetFormat.Binary,
      url: 'https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@sing/geo/geosite/geolocation-!cn.srs',
      download_detour: DefaultOutboundIds.Direct,
      update_interval: '',
      rules: '',
      path: '',
    },
  ],
  auto_detect_interface: true,
  default_interface: '',
  final: DefaultOutboundIds.Fallback,
  find_process: false,
  default_domain_resolver: {
    server: DefaultDnsServersIds.LocalDns,
    client_subnet: '',
  },
})

export const DefaultDnsServer = (): IDNSServer => ({
  id: sampleID(),
  tag: '',
  type: DnsServer.Local,
  detour: '',
  domain_resolver: '',
  server: '',
  server_port: '',
  path: '',
  interface: '',
  inet4_range: '',
  inet6_range: '',
  hosts_path: [],
  predefined: {},
})

export const DefaultDnsServers = (): IDNSServer[] => [
  {
    id: DefaultDnsServersIds.FakeIP,
    tag: DefaultDnsServersIds.FakeIP,
    detour: '',
    type: DnsServer.FakeIP,
    domain_resolver: '',
    server: '',
    server_port: '',
    path: '',
    interface: '',
    inet4_range: '198.18.0.0/15',
    inet6_range: 'fc00::/18',
    hosts_path: [],
    predefined: {},
  },
  {
    id: DefaultDnsServersIds.LocalDns,
    tag: DefaultDnsServersIds.LocalDns,
    detour: '',
    type: DnsServer.Https,
    domain_resolver: DefaultDnsServersIds.LocalDnsResolver,
    server: '223.5.5.5',
    server_port: '443',
    path: '/dns-query',
    interface: '',
    inet4_range: '',
    inet6_range: '',
    hosts_path: [],
    predefined: {},
  },
  {
    id: DefaultDnsServersIds.LocalDnsResolver,
    tag: DefaultDnsServersIds.LocalDnsResolver,
    detour: '',
    type: DnsServer.Udp,
    domain_resolver: '',
    server: '223.5.5.5',
    server_port: '53',
    path: '',
    interface: '',
    inet4_range: '',
    inet6_range: '',
    hosts_path: [],
    predefined: {},
  },
  {
    id: DefaultDnsServersIds.RemoteDns,
    tag: DefaultDnsServersIds.RemoteDns,
    detour: DefaultOutboundIds.Select,
    type: DnsServer.Tls,
    domain_resolver: DefaultDnsServersIds.RemoteDnsResolver,
    server: '8.8.8.8',
    server_port: '853',
    path: '',
    interface: '',
    inet4_range: '',
    inet6_range: '',
    hosts_path: [],
    predefined: {},
  },
  {
    id: DefaultDnsServersIds.RemoteDnsResolver,
    tag: DefaultDnsServersIds.RemoteDnsResolver,
    detour: DefaultOutboundIds.Select,
    type: DnsServer.Udp,
    domain_resolver: '',
    server: '8.8.8.8',
    server_port: '53',
    path: '',
    interface: '',
    inet4_range: '',
    inet6_range: '',
    hosts_path: [],
    predefined: {},
  },
]

export const DefaultFakeIPDnsRule = () => ({
  __is_fake_ip: true,
  type: 'logical',
  mode: 'and',
  rules: [
    {
      domain_suffix: [
        '.lan',
        '.localdomain',
        '.example',
        '.invalid',
        '.localhost',
        '.test',
        '.local',
        '.home.arpa',
        '.msftconnecttest.com',
        '.msftncsi.com',
      ],
      invert: true,
    },
    {
      query_type: ['A', 'AAAA'],
    },
  ],
})

export const DefaultDnsActionOptions = (): IDNSActionOptions => ({
  server: '',
  disable_cache: false,
  disable_optimistic_cache: false,
  timeout: '',
  client_subnet: '',
  method: 'default',
  no_drop: false,
  rcode: 'NOERROR',
  answer: [],
  ns: [],
  extra: [],
})

export const DefaultDnsRule = (): IDNSRule => ({
  id: sampleID(),
  enable: true,
  action: RuleAction.Route,
  invert: false,
  inbound: [],
  clash_mode: '',
  ip_version: 0,
  query_type: [],
  network: [],
  protocol: [],
  preferred_by: [],
  domain: [],
  domain_suffix: [],
  domain_keyword: [],
  domain_regex: [],
  source_ip_cidr: [],
  source_ip_is_private: false,
  source_port: [],
  source_port_range: [],
  rule_set: [],
  rule_set_ip_cidr_match_source: false,
  match_response: false,
  ip_accept_any: false,
  ip_cidr: [],
  ip_is_private: false,
  response_rcode: '',
  response_answer: [],
  response_ns: [],
  response_extra: [],
  process_name: [],
  process_path: [],
  process_path_regex: [],
  action_options: DefaultDnsActionOptions(),
  raw: '',
})

const createDefaultDnsRule = (
  fields: Omit<Partial<IDNSRule>, 'action_options'> & {
    action_options?: Partial<IDNSActionOptions>
  },
): IDNSRule => {
  const rule = DefaultDnsRule()
  return {
    ...rule,
    ...fields,
    action_options: { ...rule.action_options, ...fields.action_options },
  }
}

export const DefaultDnsRules = (): IDNSRule[] => [
  createDefaultDnsRule({
    clash_mode: ClashMode.Direct,
    action_options: { server: DefaultDnsServersIds.LocalDns },
  }),
  createDefaultDnsRule({
    clash_mode: ClashMode.Global,
    action_options: { server: DefaultDnsServersIds.RemoteDns },
  }),
  createDefaultDnsRule({ id: RuleType.InsertionPoint }),
  createDefaultDnsRule({
    rule_set: [DefaultRulesetIds.GEOSITE_CN],
    action_options: { server: DefaultDnsServersIds.LocalDns },
  }),
  createDefaultDnsRule({
    enable: false,
    raw: JSON.stringify(DefaultFakeIPDnsRule(), null, 2),
    action_options: { server: DefaultDnsServersIds.FakeIP },
  }),
  createDefaultDnsRule({
    rule_set: [DefaultRulesetIds.GEOLOCATION_NOT_CN],
    action_options: { server: DefaultDnsServersIds.RemoteDns },
  }),
]

export const DefaultDns = (): IDNS => ({
  servers: DefaultDnsServers(),
  rules: DefaultDnsRules(),
  disable_cache: false,
  disable_expire: false,
  independent_cache: false,
  client_subnet: '',
  final: DefaultDnsServersIds.RemoteDns,
  strategy: Strategy.Default,
})

export const DefaultMixin = (): IProfile['mixin'] => {
  return { priority: 'mixin', format: 'json', config: '' }
}

export const DefaultScript = (): IProfile['script'] => {
  return { code: `const onGenerate = async (config) => {\n  return config\n}` }
}
