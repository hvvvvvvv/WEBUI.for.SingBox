type JsonRecord = Record<string, unknown>

export class HTTPClientPreparationError extends Error {
  constructor(
    readonly path: string,
    detail: string,
  ) {
    super(`${path}: ${detail}`)
    this.name = 'HTTPClientPreparationError'
  }
}

const isRecord = (value: unknown): value is JsonRecord =>
  typeof value === 'object' && value !== null && !Array.isArray(value)

// sing-box 1.14 option.DialerOptions JSON fields. Do not copy direct-specific
// fields (such as override_address) into HTTP clients.
const DIAL_FIELDS = [
  'bind_interface',
  'inet4_bind_address',
  'inet6_bind_address',
  'bind_address_no_port',
  'protect_path',
  'routing_mark',
  'reuse_addr',
  'netns',
  'connect_timeout',
  'tcp_fast_open',
  'tcp_multi_path',
  'disable_tcp_keep_alive',
  'tcp_keep_alive',
  'tcp_keep_alive_interval',
  'udp_fragment',
  'domain_resolver',
  'network_strategy',
  'network_type',
  'fallback_network_type',
  'fallback_delay',
  'domain_strategy',
] as const

const optionalString = (value: unknown, path: string): string => {
  if (value === undefined || value === null) return ''
  if (typeof value !== 'string') {
    throw new HTTPClientPreparationError(path, 'expected a string')
  }
  return value
}

const recordArray = (value: unknown, path: string): JsonRecord[] => {
  if (value === undefined || value === null) return []
  if (!Array.isArray(value)) {
    throw new HTTPClientPreparationError(path, 'expected an array')
  }
  return value.map((item, index) => {
    if (!isRecord(item)) {
      throw new HTTPClientPreparationError(`${path}[${index}]`, 'expected an object')
    }
    return item
  })
}

const copyJSONValue = (value: unknown): unknown => {
  if (Array.isArray(value)) return value.map(copyJSONValue)
  if (isRecord(value)) {
    return Object.fromEntries(
      Object.entries(value).map(([key, child]) => [key, copyJSONValue(child)]),
    )
  }
  return value
}

const resolveOutbound = (config: JsonRecord, tag: string, path: string): JsonRecord | undefined => {
  const outbounds = recordArray(config.outbounds, 'outbounds')
  const endpoints = recordArray(config.endpoints, 'endpoints')
  const find = (items: JsonRecord[], prefix: string) =>
    items.findLast(
      (item, index) =>
        (optionalString(item.tag, `${prefix}[${index}].tag`) || String(index)) === tag,
    )
  const outbound = find(outbounds, 'outbounds')
  if (!outbound) {
    if (
      tag === 'direct' &&
      outbounds.length === 0 &&
      !optionalString(isRecord(config.route) ? config.route.final : undefined, 'route.final')
    )
      return { type: 'direct' }
    if (find(endpoints, 'endpoints')) return undefined
    throw new HTTPClientPreparationError(path, `outbound or endpoint "${tag}" was not found`)
  }
  return outbound
}

const outboundClient = (config: JsonRecord, tag: string, path: string): JsonRecord => {
  const outbound = resolveOutbound(config, tag, path)
  if (!outbound) return { detour: tag }
  const type = optionalString(outbound.type, `${path}.type`)
  if (type !== 'direct') return { detour: tag }

  const client: JsonRecord = { engine: 'go' }
  for (const field of DIAL_FIELDS) {
    if (Object.hasOwn(outbound, field)) client[field] = copyJSONValue(outbound[field])
  }
  return client
}

const defaultOutboundClient = (config: JsonRecord, route: JsonRecord): JsonRecord => {
  const final = optionalString(route.final, 'route.final')
  if (final) return outboundClient(config, final, 'route.final')
  const outbounds = recordArray(config.outbounds, 'outbounds')
  if (outbounds.length === 0) return { engine: 'go' }
  const first = outbounds[0]!
  const tag = optionalString(first.tag, 'outbounds[0].tag') || '0'
  return outboundClient(config, tag, 'outbounds[0]')
}

const uniqueClientTag = (clients: readonly JsonRecord[]): string => {
  const tags = new Set(
    clients.map((client, index) => optionalString(client.tag, `http_clients[${index}].tag`)),
  )
  const base = 'webui-rule-set-default'
  let tag = base
  let suffix = 2
  while (tags.has(tag)) tag = `${base}-${suffix++}`
  return tag
}

/** Complete the remote rule-set HTTP clients after QRS resource conversion. */
export const prepareRuleSetHTTPClients = (config: JsonRecord): JsonRecord => {
  if (config.route === undefined || config.route === null) return config
  if (!isRecord(config.route)) {
    throw new HTTPClientPreparationError('route', 'expected an object')
  }
  const route = config.route
  if (route.rule_set === undefined || route.rule_set === null) return config
  const rules = recordArray(route.rule_set, 'route.rule_set')
  const remoteRules = rules.filter(
    (rule, index) => optionalString(rule.type, `route.rule_set[${index}].type`) === 'remote',
  )
  if (remoteRules.length === 0) return config

  optionalString(route.final, 'route.final')
  for (const key of ['outbounds', 'endpoints']) {
    recordArray(config[key], key).forEach((item, index) => {
      optionalString(item.tag, `${key}[${index}].tag`)
      optionalString(item.type, `${key}[${index}].type`)
    })
  }

  let clients = recordArray(config.http_clients, 'http_clients')
  clients.forEach((client, index) => optionalString(client.tag, `http_clients[${index}].tag`))
  const validateDetour = (client: JsonRecord, path: string) => {
    const detour = optionalString(client.detour, `${path}.detour`)
    if (detour) resolveOutbound(config, detour, `${path}.detour`)
  }
  const findClient = (tag: string, path: string) => {
    const index = clients.findLastIndex(
      (item, index) => optionalString(item.tag, `http_clients[${index}].tag`) === tag,
    )
    if (index === -1)
      throw new HTTPClientPreparationError(path, `HTTP client "${tag}" was not found`)
    validateDetour(clients[index]!, `http_clients[${index}]`)
  }
  const validateClient = (value: unknown, path: string) => {
    if (typeof value === 'string') {
      if (value) findClient(value, path)
    } else if (isRecord(value)) {
      validateDetour(value, path)
    } else if (value !== undefined && value !== null) {
      throw new HTTPClientPreparationError(path, 'expected an HTTP client tag or object')
    }
  }
  const defaultTag = optionalString(route.default_http_client, 'route.default_http_client')
  if (defaultTag) {
    findClient(defaultTag, 'route.default_http_client')
  } else if (clients.length > 0) {
    const firstTag = optionalString(clients[0]!.tag, 'http_clients[0].tag')
    if (firstTag) findClient(firstTag, 'http_clients[0].tag')
  }
  let needsDefault = false
  const ruleSets = rules.map((rule, index): unknown => {
    if (rule.type !== 'remote') return rule
    const path = `route.rule_set[${index}]`
    const explicit =
      rule.http_client !== undefined && rule.http_client !== null && rule.http_client !== ''
    validateClient(rule.http_client, `${path}.http_client`)
    const { download_detour: legacy, ...modern } = rule
    if (explicit) return Object.hasOwn(rule, 'download_detour') ? modern : rule

    delete modern.http_client
    const tag = optionalString(legacy, `${path}.download_detour`)
    if (tag)
      return { ...modern, http_client: outboundClient(config, tag, `${path}.download_detour`) }
    needsDefault = true
    return Object.hasOwn(rule, 'download_detour') || Object.hasOwn(rule, 'http_client')
      ? modern
      : rule
  })

  let resolvedDefault = ''
  let clientsChanged = false
  if (needsDefault) {
    resolvedDefault = defaultTag
    if (resolvedDefault) {
      findClient(resolvedDefault, 'route.default_http_client')
    } else if (clients.length > 0) {
      resolvedDefault = optionalString(clients[0]!.tag, 'http_clients[0].tag')
      if (!resolvedDefault) {
        resolvedDefault = uniqueClientTag(clients)
        clients = [{ ...clients[0], tag: resolvedDefault }, ...clients.slice(1)]
        clientsChanged = true
      }
      findClient(resolvedDefault, 'route.default_http_client')
    } else {
      resolvedDefault = uniqueClientTag(clients)
      clients = [{ ...defaultOutboundClient(config, route), tag: resolvedDefault }]
      clientsChanged = true
    }
  }

  return {
    ...config,
    ...(clientsChanged ? { http_clients: clients } : {}),
    route: {
      ...route,
      rule_set: ruleSets,
      ...(needsDefault ? { default_http_client: resolvedDefault } : {}),
    },
  }
}
