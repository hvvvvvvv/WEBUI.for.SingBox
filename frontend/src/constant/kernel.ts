import {
  ClashMode,
  InboundNetwork,
  Outbound,
  TunStack,
  LogLevel,
  RulesetFormat,
  RulesetType,
  Sniffer,
  Strategy,
  RuleActionReject,
  NetworkStrategy,
  NetworkType,
  TLSSpoofMethod,
  DnsServer,
} from '@/enums/kernel'

export const ModeOptions = [
  {
    label: 'kernel.global',
    value: ClashMode.Global,
    desc: 'kernel.globalDesc',
  },
  {
    label: 'kernel.rule',
    value: ClashMode.Rule,
    desc: 'kernel.ruleDesc',
  },
  {
    label: 'kernel.direct',
    value: ClashMode.Direct,
    desc: 'kernel.directDesc',
  },
]

export const LogLevelOptions = [
  {
    label: 'kernel.log.trace',
    value: LogLevel.Trace,
  },
  {
    label: 'kernel.log.debug',
    value: LogLevel.Debug,
  },
  {
    label: 'kernel.log.info',
    value: LogLevel.Info,
  },
  {
    label: 'kernel.log.warn',
    value: LogLevel.Warn,
  },
  {
    label: 'kernel.log.error',
    value: LogLevel.Error,
  },
  {
    label: 'kernel.log.fatal',
    value: LogLevel.Fatal,
  },
  {
    label: 'kernel.log.panic',
    value: LogLevel.Panic,
  },
]

export const InboundNetworkOptions = [
  { label: 'tcp', value: InboundNetwork.Tcp },
  { label: 'udp', value: InboundNetwork.Udp },
]

export const OutboundOptions = [
  { label: 'kernel.outbounds.direct', value: Outbound.Direct },
  { label: 'kernel.outbounds.bridge', value: Outbound.Bridge },
  { label: 'kernel.outbounds.block', value: Outbound.Block },
  { label: 'kernel.outbounds.selector', value: Outbound.Selector },
  { label: 'kernel.outbounds.urltest', value: Outbound.Urltest },
]

export const TunStackOptions = [
  { label: 'kernel.inbounds.tun.system', value: TunStack.System },
  { label: 'kernel.inbounds.tun.gvisor', value: TunStack.GVisor },
  { label: 'kernel.inbounds.tun.mixed', value: TunStack.Mixed },
]

export const RulesetTypeOptions = [
  { label: 'kernel.route.rule_set.type.inline', value: RulesetType.Inline },
  { label: 'kernel.route.rule_set.type.local', value: RulesetType.Local },
  { label: 'kernel.route.rule_set.type.remote', value: RulesetType.Remote },
]

export const RulesetFormatOptions = [
  { label: 'ruleset.format.source', value: RulesetFormat.Source },
  { label: 'ruleset.format.binary', value: RulesetFormat.Binary },
]

export const DomainStrategyOptions = [
  { label: 'kernel.strategy.default', value: Strategy.Default },
  { label: 'kernel.strategy.prefer_ipv4', value: Strategy.PreferIPv4 },
  { label: 'kernel.strategy.prefer_ipv6', value: Strategy.PreferIPv6 },
  { label: 'kernel.strategy.ipv4_only', value: Strategy.IPv4Only },
  { label: 'kernel.strategy.ipv6_only', value: Strategy.IPv6Only },
]

export const RouteRuleIPVersionOptions = [
  { label: 'IPV4', value: 4 },
  { label: 'IPV6', value: 6 },
]

export const RouteRuleNetworkOptions = [
  { label: 'TCP', value: 'tcp' },
  { label: 'UDP', value: 'udp' },
  { label: 'ICMP', value: 'icmp' },
]

export const RouteRulePreferredByOptions = [
  { label: 'Tailscale', value: 'tailscale' },
  { label: 'WireGuard', value: 'wireguard' },
  { label: 'Bridge', value: 'bridge' },
]

export const RouteRuleProtocolOptions = Object.values(Sniffer).map((value) => ({
  label: value,
  value,
}))

export const NetworkStrategyOptions = Object.values(NetworkStrategy).map((value) => ({
  label: value,
  value,
}))

export const NetworkTypeOptions = Object.values(NetworkType).map((value) => ({
  label: value,
  value,
}))

export const TLSSpoofMethodOptions = Object.values(TLSSpoofMethod).map((value) => ({
  label: value,
  value,
}))

export const RuleActionRejectOptions = [
  { label: 'kernel.route.rules.action.rejectDefault', value: RuleActionReject.Default },
  { label: 'kernel.route.rules.action.rejectDrop', value: RuleActionReject.Drop },
  { label: 'kernel.route.rules.action.rejectReply', value: RuleActionReject.Reply },
]

export const DnsServerTypeOptions = [
  { label: 'kernel.dns.type.local', value: DnsServer.Local },
  { label: 'kernel.dns.type.hosts', value: DnsServer.Hosts },
  { label: 'kernel.dns.type.tcp', value: DnsServer.Tcp },
  { label: 'kernel.dns.type.udp', value: DnsServer.Udp },
  { label: 'kernel.dns.type.tls', value: DnsServer.Tls },
  { label: 'kernel.dns.type.quic', value: DnsServer.Quic },
  { label: 'kernel.dns.type.https', value: DnsServer.Https },
  { label: 'kernel.dns.type.h3', value: DnsServer.H3 },
  { label: 'kernel.dns.type.dhcp', value: DnsServer.Dhcp },
  { label: 'kernel.dns.type.fakeip', value: DnsServer.FakeIP },
]

export const DnsRuleNetworkOptions = [
  { label: 'TCP', value: 'tcp' },
  { label: 'UDP', value: 'udp' },
]

export const DnsRulePreferredByOptions = [
  { label: 'Hosts', value: 'hosts' },
  { label: 'Local', value: 'local' },
  { label: 'mDNS', value: 'mdns' },
  { label: 'Tailscale', value: 'tailscale' },
  { label: 'OpenConnect', value: 'openconnect' },
  { label: 'Resolved', value: 'resolved' },
]

export const DnsQueryTypeOptions = [
  'A',
  'AAAA',
  'CNAME',
  'MX',
  'NS',
  'PTR',
  'SOA',
  'SRV',
  'TXT',
  'HTTPS',
  'SVCB',
  'CAA',
].map((value) => ({ label: value, value }))

export const DnsRcodeOptions = [
  'NOERROR',
  'FORMERR',
  'SERVFAIL',
  'NXDOMAIN',
  'NOTIMP',
  'REFUSED',
].map((value) => ({ label: value, value }))

export const DnsRuleActionRejectOptions = [
  { label: 'kernel.route.rules.action.rejectDefault', value: RuleActionReject.Default },
  { label: 'kernel.route.rules.action.rejectDrop', value: RuleActionReject.Drop },
]

export const RuleSnifferOptions = [
  { label: 'kernel.route.rules.sniffer.http', value: Sniffer.Http },
  { label: 'kernel.route.rules.sniffer.tls', value: Sniffer.Tls },
  { label: 'kernel.route.rules.sniffer.quic', value: Sniffer.Quic },
  { label: 'kernel.route.rules.sniffer.stun', value: Sniffer.Stun },
  { label: 'kernel.route.rules.sniffer.dns', value: Sniffer.Dns },
  { label: 'kernel.route.rules.sniffer.bittorrent', value: Sniffer.Bittorrent },
  { label: 'kernel.route.rules.sniffer.dtls', value: Sniffer.Dtls },
  { label: 'kernel.route.rules.sniffer.ssh', value: Sniffer.Ssh },
  { label: 'kernel.route.rules.sniffer.rdp', value: Sniffer.Rdp },
  { label: 'kernel.route.rules.sniffer.ntp', value: Sniffer.Ntp },
]

export const DefaultExcludeProtocols = 'direct|reject|selector|urltest|block|dns|shadowsocksr'

export const BuiltInOutbound = [Outbound.Direct, Outbound.Block]

export const DefaultConnections = () => {
  return {
    visibility: {
      'connection.inboundType': true,
      'connection.processInfo.processPath': false,
      'connection.domain': true,
      'connection.source': false,
      'connection.destination': false,
      'connection.rule': true,
      'connection.chainList': true,
      uplinkBps: true,
      downlinkBps: true,
      uplinkTotal: true,
      downlinkTotal: true,
      'connection.createdAt': true,
    },
    order: [
      'connection.inboundType',
      'connection.processInfo.processPath',
      'connection.domain',
      'connection.source',
      'connection.destination',
      'connection.rule',
      'connection.chainList',
      'uplinkBps',
      'downlinkBps',
      'uplinkTotal',
      'downlinkTotal',
      'connection.createdAt',
    ],
  }
}

export const DefaultCoreConfig = () => {
  return {
    env: {},
    args: [
      'run',
      '--disable-color',
      '-c',
      '$APP_BASE_PATH/$CORE_BASE_PATH/config.json',
      '-D',
      '$APP_BASE_PATH/$CORE_BASE_PATH',
    ],
  }
}
