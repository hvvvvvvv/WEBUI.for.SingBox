import { describe, expect, it, vi } from 'vitest'

import {
  prepareConfigForQRS,
  QRSConfigPreparationError,
  type QRSConfigErrorCode,
  type QRSRuleSetResource,
} from '..'

const resource = (
  overrides: Partial<QRSRuleSetResource> & Pick<QRSRuleSetResource, 'id' | 'path'>,
): QRSRuleSetResource => ({
  type: 'Manual',
  format: 'source',
  url: '',
  ...overrides,
})

const localRuleSet = (tag: string, path: string, format: string) => ({
  type: 'local',
  tag,
  path,
  format,
})

const expectPreparationError = async (
  promise: Promise<unknown>,
  code: QRSConfigErrorCode,
  ruleSet: string,
) => {
  await expect(promise).rejects.toBeInstanceOf(QRSConfigPreparationError)
  await expect(promise).rejects.toMatchObject({ code, ruleSet })
}

const prepareSourceRules = async (rules: unknown): Promise<unknown> => {
  const prepared = await prepareConfigForQRS(
    {
      route: {
        rule_set: [localRuleSet('source', '../rulesets/source.json', 'source')],
      },
    },
    {
      resources: [resource({ id: 'source', path: 'data/rulesets/source.json' })],
      loadSource: async () => JSON.stringify({ version: 5, rules }),
    },
  )
  return (prepared.route.rule_set as Record<string, unknown>[])[0]!.rules
}

const neverMatchRule = {
  domain_regex: ['.*'],
  ip_cidr: ['0.0.0.0/0', '::/0'],
  invert: true,
}

describe('QRS profile configuration preparation', () => {
  it('converts any matching HTTP binary resource to remote with its stored URL', async () => {
    const local = localRuleSet('private-binary', '../rulesets/private.srs', 'binary')
    const existingRemote = {
      type: 'remote',
      tag: 'existing',
      format: 'source',
      url: 'https://example.com/existing.json',
    }
    const config = {
      log: { level: 'info' },
      route: {
        final: 'proxy',
        rule_set: [local, existingRemote],
      },
    }
    const loadSource = vi.fn<() => Promise<string>>()

    const prepared = await prepareConfigForQRS(config, {
      resources: [
        resource({
          id: 'private-download',
          type: 'Http',
          format: 'binary',
          path: 'data/rulesets/private.srs',
          url: 'https://private.example/rules/latest.srs',
        }),
      ],
      loadSource,
    })

    expect(prepared).toEqual({
      log: { level: 'info' },
      route: {
        final: 'proxy',
        rule_set: [
          {
            type: 'remote',
            tag: 'private-binary',
            format: 'binary',
            url: 'https://private.example/rules/latest.srs',
          },
          existingRemote,
        ],
      },
    })
    expect(config.route.rule_set[0]).toBe(local)
    expect(config.route.rule_set[0]!.type).toBe('local')
    expect(loadSource).not.toHaveBeenCalled()
  })

  it('converts Manual and HTTP source files to inline rules and reads duplicates once', async () => {
    const preservedInline = {
      type: 'inline',
      tag: 'already-inline',
      rules: [{ domain_suffix: ['example.org'] }],
    }
    const config = {
      experimental: { cache_file: { enabled: true } },
      route: {
        final: 'direct',
        rule_set: [
          localRuleSet('manual', '../rulesets/manual.json', 'source'),
          preservedInline,
          localRuleSet('http', '../rulesets/http.json', 'source'),
          localRuleSet('manual-copy', '../rulesets/manual.json', 'source'),
        ],
      },
    }
    const contents: Record<string, string> = {
      manual: JSON.stringify({
        version: 5,
        rules: [
          {
            type: 'logical',
            mode: 'and',
            rules: [{ domain: ['example.com'] }, { ip_cidr: ['192.0.2.0/24'] }],
          },
        ],
        ignored: true,
      }),
      http: JSON.stringify({ version: 1, rules: { domain_keyword: ['single'] } }),
    }
    const loadSource = vi.fn(async (id: string) => contents[id]!)

    const prepared = await prepareConfigForQRS(config, {
      resources: [
        resource({ id: 'manual', path: 'data/rulesets/manual.json' }),
        resource({
          id: 'http',
          type: 'Http',
          path: 'data/rulesets/http.json',
          url: 'https://example.com/http.json',
        }),
      ],
      loadSource,
    })
    const ruleSets = prepared.route.rule_set as Record<string, unknown>[]

    expect(ruleSets.map((item) => item.type)).toEqual(['inline', 'inline', 'inline', 'inline'])
    expect(ruleSets[0]).toEqual({
      type: 'inline',
      tag: 'manual',
      rules: [
        {
          type: 'logical',
          mode: 'and',
          rules: [{ domain: ['example.com'] }, { ip_cidr: ['192.0.2.0/24'] }],
        },
      ],
    })
    expect(ruleSets[1]).toBe(preservedInline)
    expect(ruleSets[2]).toEqual({
      type: 'inline',
      tag: 'http',
      rules: { domain_keyword: ['single'] },
    })
    expect(ruleSets[3]!.rules).toEqual(ruleSets[0]!.rules)
    expect(loadSource).toHaveBeenCalledTimes(2)
    expect(loadSource).toHaveBeenCalledWith('manual')
    expect(loadSource).toHaveBeenCalledWith('http')
    expect(JSON.stringify(prepared)).not.toContain('"type":"local"')
    expect(prepared.experimental).toBe(config.experimental)
  })

  it('replaces an empty top-level rule list with an exact never-match rule', async () => {
    expect(await prepareSourceRules([])).toEqual([neverMatchRule])
  })

  it('replaces every empty default rule with an independent never-match rule', async () => {
    const rules = (await prepareSourceRules([
      {},
      { invert: true },
      { domain: [], network_is_expensive: false, future_match: {} },
    ])) as Record<string, unknown>[]

    expect(rules).toEqual([neverMatchRule, neverMatchRule, neverMatchRule])
    expect(rules[0]).not.toBe(rules[1])
    expect(rules[1]).not.toBe(rules[2])
    expect(await prepareSourceRules({})).toEqual(neverMatchRule)
  })

  it('normalizes empty logical rules and recursively normalizes their children', async () => {
    const rules = await prepareSourceRules([
      { type: 'logical', mode: 'and', rules: [] },
      { type: 'logical', mode: 'or' },
      {
        type: 'logical',
        mode: 'or',
        invert: true,
        rules: [
          {},
          { domain: ['example.com'] },
          {
            type: 'logical',
            mode: 'and',
            rules: [{ invert: true }, { ip_cidr: ['192.0.2.0/24'] }],
          },
        ],
      },
    ])

    expect(rules).toEqual([
      neverMatchRule,
      neverMatchRule,
      {
        type: 'logical',
        mode: 'or',
        invert: true,
        rules: [
          neverMatchRule,
          { domain: ['example.com'] },
          {
            type: 'logical',
            mode: 'and',
            rules: [neverMatchRule, { ip_cidr: ['192.0.2.0/24'] }],
          },
        ],
      },
    ])
  })

  it('preserves every non-empty matching value, including future fields', async () => {
    const nonEmptyRules = [
      { domain: ['example.com'] },
      { ip_cidr: ['2001:db8::/32'] },
      { port: 0 },
      { process_name: 'sing-box' },
      { network_is_expensive: true },
      { future_match: { enabled: true } },
    ]

    expect(await prepareSourceRules(nonEmptyRules)).toEqual(nonEmptyRules)
  })

  it('does not normalize an existing inline rule set', async () => {
    const existingInline = { type: 'inline', tag: 'existing', rules: [] }
    const config = { route: { rule_set: [existingInline] } }
    const prepared = await prepareConfigForQRS(config, {
      resources: [],
      loadSource: vi.fn(),
    })

    expect(prepared.route.rule_set[0]).toBe(existingInline)
    expect(prepared.route.rule_set[0]!.rules).toEqual([])
  })

  it('returns the original configuration when no standard rule-set array exists', async () => {
    const config = { route: { final: 'direct' } }
    const prepared = await prepareConfigForQRS(config, {
      resources: [],
      loadSource: vi.fn(),
    })
    expect(prepared).toBe(config)
  })

  it.each([
    {
      name: 'missing resource',
      code: 'resourceNotFound' as const,
      rule: localRuleSet('missing', '../rulesets/missing.json', 'source'),
      resources: [],
    },
    {
      name: 'resource format mismatch',
      code: 'formatMismatch' as const,
      rule: localRuleSet('mismatch', '../rulesets/mismatch.srs', 'source'),
      resources: [
        resource({
          id: 'mismatch',
          type: 'Http',
          format: 'binary',
          path: 'data/rulesets/mismatch.srs',
          url: 'https://example.com/mismatch.srs',
        }),
      ],
    },
    {
      name: 'non-HTTP binary resource',
      code: 'binaryNotHttp' as const,
      rule: localRuleSet('manual-binary', '../rulesets/manual.srs', 'binary'),
      resources: [
        resource({ id: 'manual-binary', format: 'binary', path: 'data/rulesets/manual.srs' }),
      ],
    },
    {
      name: 'empty binary URL',
      code: 'binaryUrlEmpty' as const,
      rule: localRuleSet('empty-url', '../rulesets/empty.srs', 'binary'),
      resources: [
        resource({
          id: 'empty-url',
          type: 'Http',
          format: 'binary',
          path: 'data/rulesets/empty.srs',
          url: '   ',
        }),
      ],
    },
    {
      name: 'unknown local format',
      code: 'unsupportedFormat' as const,
      rule: localRuleSet('unknown-format', '../rulesets/unknown.yaml', 'yaml'),
      resources: [],
    },
  ])(
    'rejects $name without returning a partially converted config',
    async ({ code, rule, resources }) => {
      const promise = prepareConfigForQRS(
        { route: { rule_set: [rule] } },
        { resources, loadSource: vi.fn() },
      )
      await expectPreparationError(promise, code, rule.tag)
    },
  )

  it.each([
    ['sourceLoadFailed', () => Promise.reject(new Error('disk unavailable'))],
    ['sourceInvalidJson', () => Promise.resolve('{invalid')],
    ['sourceRulesMissing', () => Promise.resolve('{"version":5}')],
    ['sourceRulesInvalid', () => Promise.resolve('{"version":5,"rules":null}')],
  ] as const)('rejects invalid source content with %s', async (code, loadSource) => {
    const promise = prepareConfigForQRS(
      {
        route: {
          rule_set: [localRuleSet('broken-source', '../rulesets/broken.json', 'source')],
        },
      },
      {
        resources: [resource({ id: 'broken', path: 'data/rulesets/broken.json' })],
        loadSource,
      },
    )
    await expectPreparationError(promise, code, 'broken-source')
  })
})
