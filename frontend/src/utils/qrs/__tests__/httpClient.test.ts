import { describe, expect, it } from 'vitest'

import { HTTPClientPreparationError, prepareRuleSetHTTPClients } from '../httpClient'

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
  it('creates an explicit direct default for remote-only profiles with no outbounds', () => {
    const input = withRemote()
    expect(prepareRuleSetHTTPClients(input)).toEqual({
      http_clients: [{ tag: 'webui-rule-set-default', engine: 'go' }],
      route: {
        ...input.route,
        default_http_client: 'webui-rule-set-default',
      },
    })
    expect(input).not.toHaveProperty('http_clients')
    expect(input.route).not.toHaveProperty('default_http_client')
  })

  it.each(['selector', 'urltest', 'socks'])(
    'follows route.final through a %s outbound and preserves the selected group',
    (type) => {
      const result = prepareRuleSetHTTPClients(
        withRemote(
          {},
          {
            outbounds: [
              { tag: 'first', type: 'direct' },
              { tag: 'proxy', type },
            ],
            route: { final: 'proxy' },
          },
        ),
      )
      expect(result.http_clients).toEqual([{ tag: 'webui-rule-set-default', detour: 'proxy' }])
    },
  )

  it('follows the first outbound using the core index tag when route.final is empty', () => {
    const result = prepareRuleSetHTTPClients(
      withRemote(
        {},
        {
          outbounds: [{ type: 'socks' }, { tag: 'direct', type: 'direct' }],
        },
      ),
    )
    expect(result.http_clients).toEqual([{ tag: 'webui-rule-set-default', detour: '0' }])
  })

  it('copies all direct dial fields while excluding outbound-only fields', () => {
    const direct = {
      type: 'direct',
      tag: 'direct',
      bind_interface: 'eth0',
      inet4_bind_address: '192.0.2.2',
      inet6_bind_address: '2001:db8::2',
      bind_address_no_port: true,
      protect_path: '/tmp/protect',
      routing_mark: '0x1',
      reuse_addr: true,
      netns: 'ns',
      connect_timeout: '5s',
      tcp_fast_open: true,
      tcp_multi_path: true,
      disable_tcp_keep_alive: true,
      tcp_keep_alive: '20s',
      tcp_keep_alive_interval: '5s',
      udp_fragment: false,
      domain_resolver: { server: 'dns', strategy: 'prefer_ipv6' },
      network_strategy: 'hybrid',
      network_type: ['wifi'],
      fallback_network_type: 'cellular',
      fallback_delay: '300ms',
      domain_strategy: 'prefer_ipv4',
      override_address: '192.0.2.9',
      override_port: 8080,
    }
    const { type, tag, override_address, override_port, ...dialFields } = direct
    const result = prepareRuleSetHTTPClients(
      withRemote(
        { download_detour: 'direct' },
        {
          outbounds: [direct],
        },
      ),
    )
    expect((result.route as Record<string, any>).rule_set[0].http_client).toEqual({
      engine: 'go',
      ...dialFields,
    })
    expect((result.route as Record<string, any>).rule_set[0].http_client.domain_resolver).not.toBe(
      direct.domain_resolver,
    )
    expect(result).not.toHaveProperty('http_clients')
    expect((result.route as Record<string, any>).rule_set[0]).not.toHaveProperty('download_detour')
  })

  it('does not copy direct detour, which core does not support', () => {
    const result = prepareRuleSetHTTPClients(
      withRemote(
        { download_detour: 'direct' },
        {
          outbounds: [
            { type: 'direct', tag: 'direct', detour: 'proxy' },
            { type: 'socks', tag: 'proxy' },
          ],
        },
      ),
    )
    expect((result.route as Record<string, any>).rule_set[0].http_client).toEqual({
      engine: 'go',
    })
  })

  it('resolves endpoints and gives outbound tags precedence over matching endpoints', () => {
    const endpoint = prepareRuleSetHTTPClients(
      withRemote(
        {},
        {
          endpoints: [{ type: 'wireguard', tag: 'vpn' }],
          route: { final: 'vpn' },
        },
      ),
    )
    expect(endpoint.http_clients).toEqual([{ tag: 'webui-rule-set-default', detour: 'vpn' }])
    const collision = prepareRuleSetHTTPClients(
      withRemote(
        { download_detour: 'vpn' },
        {
          outbounds: [{ type: 'direct', tag: 'vpn' }],
          endpoints: [{ type: 'wireguard', tag: 'vpn' }],
        },
      ),
    )
    expect((collision.route as Record<string, any>).rule_set[0].http_client).toEqual({
      engine: 'go',
    })
  })

  it('resolves an untagged endpoint by its core index', () => {
    const result = prepareRuleSetHTTPClients(
      withRemote(
        { download_detour: '0' },
        {
          endpoints: [{ type: 'wireguard' }],
        },
      ),
    )
    expect((result.route as Record<string, any>).rule_set[0].http_client).toEqual({ detour: '0' })
  })

  it('supports the core virtual direct fallback only when no final or outbound exists', () => {
    const result = prepareRuleSetHTTPClients(withRemote({ download_detour: 'direct' }))
    expect((result.route as Record<string, any>).rule_set[0].http_client).toEqual({ engine: 'go' })
    expect(() => prepareRuleSetHTTPClients(withRemote({}, { route: { final: 'direct' } }))).toThrow(
      'route.final',
    )
  })

  it('gives the virtual direct outbound precedence over an endpoint with the same tag', () => {
    const result = prepareRuleSetHTTPClients(
      withRemote(
        { download_detour: 'direct' },
        {
          endpoints: [{ type: 'wireguard', tag: 'direct' }],
        },
      ),
    )
    expect((result.route as Record<string, any>).rule_set[0].http_client).toEqual({ engine: 'go' })
  })

  it('honors an explicit default shared client instead of route.final or the first client', () => {
    const clients = [
      { tag: 'first', engine: 'go' },
      { tag: 'chosen', headers: { Token: 'secret' } },
    ]
    const config = withRemote(
      {},
      {
        http_clients: clients,
        route: { final: 'absent', default_http_client: 'chosen' },
      },
    )
    const result = prepareRuleSetHTTPClients(config)
    expect(result.http_clients).toBe(clients)
    expect((result.route as Record<string, unknown>).default_http_client).toBe('chosen')
  })

  it('sets the default reference to an already tagged first shared client', () => {
    const clients = [{ tag: 'custom', engine: 'go' }]
    const result = prepareRuleSetHTTPClients(withRemote({}, { http_clients: clients }))
    expect(result.http_clients).toBe(clients)
    expect((result.route as Record<string, unknown>).default_http_client).toBe('custom')
  })

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

  it.each([{}, { engine: 'go', headers: { 'User-Agent': 'rules' } }])(
    'preserves an explicit inline client and removes the legacy field',
    (client) => {
      const result = prepareRuleSetHTTPClients(
        withRemote({ http_client: client, download_detour: 42 }),
      )
      expect((result.route as Record<string, any>).rule_set[0].http_client).toBe(client)
      expect((result.route as Record<string, any>).rule_set[0]).not.toHaveProperty(
        'download_detour',
      )
      expect(result).not.toHaveProperty('http_clients')
    },
  )

  it('preserves a per-rule shared-client reference and its settings', () => {
    const clients = [{ tag: 'custom', engine: 'go', headers: { Authorization: 'token' } }]
    const result = prepareRuleSetHTTPClients(
      withRemote({ http_client: 'custom' }, { http_clients: clients }),
    )
    expect((result.route as Record<string, any>).rule_set[0].http_client).toBe('custom')
    expect(result.http_clients).toBe(clients)
    expect(result.route).not.toHaveProperty('default_http_client')
  })

  it('keeps unused shared clients with invalid detours while checking the effective default', () => {
    const clients = [
      { tag: 'active', engine: 'go' },
      { tag: 'unused', detour: 'missing' },
    ]
    const result = prepareRuleSetHTTPClients(
      withRemote({ http_client: {} }, { http_clients: clients }),
    )
    expect(result.http_clients).toBe(clients)
    expect(result.route).not.toHaveProperty('default_http_client')
    expect(() =>
      prepareRuleSetHTTPClients(
        withRemote(
          { http_client: {} },
          {
            http_clients: clients,
            route: { default_http_client: 'unused' },
          },
        ),
      ),
    ).toThrow('http_clients[1].detour')
  })

  it('does not activate an untagged shared client when all remote clients are explicit', () => {
    const clients = [{ detour: 'missing' }]
    const result = prepareRuleSetHTTPClients(
      withRemote({ http_client: {} }, { http_clients: clients }),
    )
    expect(result.http_clients).toBe(clients)
    expect(result.route).not.toHaveProperty('default_http_client')
    expect(() => prepareRuleSetHTTPClients(withRemote({}, { http_clients: clients }))).toThrow(
      'http_clients[0].detour',
    )
  })

  it.each([null, ''])('treats %s per-rule client as unset', (client) => {
    const result = prepareRuleSetHTTPClients(withRemote({ http_client: client }))
    expect((result.route as Record<string, unknown>).default_http_client).toBe(
      'webui-rule-set-default',
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

  it('does not add or validate clients for local or inline-only configs', () => {
    const config = {
      http_clients: false,
      route: { final: 'absent', rule_set: [{ type: 'inline', rules: [] }] },
    }
    expect(prepareRuleSetHTTPClients(config)).toBe(config)
  })

  it.each([
    [{ route: { rule_set: 'invalid' } }, 'route.rule_set'],
    [{ route: { rule_set: [null] } }, 'route.rule_set[0]'],
    [withRemote({ http_client: false }), 'route.rule_set[0].http_client'],
    [withRemote({ download_detour: 42 }), 'route.rule_set[0].download_detour'],
    [withRemote({ http_client: 'missing' }), 'route.rule_set[0].http_client'],
    [withRemote({ http_client: { detour: 'missing' } }), 'route.rule_set[0].http_client.detour'],
    [withRemote({}, { http_clients: {} }), 'http_clients'],
    [withRemote({}, { http_clients: [null] }), 'http_clients[0]'],
    [withRemote({}, { http_clients: [{ tag: 1 }] }), 'http_clients[0].tag'],
    [withRemote({}, { route: { default_http_client: 'missing' } }), 'route.default_http_client'],
    [withRemote({}, { route: { default_http_client: 1 } }), 'route.default_http_client'],
    [
      withRemote({ http_client: {} }, { route: { default_http_client: 'missing' } }),
      'route.default_http_client',
    ],
    [
      withRemote({ http_client: {} }, { route: { default_http_client: 1 } }),
      'route.default_http_client',
    ],
    [withRemote({}, { route: { final: 'missing' } }), 'route.final'],
    [withRemote({}, { route: { final: 1 } }), 'route.final'],
    [withRemote({}, { outbounds: false }), 'outbounds'],
  ])('rejects invalid migration input with path %s', (config, path) => {
    expect(() => prepareRuleSetHTTPClients(config)).toThrow(HTTPClientPreparationError)
    expect(() => prepareRuleSetHTTPClients(config)).toThrow(path)
  })
})
