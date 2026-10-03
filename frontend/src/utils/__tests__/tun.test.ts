import { describe, expect, it } from 'vitest'

import { validateTunInbounds } from '../tun'

const makeTun = (overrides: Record<string, unknown> = {}, enable = true): IInbound =>
  ({
    id: 'tun-in',
    tag: 'tun-in',
    type: 'tun',
    enable,
    tun: { dns_mode: 'hijack', auto_route: true, auto_redirect: false, ...overrides },
  }) as IInbound

describe('TUN form validation', () => {
  it.each(['linux', 'windows', 'darwin', 'android'])(
    'validates DNS mode on %s regardless of auto_route',
    (platformOS) => {
      for (const autoRoute of [true, false]) {
        expect(
          validateTunInbounds(
            [makeTun({ dns_mode: 'invalid', auto_route: autoRoute })],
            platformOS,
          ),
        ).toBe('kernel.inbounds.tun.dns_mode_invalid')
      }
    },
  )

  it.each(['disabled', 'native', 'hijack', '', undefined])('accepts DNS mode %s', (mode) => {
    expect(validateTunInbounds([makeTun({ dns_mode: mode })], 'linux')).toBe('')
  })

  it.each([false, null, 1, []])('rejects a malformed DNS mode (%s)', (mode) => {
    expect(validateTunInbounds([makeTun({ dns_mode: mode })], 'linux')).toBe(
      'kernel.inbounds.tun.dns_mode_invalid',
    )
  })

  it('ignores disabled TUNs and inactive TUN options on another inbound type', () => {
    const inactive = { ...makeTun({ dns_mode: 'invalid' }), type: 'mixed' } as IInbound
    expect(validateTunInbounds([makeTun({ dns_mode: 'invalid' }, false), inactive], 'linux')).toBe(
      '',
    )
  })

  it('preserves Linux auto_route index and redirect checks', () => {
    expect(validateTunInbounds([makeTun({ iproute2_table_index: 0 })], 'linux')).toBe(
      'kernel.inbounds.tun.iproute2_table_index_invalid',
    )
    expect(validateTunInbounds([makeTun({ iproute2_rule_index: -1 })], 'linux')).toBe(
      'kernel.inbounds.tun.iproute2_rule_index_invalid',
    )
    expect(
      validateTunInbounds(
        [makeTun({ auto_redirect: true }), makeTun({ auto_redirect: true })],
        'linux',
      ),
    ).toBe('kernel.inbounds.tun.auto_redirect_conflict')
    expect(
      validateTunInbounds([makeTun({ iproute2_table_index: 0, auto_route: false })], 'linux'),
    ).toBe('')
    expect(validateTunInbounds([makeTun({ iproute2_table_index: 0 })], 'windows')).toBe('')
  })
})
