import { describe, expect, it, vi } from 'vitest'

import { prepareConfigForQRS, type QRSRuleSetResource } from '@/utils/qrs'

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
      outbounds: [{ type: 'selector', tag: 'proxy', outbounds: ['direct'] }],
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
      outbounds: config.outbounds,
      http_clients: [{ tag: 'webui-rule-set-default', detour: 'proxy' }],
      route: {
        final: 'proxy',
        default_http_client: 'webui-rule-set-default',
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
    expect(config).not.toHaveProperty('http_clients')
    expect(config.route).not.toHaveProperty('default_http_client')
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
    expect(prepared).not.toHaveProperty('http_clients')
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
})
