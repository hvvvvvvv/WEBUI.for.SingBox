type LogLevel = 'trace' | 'debug' | 'info' | 'warn' | 'error' | 'fatal' | 'panic'
interface ILog {
  disabled: boolean
  level: LogLevel
  output: string
  timestamp: boolean
}

interface IExperimental {
  cache_file: {
    enabled: boolean
    path: string
    cache_id: string
    store_fakeip: boolean
    store_rdrc: boolean
    rdrc_timeout: string
  }
}

interface IProxy {
  id: string
  type: string
  tag: string
}

type RuleSetType = 'inline' | 'local' | 'remote'
type RuleSetFormat = 'source' | 'binary'
interface IRuleSet {
  id: string
  type: RuleSetType
  tag: string
  // inline
  rules: string
  // local
  path: string
  // remote
  url: string
  download_detour: string
  update_interval: string
  // local or remote
  format: RuleSetFormat
}

type InboundType = 'mixed' | 'socks' | 'http' | 'tun' | 'direct'
type InboundNetworkType = 'tcp' | 'udp'
type InboundListen = {
  listen: string
  listen_port: number
  tcp_fast_open: boolean
  tcp_multi_path: boolean
  udp_fragment: boolean
}

interface IInbound {
  id: string
  type: InboundType
  tag: string
  enable: boolean
  mixed?: {
    listen: InboundListen
    users: string[]
  }
  socks?: {
    listen: InboundListen
    users: string[]
  }
  http?: {
    listen: InboundListen
    users: string[]
  }
  direct?: {
    listen: InboundListen
    network: InboundNetworkType
  }
  tun?: {
    interface_name: string
    address: string[]
    mtu: number
    auto_route: boolean
    auto_redirect: boolean
    strict_route: boolean
    route_address: string[]
    route_exclude_address: string[]
    endpoint_independent_nat: boolean
    stack: TunStackEnum
  }
}

type OutboundType = 'direct' | 'bridge' | 'block' | 'selector' | 'urltest'

type RuleAction =
  | 'route'
  | 'bypass'
  | 'route-options'
  | 'reject'
  | 'hijack-dns'
  | 'sniff'
  | 'resolve'
  | 'inline'
type DnsRuleAction =
  | 'route'
  | 'evaluate'
  | 'respond'
  | 'route-options'
  | 'reject'
  | 'predefined'
  | 'inline'

interface IOutbound {
  id: string
  tag: string
  type: OutboundType
  outbounds: IProxy[]
  url: string
  interval: string
  tolerance: number
  interrupt_exist_connections: boolean
  // gui
  include: string
  exclude: string
  icon: string
  hidden: boolean
  interface: string
  bridge_name: string
}

type RuleType =
  | 'inbound'
  | 'network'
  | 'protocol'
  | 'domain'
  | 'domain_suffix'
  | 'domain_keyword'
  | 'domain_regex'
  | 'source_ip_cidr'
  | 'ip_cidr'
  | 'source_port'
  | 'source_port_range'
  | 'port'
  | 'port_range'
  | 'process_name'
  | 'process_path'
  | 'process_path_regex'
  | 'rule_set'
  | 'ip_is_private'
  | 'clash_mode'
  | 'outbound'
  | 'inline'
  | 'InsertionPoint'

interface IActionOptions {
  outbound: string
  override_address: string
  override_port: number
  network_strategy: string
  network_type: string[]
  fallback_network_type: string[]
  fallback_delay: string
  udp_disable_domain_unmapping: boolean
  udp_connect: boolean
  udp_timeout: string
  tls_fragment: boolean
  tls_fragment_fallback_delay: string
  tls_record_fragment: boolean
  tls_spoof: string
  tls_spoof_method: string
  method: string
  no_drop: boolean
  sniffer: string[]
  timeout: string
  server: string
  strategy: string
  disable_cache: boolean
  disable_optimistic_cache: boolean
  rewrite_ttl?: number
  client_subnet: string
}

interface IRule {
  id: string
  enable: boolean
  invert: boolean
  action: RuleAction
  inbound: string[]
  ip_version: number
  network: string[]
  preferred_by: string[]
  protocol: string[]
  domain: string[]
  domain_suffix: string[]
  domain_keyword: string[]
  domain_regex: string[]
  ip_cidr: string[]
  source_ip_cidr: string[]
  source_ip_is_private: boolean
  ip_is_private: boolean
  source_port: number[]
  source_port_range: string[]
  port: number[]
  port_range: string[]
  process_name: string[]
  process_path: string[]
  process_path_regex: string[]
  clash_mode: string
  rule_set: string[]
  action_options: IActionOptions
  raw: string
}

interface IRoute {
  rules: IRule[]
  rule_set: IRuleSet[]
  final: string
  auto_detect_interface: boolean
  default_interface: string
  find_process: boolean
  default_domain_resolver: {
    server: string
    client_subnet: string
  }
}

type Strategy = 'default' | 'prefer_ipv4' | 'prefer_ipv6' | 'ipv4_only' | 'ipv6_only'
type DNSServer =
  | 'local'
  | 'hosts'
  | 'tcp'
  | 'udp'
  | 'tls'
  | 'quic'
  | 'https'
  | 'h3'
  | 'dhcp'
  | 'fakeip'
  | 'tailscale'

interface IDNSServer {
  id: string
  tag: string
  type: DNSServer
  // [local,tcp,udp,tls,quic,https/h3,dhcp]
  detour: string
  domain_resolver: string
  // hosts
  hosts_path: string[]
  predefined: Recordable
  // [tcp,udp,tls,quic/https,h3]
  server: string
  server_port: string
  // [https,h3]
  path: string
  // dhcp
  interface: string
  // fakeip
  inet4_range: string
  inet6_range: string
}

interface IDNSActionOptions {
  server: string
  disable_cache: boolean
  disable_optimistic_cache: boolean
  rewrite_ttl?: number
  timeout: string
  client_subnet: string
  method: string
  no_drop: boolean
  rcode: string
  answer: string[]
  ns: string[]
  extra: string[]
}

interface IDNSRule {
  id: string
  enable: boolean
  action: DnsRuleAction
  invert: boolean
  inbound: string[]
  clash_mode: string
  ip_version: number
  query_type: string[]
  network: string[]
  protocol: string[]
  preferred_by: string[]
  domain: string[]
  domain_suffix: string[]
  domain_keyword: string[]
  domain_regex: string[]
  source_ip_cidr: string[]
  source_ip_is_private: boolean
  source_port: number[]
  source_port_range: string[]
  rule_set: string[]
  rule_set_ip_cidr_match_source: boolean
  match_response: boolean
  ip_accept_any: boolean
  ip_cidr: string[]
  ip_is_private: boolean
  response_rcode: string
  response_answer: string[]
  response_ns: string[]
  response_extra: string[]
  process_name: string[]
  process_path: string[]
  process_path_regex: string[]
  action_options: IDNSActionOptions
  raw: string
}

interface IDNS {
  servers: IDNSServer[]
  rules: IDNSRule[]
  disable_cache: boolean
  disable_expire: boolean
  independent_cache: boolean
  client_subnet: string
  final: string
  strategy: Strategy
}

type MixinPriority = 'mixin' | 'gui'

interface IMixin {
  priority: MixinPriority
  format: 'json' | 'yaml'
  config: string
}

interface IScript {
  code: string
}

interface IProfile {
  id: string
  name: string
  log: ILog
  experimental: IExperimental
  inbounds: IInbound[]
  outbounds: IOutbound[]
  route: IRoute
  dns: IDNS
  mixin: IMixin
  script: IScript
}
