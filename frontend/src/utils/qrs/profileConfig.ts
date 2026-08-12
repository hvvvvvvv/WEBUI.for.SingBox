type JsonRecord = Record<string, unknown>

export interface QRSRuleSetResource {
  id: string
  type: 'Http' | 'Manual'
  format: 'source' | 'binary'
  path: string
  url: string
}

export interface PrepareQRSConfigContext {
  resources: readonly QRSRuleSetResource[]
  loadSource: (resourceId: string) => Promise<string>
}

export type QRSConfigErrorCode =
  | 'resourceNotFound'
  | 'formatMismatch'
  | 'binaryNotHttp'
  | 'binaryUrlEmpty'
  | 'sourceLoadFailed'
  | 'sourceInvalidJson'
  | 'sourceRulesMissing'
  | 'sourceRulesInvalid'
  | 'unsupportedFormat'
  | 'localRemaining'

export class QRSConfigPreparationError extends Error {
  constructor(
    readonly code: QRSConfigErrorCode,
    readonly ruleSet: string,
    readonly detail = '',
  ) {
    super(`${code}: ${ruleSet}${detail ? ` (${detail})` : ''}`)
    this.name = 'QRSConfigPreparationError'
  }
}

const isRecord = (value: unknown): value is JsonRecord =>
  typeof value === 'object' && value !== null && !Array.isArray(value)

const HEADLESS_RULE_STRUCTURE_FIELDS = new Set(['type', 'mode', 'rules', 'invert'])

const createNeverMatchRule = (): JsonRecord => ({
  domain_regex: ['.*'],
  ip_cidr: ['0.0.0.0/0', '::/0'],
  invert: true,
})

const hasEffectiveMatchValue = (value: unknown): boolean => {
  if (value === null || value === undefined || value === false) return false
  if (typeof value === 'string') return value.length > 0
  if (Array.isArray(value)) return value.length > 0
  if (isRecord(value)) return Object.keys(value).length > 0
  return typeof value === 'number' || typeof value === 'bigint' || value === true
}

const normalizeHeadlessRule = (rule: JsonRecord): JsonRecord => {
  if (rule.type === 'logical') {
    if (Array.isArray(rule.rules)) {
      if (rule.rules.length === 0) return createNeverMatchRule()
      return {
        ...rule,
        rules: rule.rules.map((child) => (isRecord(child) ? normalizeHeadlessRule(child) : child)),
      }
    }
    if (isRecord(rule.rules)) {
      return { ...rule, rules: normalizeHeadlessRule(rule.rules) }
    }
    return createNeverMatchRule()
  }

  const hasMatchCondition = Object.entries(rule).some(
    ([key, value]) => !HEADLESS_RULE_STRUCTURE_FIELDS.has(key) && hasEffectiveMatchValue(value),
  )
  return hasMatchCondition ? rule : createNeverMatchRule()
}

const normalizeHeadlessRules = (rules: unknown[] | JsonRecord): unknown[] | JsonRecord => {
  if (Array.isArray(rules)) {
    if (rules.length === 0) return [createNeverMatchRule()]
    return rules.map((rule) => (isRecord(rule) ? normalizeHeadlessRule(rule) : rule))
  }
  return normalizeHeadlessRule(rules)
}

const generatedResourcePath = (path: string) => path.replace('data/', '../')

const ruleSetLabel = (ruleSet: JsonRecord, index: number): string => {
  if (typeof ruleSet.tag === 'string' && ruleSet.tag) return ruleSet.tag
  if (Array.isArray(ruleSet.tag) && ruleSet.tag.length > 0) return ruleSet.tag.join(', ')
  if (typeof ruleSet.path === 'string' && ruleSet.path) return ruleSet.path
  return `#${index + 1}`
}

const describeError = (error: unknown): string => {
  if (error instanceof Error && error.message) return error.message
  return String(error)
}

const parseSourceRules = (content: string, label: string): unknown[] | JsonRecord => {
  let parsed: unknown
  try {
    parsed = JSON.parse(content)
  } catch (error) {
    throw new QRSConfigPreparationError('sourceInvalidJson', label, describeError(error))
  }

  if (!isRecord(parsed) || !Object.hasOwn(parsed, 'rules')) {
    throw new QRSConfigPreparationError('sourceRulesMissing', label)
  }
  if (!Array.isArray(parsed.rules) && !isRecord(parsed.rules)) {
    throw new QRSConfigPreparationError('sourceRulesInvalid', label)
  }
  return normalizeHeadlessRules(parsed.rules)
}

export const prepareConfigForQRS = async (
  config: Recordable,
  context: PrepareQRSConfigContext,
): Promise<Recordable> => {
  if (!isRecord(config.route) || !Array.isArray(config.route.rule_set)) return config

  const resourcesByPath = new Map(
    context.resources
      .filter((resource) => resource.path)
      .map((resource) => [generatedResourcePath(resource.path), resource] as const),
  )
  const sourceRequests = new Map<string, Promise<string>>()
  const loadSourceOnce = (resourceId: string) => {
    let request = sourceRequests.get(resourceId)
    if (!request) {
      request = context.loadSource(resourceId)
      sourceRequests.set(resourceId, request)
    }
    return request
  }

  const ruleSets = await Promise.all(
    config.route.rule_set.map(async (value, index): Promise<unknown> => {
      if (!isRecord(value) || value.type !== 'local') return value

      const label = ruleSetLabel(value, index)
      if (value.format !== 'source' && value.format !== 'binary') {
        throw new QRSConfigPreparationError('unsupportedFormat', label)
      }
      const resource = typeof value.path === 'string' ? resourcesByPath.get(value.path) : undefined
      if (!resource) {
        throw new QRSConfigPreparationError('resourceNotFound', label)
      }
      if (value.format !== resource.format) {
        throw new QRSConfigPreparationError('formatMismatch', label, resource.id)
      }

      if (value.format === 'binary') {
        if (resource.type !== 'Http') {
          throw new QRSConfigPreparationError('binaryNotHttp', label, resource.id)
        }
        if (!resource.url.trim()) {
          throw new QRSConfigPreparationError('binaryUrlEmpty', label, resource.id)
        }
        return {
          type: 'remote',
          tag: value.tag,
          format: 'binary',
          url: resource.url,
        }
      }

      if (value.format === 'source') {
        let content: string
        try {
          content = await loadSourceOnce(resource.id)
        } catch (error) {
          throw new QRSConfigPreparationError('sourceLoadFailed', label, describeError(error))
        }
        return {
          type: 'inline',
          tag: value.tag,
          rules: parseSourceRules(content, label),
        }
      }

      throw new QRSConfigPreparationError('localRemaining', label)
    }),
  )

  const localIndex = ruleSets.findIndex((value) => isRecord(value) && value.type === 'local')
  if (localIndex !== -1) {
    throw new QRSConfigPreparationError(
      'localRemaining',
      ruleSetLabel(ruleSets[localIndex] as JsonRecord, localIndex),
    )
  }

  return {
    ...config,
    route: {
      ...config.route,
      rule_set: ruleSets,
    },
  }
}
