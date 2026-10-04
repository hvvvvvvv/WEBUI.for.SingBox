import { describe, expect, it } from 'vitest'

import { prepareRuleSetHTTPClients } from '@/utils/qrs/httpClient'

const remote = (fields: Record<string, unknown> = {}) => ({
  type: 'remote',
  tag: 'rules',
  format: 'source',
  url: 'https://example.com/rules.json',
  ...fields,
})

const withRemote = (
  fields: Record<string, unknown> = {},
  config: Record<string, unknown> = {},
) => ({
  ...config,
  route: { rule_set: [remote(fields)], ...(config.route as Record<string, unknown>) },
})

describe('QRS rule-set HTTP clients', () => {
  it('names an untagged first shared client without mutating it and avoids tag collisions', () => {
    const clients = [
      { engine: 'go', headers: { 'User-Agent': 'QRS' } },
      { tag: 'webui-rule-set-default', engine: 'go' },
      { tag: 'webui-rule-set-default-2', engine: 'go' },
    ]
    const result = prepareRuleSetHTTPClients(withRemote({}, { http_clients: clients }))
    expect(result.http_clients).toEqual([
      { ...clients[0], tag: 'webui-rule-set-default-3' },
      ...clients.slice(1),
    ])
    expect(clients[0]).not.toHaveProperty('tag')
    expect((result.route as Record<string, unknown>).default_http_client).toBe(
      'webui-rule-set-default-3',
    )
  })

  it('is idempotent across default creation and legacy-field migration', () => {
    const input = {
      outbounds: [{ type: 'direct', tag: 'direct' }],
      route: { rule_set: [remote(), remote({ download_detour: 'direct' })] },
    }
    const once = prepareRuleSetHTTPClients(input)
    expect(prepareRuleSetHTTPClients(once)).toEqual(once)
    expect(input.route.rule_set[1]).toHaveProperty('download_detour', 'direct')
  })
})
