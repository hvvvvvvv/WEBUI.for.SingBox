import * as Defaults from '@/constant/profile'
import { Inbound, Outbound, RuleAction, RulesetType, DnsServer } from '@/enums/kernel'

import { createTextMatcher, deepAssign, sampleID } from './others'
import { useProfilesStore, useRulesetsStore, useSubscribesStore } from '@/stores'
import type { Subscription } from '@/types/app'

const routeArrayMatchKeys = [
  'network',
  'preferred_by',
  'protocol',
  'domain',
  'domain_suffix',
  'domain_keyword',
  'domain_regex',
  'ip_cidr',
  'source_ip_cidr',
  'source_port',
  'source_port_range',
  'port',
  'port_range',
  'process_name',
  'process_path',
  'process_path_regex',
] as const

const routeOptionKeys = [
  'override_address',
  'override_port',
  'network_strategy',
  'network_type',
  'fallback_network_type',
  'fallback_delay',
  'udp_disable_domain_unmapping',
  'udp_connect',
  'udp_timeout',
  'tls_fragment',
  'tls_fragment_fallback_delay',
  'tls_record_fragment',
  'tls_spoof',
  'tls_spoof_method',
] as const

const buildTagIdMapping = (prefix: string, arr?: Recordable[]): Recordable<string> => {
  if (!arr) return {}
  return arr.reduce((p, c, i) => ((p[c.tag] = prefix + i), p), {})
}

const restoreReference = (mapping: Recordable<string>, value: unknown): string => {
  if (typeof value !== 'string' || value === '') return ''
  return mapping[value] ?? value
}

type RestoreProfileOptions = {
  profile?: IProfile
  subscriptionIds?: string[]
}

export const restoreProfile = (
  config: Recordable,
  name = sampleID(),
  options: RestoreProfileOptions = {},
): IProfile => {
  const template = useProfilesStore().getProfileTemplate()

  const { profile, subscriptionIds } = options

  const InboundsIds = buildTagIdMapping('in-', config.inbounds)
  const OutboundsIds = buildTagIdMapping('out-', config.outbounds)
  const RouteRuleSetIds = buildTagIdMapping('ruleset-', config.route?.rule_set)
  const DnsServersIds = buildTagIdMapping('dns-', config.dns?.servers)

  return {
    id: profile?.id || sampleID(),
    name,
    log: deepAssign(Defaults.DefaultLog(), config.log),
    experimental: restoreExperimental(config.experimental),
    inbounds: restoreInbounds(config.inbounds || [], InboundsIds),
    outbounds: restoreOutbounds(
      config.outbounds || [],
      OutboundsIds,
      profile?.outbounds || [],
      subscriptionIds || [],
    ),
    route: {
      rule_set: restoreRouteRuleset(config.route?.rule_set || [], RouteRuleSetIds, OutboundsIds),
      rules: restoreRouteRules(
        config.route?.rules || [],
        InboundsIds,
        OutboundsIds,
        RouteRuleSetIds,
        DnsServersIds,
      ),
      auto_detect_interface:
        config.route?.auto_detect_interface ?? template.route.auto_detect_interface,
      find_process: config.route?.find_process ?? template.route.find_process,
      default_interface: config.route?.default_interface ?? template.route.default_interface,
      final: restoreReference(OutboundsIds, config.route?.final),
      default_domain_resolver: {
        server: restoreReference(DnsServersIds, config.route?.default_domain_resolver?.server),
        client_subnet:
          config.route?.default_domain_resolver?.client_subnet ??
          template.route.default_domain_resolver.client_subnet,
      },
    },
    dns: {
      disable_cache: config.dns?.disable_cache ?? template.dns.disable_cache,
      disable_expire: config.dns?.disable_expire ?? template.dns.disable_expire,
      independent_cache: config.dns?.independent_cache ?? template.dns.independent_cache,
      final: restoreReference(DnsServersIds, config.dns?.final),
      strategy: config.dns?.strategy ?? template.dns.strategy,
      client_subnet: config.dns?.client_subnet ?? template.dns.client_subnet,
      servers: restoreDnsServers(config.dns?.servers || [], DnsServersIds, OutboundsIds),
      rules: restoreDnsRules(config.dns?.rules || [], InboundsIds, RouteRuleSetIds, DnsServersIds),
    },
    mixin: profile?.mixin || Defaults.DefaultMixin(),
    script: profile?.script || Defaults.DefaultScript(),
  }
}

const restoreExperimental = (raw: Recordable): IExperimental => {
  const template = Defaults.DefaultExperimental()
  return {
    ...template,
    cache_file: deepAssign(template.cache_file, raw?.cache_file || {}),
  }
}

const restoreInbounds = (inbounds: Recordable[], InboundsIds: Recordable): IInbound[] => {
  return inbounds.flatMap((raw) => {
    if (
      ![Inbound.Mixed, Inbound.Http, Inbound.Socks, Inbound.Tun, Inbound.Direct].includes(raw.type)
    )
      return []
    const inbound: IInbound = {
      id: InboundsIds[raw.tag],
      tag: raw.tag,
      type: raw.type,
      enable: true,
    }
    if (raw.type === Inbound.Tun) {
      const template = Defaults.DefaultInboundTun()
      inbound.tun = {
        interface_name: raw.interface_name ?? template.interface_name,
        address: raw.address ?? template.address,
        mtu: raw.mtu ?? template.mtu,
        auto_route: raw.auto_route ?? template.auto_route,
        auto_redirect: raw.auto_redirect ?? template.auto_redirect,
        iproute2_table_index: raw.iproute2_table_index ?? template.iproute2_table_index,
        iproute2_rule_index: raw.iproute2_rule_index ?? template.iproute2_rule_index,
        strict_route: raw.strict_route ?? template.strict_route,
        route_address: raw.route_address ?? template.route_address,
        route_exclude_address: raw.route_exclude_address ?? template.route_exclude_address,
        endpoint_independent_nat: raw.endpoint_independent_nat ?? template.endpoint_independent_nat,
        stack: raw.stack ?? template.stack,
      }
    }
    if ([Inbound.Mixed, Inbound.Http, Inbound.Socks].includes(raw.type)) {
      const template = Defaults.DefaultInboundMixed()
      inbound[raw.type as Inbound.Mixed | Inbound.Http | Inbound.Socks] = {
        listen: {
          listen: raw.listen ?? template.listen.listen,
          listen_port: raw.listen_port ?? template.listen.listen_port,
          tcp_fast_open: raw.tcp_fast_open ?? template.listen.tcp_fast_open,
          tcp_multi_path: raw.tcp_multi_path ?? template.listen.tcp_multi_path,
          udp_fragment: raw.udp_fragment ?? template.listen.udp_fragment,
        },
        users: raw.users?.map((user: any) => user.username + ':' + user.password) ?? template.users,
      }
    }
    if (raw.type === Inbound.Direct) {
      const template = Defaults.DefaultInboundDirect()
      inbound.direct = {
        listen: {
          listen: raw.listen ?? template.listen.listen,
          listen_port: raw.listen_port ?? template.listen.listen_port,
          tcp_fast_open: raw.tcp_fast_open ?? template.listen.tcp_fast_open,
          tcp_multi_path: raw.tcp_multi_path ?? template.listen.tcp_multi_path,
          udp_fragment: raw.udp_fragment ?? template.listen.udp_fragment,
        },
        network: raw.network === 'tcp' || raw.network === 'udp' ? raw.network : template.network,
      }
    }
    return inbound
  })
}

const restoreOutbounds = (
  outbounds: Recordable[],
  OutboundsIds: Recordable,
  originalOutbounds: IOutbound[],
  subscriptionIds: string[],
): IOutbound[] => {
  const subscribesStore = useSubscribesStore()

  const subscriptionCache = new Map<string, Subscription>()
  const proxyToSubMap = new Map<string, { sub: string; id: string }>()
  const originalOutboundMap = new Map<string, IOutbound>()

  const groupTags = new Set(
    outbounds
      .filter((o: Recordable) => [Outbound.Selector, Outbound.Urltest].includes(o.type))
      .map((o: Recordable) => o.tag),
  )

  subscriptionIds.forEach((id) => {
    const sub = subscribesStore.getSubscribeById(id)
    if (sub) {
      subscriptionCache.set(id, sub)
      sub.proxies.forEach((proxy) => {
        proxyToSubMap.set(proxy.tag, { sub: id, id: proxy.id })
      })
    }
  })

  originalOutbounds.forEach((outbound) => {
    originalOutboundMap.set(outbound.tag, outbound)
  })

  return outbounds.flatMap((raw) => {
    if (raw.type === Outbound.Bridge) {
      const outbound = Defaults.DefaultOutbound()
      outbound.id = OutboundsIds[raw.tag]
      outbound.tag = raw.tag
      outbound.type = Outbound.Bridge
      outbound.interface = raw.interface ?? ''
      outbound.bridge_name = raw.bridge_name ?? ''
      return outbound
    }
    if (![Outbound.Selector, Outbound.Urltest].includes(raw.type)) {
      return []
    }
    const outbound = Defaults.DefaultOutbound()
    outbound.id = OutboundsIds[raw.tag]
    outbound.tag = raw.tag
    outbound.type = raw.type

    let newOutbounds: IProxy[] = []

    raw.outbounds?.forEach((tag: string) => {
      const isBuiltIn = [Outbound.Direct, Outbound.Block].includes(tag as Outbound)
      if (isBuiltIn) {
        newOutbounds.push({ id: tag, type: 'Built-in', tag })
      } else if (groupTags.has(tag)) {
        const id = OutboundsIds[tag]
        if (id) {
          newOutbounds.push({ id, type: 'Built-in', tag })
        }
      } else {
        const proxy = proxyToSubMap.get(tag)
        if (proxy) {
          newOutbounds.push({ id: proxy.id, type: proxy.sub, tag })
        } else {
          newOutbounds.push({ id: tag, type: 'Built-in', tag })
        }
      }
    })

    const originalGroup = originalOutboundMap.get(outbound.tag)
    if (originalGroup) {
      outbound.icon = originalGroup.icon
      outbound.hidden = originalGroup.hidden
      outbound.include = originalGroup.include
      outbound.exclude = originalGroup.exclude

      const currentNonBuiltInIds = new Set(
        newOutbounds.filter((v) => v.type !== 'Built-in').map((v) => v.id),
      )

      subscriptionIds.forEach((id) => {
        const sub = subscriptionCache.get(id)
        if (sub) {
          const isTagMatching = createTextMatcher(originalGroup.include, originalGroup.exclude)
          const matchedProxies = sub.proxies.filter((proxy) => isTagMatching(proxy.tag))

          const isAllMatched =
            matchedProxies.length > 0 &&
            matchedProxies.every((proxy) => currentNonBuiltInIds.has(proxy.id))

          if (isAllMatched) {
            const matchedIds = new Set(matchedProxies.map((p) => p.id))
            newOutbounds = newOutbounds.filter(
              (v) => v.type === 'Built-in' || !matchedIds.has(v.id),
            )
            newOutbounds.push({ id: sub.id, type: 'Subscription', tag: sub.name })

            matchedIds.forEach((matchedId) => currentNonBuiltInIds.delete(matchedId))
          }
        }
      })
    }

    outbound.outbounds = newOutbounds

    if ('interrupt_exist_connections' in raw) {
      outbound.interrupt_exist_connections = raw.interrupt_exist_connections
    }
    if (Outbound.Urltest === raw.type) {
      if ('url' in raw) {
        outbound.url = raw.url
      }
      if ('interval' in raw) {
        outbound.interval = raw.interval
      }
      if ('tolerance' in raw) {
        outbound.tolerance = raw.tolerance
      }
    }
    return outbound
  })
}

const restoreRouteRuleset = (
  rulesets: Recordable[],
  RouteRuleSetIds: Recordable,
  OutboundsIds: Recordable,
): IRuleSet[] => {
  const rulesetsStore = useRulesetsStore()
  return rulesets.flatMap((raw) => {
    const ruleset = Defaults.DefaultRouteRuleset()
    ruleset.id = RouteRuleSetIds[raw.tag]
    ruleset.type = raw.type
    ruleset.tag = raw.tag

    if (raw.type === RulesetType.Inline) {
      if ('rules' in raw) {
        ruleset.rules = JSON.stringify(raw.rules, null, 2)
      }
    } else if (raw.type === RulesetType.Local) {
      if ('path' in raw) {
        const r = rulesetsStore.rulesets.find((v) => v.path === raw.path.replace('../', 'data/'))
        if (r) {
          ruleset.path = r.id
        } else {
          ruleset.path = raw.path
        }
      }
      if ('format' in raw) {
        ruleset.format = raw.format
      }
    } else if (raw.type === RulesetType.Remote) {
      if ('format' in raw) {
        ruleset.format = raw.format
      }
      if ('url' in raw) {
        ruleset.url = raw.url
      }
      if ('download_detour' in raw) {
        ruleset.download_detour = restoreReference(OutboundsIds, raw.download_detour)
      }
      if ('update_interval' in raw) {
        ruleset.update_interval = raw.update_interval
      }
    }
    return ruleset
  })
}

const restoreRouteRules = (
  rules: Recordable[],
  InboundsIds: Recordable,
  OutboundsIds: Recordable,
  RouteRuleSetIds: Recordable,
  DnsServersIds: Recordable,
): IRule[] => {
  const asArray = (value: unknown) => (Array.isArray(value) ? value : [value])
  const knownActions = new Set<string>([
    RuleAction.Route,
    RuleAction.Bypass,
    RuleAction.RouteOptions,
    RuleAction.Reject,
    RuleAction.HijackDNS,
    RuleAction.Sniff,
    RuleAction.Resolve,
  ])

  return rules.map((raw, i) => {
    const rule = Defaults.DefaultRouteRule()
    rule.id = 'rule-' + i
    const importedAction = raw.action || RuleAction.Route
    const isKnownAction = knownActions.has(importedAction)
    rule.action = isKnownAction ? importedAction : RuleAction.Inline

    const consumed = new Set<string>()
    if ('invert' in raw) {
      rule.invert = raw.invert
      consumed.add('invert')
    }
    if ('inbound' in raw) {
      rule.inbound = asArray(raw.inbound).map((tag) => InboundsIds[String(tag)] ?? String(tag))
      consumed.add('inbound')
    }
    if (raw.ip_version === 4 || raw.ip_version === 6) {
      rule.ip_version = raw.ip_version
      consumed.add('ip_version')
    }
    for (const key of routeArrayMatchKeys) {
      if (!(key in raw)) continue
      const values = asArray(raw[key])
      const routeFields = rule as unknown as Record<string, unknown>
      if (key === 'source_port' || key === 'port') {
        routeFields[key] = values.map(Number)
      } else {
        routeFields[key] = values.map(String)
      }
      consumed.add(key)
    }
    if ('source_ip_is_private' in raw) {
      rule.source_ip_is_private = Boolean(raw.source_ip_is_private)
      consumed.add('source_ip_is_private')
    }
    if ('ip_is_private' in raw) {
      rule.ip_is_private = Boolean(raw.ip_is_private)
      consumed.add('ip_is_private')
    }
    if ('clash_mode' in raw) {
      rule.clash_mode = String(raw.clash_mode)
      consumed.add('clash_mode')
    }
    if ('rule_set' in raw) {
      rule.rule_set = asArray(raw.rule_set).map(
        (tag) => RouteRuleSetIds[String(tag)] ?? String(tag),
      )
      consumed.add('rule_set')
    }

    const assignOptions = (keys: readonly (keyof IActionOptions)[]) => {
      const actionOptions = rule.action_options as unknown as Record<string, unknown>
      for (const key of keys) {
        if (key in raw) {
          if (key === 'network_type' || key === 'fallback_network_type') {
            actionOptions[key] = asArray(raw[key]).map(String)
          } else {
            actionOptions[key] = raw[key]
          }
          consumed.add(key)
        }
      }
    }

    if (isKnownAction) {
      consumed.add('action')
      if (rule.action === RuleAction.Route || rule.action === RuleAction.Bypass) {
        if ('outbound' in raw) {
          rule.action_options.outbound = restoreReference(OutboundsIds, raw.outbound)
          consumed.add('outbound')
        }
        assignOptions(routeOptionKeys)
      } else if (rule.action === RuleAction.RouteOptions) {
        assignOptions(routeOptionKeys)
      } else if (rule.action === RuleAction.Reject) {
        assignOptions(['method', 'no_drop'])
      } else if (rule.action === RuleAction.Sniff) {
        assignOptions(['sniffer', 'timeout'])
        rule.action_options.sniffer = asArray(raw.sniffer ?? [])
          .filter(Boolean)
          .map(String)
      } else if (rule.action === RuleAction.Resolve) {
        assignOptions([
          'strategy',
          'disable_cache',
          'disable_optimistic_cache',
          'rewrite_ttl',
          'timeout',
          'client_subnet',
        ])
        if ('server' in raw) {
          rule.action_options.server = restoreReference(DnsServersIds, raw.server)
          consumed.add('server')
        }
      }
    }

    const unmodeled = Object.fromEntries(Object.entries(raw).filter(([key]) => !consumed.has(key)))
    if (Object.keys(unmodeled).length > 0) {
      rule.raw = JSON.stringify(unmodeled, null, 2)
    }
    return rule
  })
}

const restoreDnsServers = (
  servers: Recordable[],
  DnsServersIds: Recordable,
  OutboundsIds: Recordable,
): IDNSServer[] => {
  return servers.flatMap((raw) => {
    if (!raw.type) return []
    const server = Defaults.DefaultDnsServer()
    server.id = DnsServersIds[raw.tag]
    server.tag = raw.tag
    server.type = raw.type
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
      ].includes(raw.type)
    ) {
      if ('detour' in raw) {
        server.detour = restoreReference(OutboundsIds, raw.detour)
      }
      if ('domain_resolver' in raw) {
        server.domain_resolver = restoreReference(DnsServersIds, raw.domain_resolver)
      }
      if (
        [
          DnsServer.Tcp,
          DnsServer.Udp,
          DnsServer.Tls,
          DnsServer.Quic,
          DnsServer.Https,
          DnsServer.H3,
        ].includes(raw.type)
      ) {
        if ('server' in raw) {
          server.server = raw.server
        }
        if ('server_port' in raw) {
          server.server_port = raw.server_port
        }
        if ([DnsServer.Https, DnsServer.H3].includes(raw.type)) {
          if ('path' in raw) {
            server.path = raw.path
          }
        }
      }
    } else if (DnsServer.Hosts === server.type) {
      if ('path' in raw) {
        server.hosts_path = raw.path
      }
      if ('predefined' in raw) {
        server.predefined = Object.entries<string[] | string>(raw.predefined).reduce(
          (p, [key, value]) => {
            p[key] = Array.isArray(value) ? value.join(',') : value
            return p
          },
          {} as Recordable,
        )
      }
    } else if (DnsServer.Dhcp === server.type) {
      if ('interface' in raw) {
        server.interface = raw.interface
      }
    } else if (DnsServer.FakeIP === server.type) {
      if ('inet4_range' in raw) {
        server.inet4_range = raw.inet4_range
      }
      if ('inet6_range' in raw) {
        server.inet6_range = raw.inet6_range
      }
    }
    return server
  })
}

const restoreDnsRules = (
  rules: Recordable[],
  InboundsIds: Recordable,
  RouteRuleSetIds: Recordable,
  DnsServersIds: Recordable,
): IDNSRule[] => {
  const asArray = (value: unknown) => (Array.isArray(value) ? value : [value])
  const knownActions = new Set<string>([
    RuleAction.Route,
    RuleAction.Evaluate,
    RuleAction.Respond,
    RuleAction.RouteOptions,
    RuleAction.Reject,
    RuleAction.Predefined,
  ])
  const arrayMatchKeys = [
    'network',
    'protocol',
    'preferred_by',
    'domain',
    'domain_suffix',
    'domain_keyword',
    'domain_regex',
    'source_ip_cidr',
    'source_port_range',
    'ip_cidr',
    'response_answer',
    'response_ns',
    'response_extra',
    'process_name',
    'process_path',
    'process_path_regex',
  ] as const
  const queryOptionKeys = [
    'disable_cache',
    'disable_optimistic_cache',
    'rewrite_ttl',
    'timeout',
    'client_subnet',
  ] as const

  return rules.map((raw: Recordable, i) => {
    const rule = Defaults.DefaultDnsRule()
    rule.id = 'rule-' + i
    const importedAction = raw.action || RuleAction.Route
    const isKnownAction = knownActions.has(importedAction)
    rule.action = isKnownAction ? importedAction : RuleAction.Inline
    const consumed = new Set<string>()

    if ('invert' in raw) {
      rule.invert = Boolean(raw.invert)
      consumed.add('invert')
    }
    if ('inbound' in raw) {
      rule.inbound = asArray(raw.inbound).map((tag) => restoreReference(InboundsIds, tag))
      consumed.add('inbound')
    }
    if ('clash_mode' in raw) {
      rule.clash_mode = String(raw.clash_mode)
      consumed.add('clash_mode')
    }
    if (raw.ip_version === 4 || raw.ip_version === 6) {
      rule.ip_version = raw.ip_version
      consumed.add('ip_version')
    }
    if ('query_type' in raw) {
      rule.query_type = asArray(raw.query_type).map(String)
      consumed.add('query_type')
    }
    if ('source_port' in raw) {
      rule.source_port = asArray(raw.source_port).map(Number)
      consumed.add('source_port')
    }
    for (const key of arrayMatchKeys) {
      if (!(key in raw)) continue
      ;(rule as unknown as Record<string, unknown>)[key] = asArray(raw[key]).map(String)
      consumed.add(key)
    }
    if ('rule_set' in raw) {
      rule.rule_set = asArray(raw.rule_set).map((tag) => restoreReference(RouteRuleSetIds, tag))
      consumed.add('rule_set')
    }
    for (const key of [
      'rule_set_ip_cidr_match_source',
      'source_ip_is_private',
      'match_response',
      'ip_accept_any',
      'ip_is_private',
    ] as const) {
      if (!(key in raw)) continue
      ;(rule as unknown as Record<string, unknown>)[key] = Boolean(raw[key])
      consumed.add(key)
    }
    if ('response_rcode' in raw) {
      rule.response_rcode = String(raw.response_rcode)
      consumed.add('response_rcode')
    }

    const assignOptions = (keys: readonly (keyof IDNSActionOptions)[]) => {
      const actionOptions = rule.action_options as unknown as Record<string, unknown>
      for (const key of keys) {
        if (!(key in raw)) continue
        actionOptions[key] = raw[key]
        consumed.add(key)
      }
    }

    if (isKnownAction) {
      consumed.add('action')
      if (rule.action === RuleAction.Route || rule.action === RuleAction.Evaluate) {
        if ('server' in raw) {
          rule.action_options.server = restoreReference(DnsServersIds, raw.server)
          consumed.add('server')
        }
        assignOptions(queryOptionKeys)
      } else if (rule.action === RuleAction.RouteOptions) {
        assignOptions(queryOptionKeys)
      } else if (rule.action === RuleAction.Reject) {
        assignOptions(['method', 'no_drop'])
      } else if (rule.action === RuleAction.Predefined) {
        assignOptions(['rcode'])
        for (const key of ['answer', 'ns', 'extra'] as const) {
          if (!(key in raw)) continue
          rule.action_options[key] = asArray(raw[key]).map(String)
          consumed.add(key)
        }
      }
    }

    const unmodeled = Object.fromEntries(Object.entries(raw).filter(([key]) => !consumed.has(key)))
    if (Object.keys(unmodeled).length > 0) {
      rule.raw = JSON.stringify(unmodeled, null, 2)
    }
    return rule
  })
}
