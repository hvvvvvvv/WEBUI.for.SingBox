export const routeMatchFields = [
  'rule_set',
  'inbound',
  'clash_mode',
  'network',
  'protocol',
  'domain',
  'domain_suffix',
  'domain_keyword',
  'domain_regex',
  'ip_cidr',
  'ip_is_private',
  'source_ip_cidr',
  'source_ip_is_private',
  'source_port',
  'source_port_range',
  'port',
  'port_range',
  'process_name',
  'process_path',
  'process_path_regex',
  'ip_version',
  'preferred_by',
  'raw',
] as const satisfies readonly (keyof IRule)[]

export type RouteMatchField = (typeof routeMatchFields)[number]

export const routeMatchLabel = (field: RouteMatchField) => {
  if (field === 'raw') return 'kernel.rules.type.inline'
  if (['ip_version', 'preferred_by', 'source_ip_is_private'].includes(field))
    return `kernel.route.rules.fields.${field}`
  return `kernel.rules.type.${field}`
}

export const hasRouteMatch = (rule: IRule, field: RouteMatchField) => {
  const value = rule[field]
  return Array.isArray(value)
    ? value.length > 0
    : typeof value === 'string'
      ? !!value.trim()
      : !!value
}

export const clearRouteMatch = (rule: IRule, field: RouteMatchField) => {
  const value = rule[field]
  Object.assign(rule, {
    [field]: Array.isArray(value)
      ? []
      : typeof value === 'boolean'
        ? false
        : typeof value === 'number'
          ? 0
          : '',
  })
}
