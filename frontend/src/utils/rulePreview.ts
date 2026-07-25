import { RuleAction, RuleActionReject } from '@/enums/kernel'

interface ReferenceOption {
  label: string
  value: string
}

interface RouteRulePreviewOptions {
  inboundOptions: ReferenceOption[]
  outboundOptions: ReferenceOption[]
  serverOptions: ReferenceOption[]
  ruleSetOptions: ReferenceOption[]
}

interface DnsRulePreviewOptions {
  inboundOptions: ReferenceOption[]
  serverOptions: ReferenceOption[]
  ruleSetOptions: ReferenceOption[]
}

type PreviewValue = string | number | boolean | (string | number | boolean)[]

const appendField = (
  fields: string[],
  key: string,
  value: PreviewValue | undefined,
  includeZero = false,
) => {
  if (Array.isArray(value)) {
    const values = value.filter((entry) => typeof entry !== 'string' || entry.trim().length > 0)
    if (values.length) fields.push(`${key}=${values.join('|')}`)
    return
  }
  if (typeof value === 'boolean') {
    if (value) fields.push(`${key}=true`)
    return
  }
  if (typeof value === 'number') {
    if (includeZero || value !== 0) fields.push(`${key}=${value}`)
    return
  }
  if (typeof value === 'string' && value.trim()) fields.push(`${key}=${value}`)
}

const resolveReferences = (ids: string[], options: ReferenceOption[]) =>
  ids.map((id) => options.find((option) => option.value === id)?.label || id)

const appendRouteOptions = (fields: string[], options: IActionOptions) => {
  appendField(fields, 'override_address', options.override_address)
  appendField(fields, 'override_port', options.override_port)
  appendField(fields, 'network_strategy', options.network_strategy)
  appendField(fields, 'network_type', options.network_type)
  appendField(fields, 'fallback_network_type', options.fallback_network_type)
  appendField(fields, 'fallback_delay', options.fallback_delay)
  appendField(fields, 'udp_disable_domain_unmapping', options.udp_disable_domain_unmapping)
  appendField(fields, 'udp_connect', options.udp_connect)
  appendField(fields, 'udp_timeout', options.udp_timeout)
  appendField(fields, 'tls_fragment', options.tls_fragment)
  appendField(fields, 'tls_fragment_fallback_delay', options.tls_fragment_fallback_delay)
  appendField(fields, 'tls_record_fragment', options.tls_record_fragment)
  appendField(fields, 'tls_spoof', options.tls_spoof)
  appendField(fields, 'tls_spoof_method', options.tls_spoof_method)
}

const appendRouteAction = (fields: string[], rule: IRule, options: RouteRulePreviewOptions) => {
  const actionOptions = rule.action_options
  appendField(fields, 'action', rule.action)

  if (rule.action === RuleAction.Route || rule.action === RuleAction.Bypass) {
    appendField(
      fields,
      'outbound',
      resolveReferences([actionOptions.outbound], options.outboundOptions),
    )
    appendRouteOptions(fields, actionOptions)
  } else if (rule.action === RuleAction.RouteOptions) {
    appendRouteOptions(fields, actionOptions)
  } else if (rule.action === RuleAction.Reject) {
    appendField(fields, 'method', actionOptions.method || RuleActionReject.Default)
    appendField(fields, 'no_drop', actionOptions.no_drop)
  } else if (rule.action === RuleAction.Sniff) {
    appendField(fields, 'sniffer', actionOptions.sniffer)
    appendField(fields, 'timeout', actionOptions.timeout)
  } else if (rule.action === RuleAction.Resolve) {
    appendField(fields, 'server', resolveReferences([actionOptions.server], options.serverOptions))
    appendField(fields, 'strategy', actionOptions.strategy)
    appendField(fields, 'disable_cache', actionOptions.disable_cache)
    appendField(fields, 'disable_optimistic_cache', actionOptions.disable_optimistic_cache)
    appendField(fields, 'rewrite_ttl', actionOptions.rewrite_ttl, true)
    appendField(fields, 'timeout', actionOptions.timeout)
    appendField(fields, 'client_subnet', actionOptions.client_subnet)
  }
}

export const renderRouteRulePreview = (rule: IRule, options: RouteRulePreviewOptions) => {
  const matches: string[] = []
  appendField(matches, 'inbound', resolveReferences(rule.inbound, options.inboundOptions))
  appendField(matches, 'ip_version', rule.ip_version)
  appendField(matches, 'network', rule.network)
  appendField(matches, 'preferred_by', rule.preferred_by)
  appendField(matches, 'protocol', rule.protocol)
  appendField(matches, 'domain', rule.domain)
  appendField(matches, 'domain_suffix', rule.domain_suffix)
  appendField(matches, 'domain_keyword', rule.domain_keyword)
  appendField(matches, 'domain_regex', rule.domain_regex)
  appendField(matches, 'ip_cidr', rule.ip_cidr)
  appendField(matches, 'source_ip_cidr', rule.source_ip_cidr)
  appendField(matches, 'source_ip_is_private', rule.source_ip_is_private)
  appendField(matches, 'ip_is_private', rule.ip_is_private)
  appendField(matches, 'source_port', rule.source_port)
  appendField(matches, 'source_port_range', rule.source_port_range)
  appendField(matches, 'port', rule.port)
  appendField(matches, 'port_range', rule.port_range)
  appendField(matches, 'process_name', rule.process_name)
  appendField(matches, 'process_path', rule.process_path)
  appendField(matches, 'process_path_regex', rule.process_path_regex)
  appendField(matches, 'clash_mode', rule.clash_mode)
  appendField(matches, 'rule_set', resolveReferences(rule.rule_set, options.ruleSetOptions))
  appendField(matches, 'invert', rule.invert)
  appendField(matches, 'raw', rule.raw)

  const action: string[] = []
  appendRouteAction(action, rule, options)
  return `${matches.join(', ') || '*'} → ${action.join(', ')}`
}

const appendDnsQueryOptions = (fields: string[], options: IDNSActionOptions) => {
  appendField(fields, 'disable_cache', options.disable_cache)
  appendField(fields, 'disable_optimistic_cache', options.disable_optimistic_cache)
  appendField(fields, 'rewrite_ttl', options.rewrite_ttl, true)
  appendField(fields, 'timeout', options.timeout)
  appendField(fields, 'client_subnet', options.client_subnet)
}

const appendDnsAction = (fields: string[], rule: IDNSRule, options: DnsRulePreviewOptions) => {
  const actionOptions = rule.action_options
  appendField(fields, 'action', rule.action)

  if (rule.action === RuleAction.Route || rule.action === RuleAction.Evaluate) {
    appendField(fields, 'server', resolveReferences([actionOptions.server], options.serverOptions))
    appendDnsQueryOptions(fields, actionOptions)
  } else if (rule.action === RuleAction.RouteOptions) {
    appendDnsQueryOptions(fields, actionOptions)
  } else if (rule.action === RuleAction.Reject) {
    appendField(fields, 'method', actionOptions.method || RuleActionReject.Default)
    appendField(fields, 'no_drop', actionOptions.no_drop)
  } else if (rule.action === RuleAction.Predefined) {
    appendField(fields, 'rcode', actionOptions.rcode || 'NOERROR')
    appendField(fields, 'answer', actionOptions.answer)
    appendField(fields, 'ns', actionOptions.ns)
    appendField(fields, 'extra', actionOptions.extra)
  }
}

export const renderDnsRulePreview = (rule: IDNSRule, options: DnsRulePreviewOptions) => {
  const matches: string[] = []
  appendField(matches, 'inbound', resolveReferences(rule.inbound, options.inboundOptions))
  appendField(matches, 'clash_mode', rule.clash_mode)
  appendField(matches, 'ip_version', rule.ip_version)
  appendField(matches, 'query_type', rule.query_type)
  appendField(matches, 'network', rule.network)
  appendField(matches, 'protocol', rule.protocol)
  appendField(matches, 'preferred_by', rule.preferred_by)
  appendField(matches, 'domain', rule.domain)
  appendField(matches, 'domain_suffix', rule.domain_suffix)
  appendField(matches, 'domain_keyword', rule.domain_keyword)
  appendField(matches, 'domain_regex', rule.domain_regex)
  appendField(matches, 'rule_set', resolveReferences(rule.rule_set, options.ruleSetOptions))
  appendField(matches, 'rule_set_ip_cidr_match_source', rule.rule_set_ip_cidr_match_source)
  appendField(matches, 'match_response', rule.match_response)
  appendField(matches, 'ip_accept_any', rule.ip_accept_any)
  appendField(matches, 'ip_cidr', rule.ip_cidr)
  appendField(matches, 'ip_is_private', rule.ip_is_private)
  appendField(matches, 'response_rcode', rule.response_rcode)
  appendField(matches, 'response_answer', rule.response_answer)
  appendField(matches, 'response_ns', rule.response_ns)
  appendField(matches, 'response_extra', rule.response_extra)
  appendField(matches, 'process_name', rule.process_name)
  appendField(matches, 'process_path', rule.process_path)
  appendField(matches, 'process_path_regex', rule.process_path_regex)
  appendField(matches, 'invert', rule.invert)
  appendField(matches, 'raw', rule.raw)

  const action: string[] = []
  appendDnsAction(action, rule, options)
  return `${matches.join(', ') || '*'} → ${action.join(', ')}`
}
