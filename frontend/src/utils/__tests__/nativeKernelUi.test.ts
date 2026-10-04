import { create } from '@bufbuild/protobuf'
import { describe, expect, it, vi } from 'vitest'

import {
  ConnectionSchema,
  LogSchema,
  LogLevel,
} from '../../../gen/native/daemon/started_service_pb'
import {
  addressCIDR,
  connectionRuleOptions,
  freezeConnectionSnapshot,
  migrateConnectionColumns,
  NativeLogBuffer,
  nativeModeOptions,
  sameKernelMode,
  stringifyNative,
} from '../nativeKernelUi'
import type { NativeConnectionSnapshot } from '@/types/kernel'

vi.mock('@/lang', () => ({ default: { global: { locale: { value: 'en' } } } }))
import { formatBytes } from '../format'

describe('native kernel UI', () => {
  it('preserves mode values including custom modes and compares them without case', () => {
    expect(nativeModeOptions(['Rule', 'Global', 'custom'])).toEqual([
      { value: 'Rule', label: 'kernel.rule', desc: 'kernel.ruleDesc' },
      { value: 'Global', label: 'kernel.global', desc: 'kernel.globalDesc' },
      { value: 'custom', label: 'custom', desc: '' },
    ])
    expect(sameKernelMode('Rule', 'rule')).toBe(true)
    expect(sameKernelMode('custom', 'rule')).toBe(false)
  })

  it('shows one rule option when the native list includes Rule and rule', () => {
    const options = nativeModeOptions(['Rule', 'Direct', 'Global', 'rule'])
    expect(options).toEqual([
      { value: 'Rule', label: 'kernel.rule', desc: 'kernel.ruleDesc' },
      { value: 'Direct', label: 'kernel.direct', desc: 'kernel.directDesc' },
      { value: 'Global', label: 'kernel.global', desc: 'kernel.globalDesc' },
    ])
    expect(options.filter(({ value }) => sameKernelMode('rule', value))).toHaveLength(1)
  })

  it('deduplicates all modes without case and preserves the first API value and order', () => {
    expect(
      nativeModeOptions(['rule', 'Rule', 'Work', 'work', 'GLOBAL', 'global', 'Work', 'direct']),
    ).toEqual([
      { value: 'rule', label: 'kernel.rule', desc: 'kernel.ruleDesc' },
      { value: 'Work', label: 'Work', desc: '' },
      { value: 'GLOBAL', label: 'kernel.global', desc: 'kernel.globalDesc' },
      { value: 'direct', label: 'kernel.direct', desc: 'kernel.directDesc' },
    ])
  })

  it.each([
    ['192.0.2.1:443', '192.0.2.1/32'],
    ['[2001:db8::1]:443', '2001:db8::1/128'],
    ['[::ffff:192.0.2.1]:443', '::ffff:192.0.2.1/128'],
    ['[fe80::1%eth0]:443', 'fe80::1/128'],
    ['2001:db8::1', '2001:db8::1/128'],
    ['api.example.com:443', undefined],
    ['192.0.2.999:443', undefined],
    ['[2001:::1]:443', undefined],
  ])('extracts safe rule CIDRs from %s', (endpoint, expected) => {
    expect(addressCIDR(endpoint)).toBe(expected)
  })

  it('creates rules from native IPv6 destinations, domains and process metadata', () => {
    const options = connectionRuleOptions(
      create(ConnectionSchema, {
        domain: 'api.example.com',
        destination: '[2001:db8::1]:443',
        processInfo: { processPath: '/usr/bin/client' },
      }),
    )
    expect(options).toEqual([
      { type: 'domain', value: 'api.example.com' },
      { type: 'domain_suffix', value: '.example.com' },
      { type: 'ip_cidr', value: '2001:db8::1/128' },
      { type: 'process_path', value: '/usr/bin/client' },
    ])
  })

  it('migrates old column preferences, preserves order, and drops invalid keys', () => {
    const defaults = {
      visibility: { 'connection.domain': true, uplinkTotal: true, 'connection.createdAt': true },
      order: ['connection.domain', 'uplinkTotal', 'connection.createdAt'],
    }
    expect(
      migrateConnectionColumns(
        {
          visibility: { 'metadata.host': false, upload: true, unknown: true },
          order: ['upload', 'metadata.host', 'upload', 'unknown'],
        },
        defaults,
      ),
    ).toEqual({
      visibility: { 'connection.domain': false, uplinkTotal: true, 'connection.createdAt': true },
      order: ['uplinkTotal', 'connection.domain', 'connection.createdAt'],
    })
  })

  it('freezes displayed connections while live traffic and metadata keep changing', () => {
    const live: NativeConnectionSnapshot = {
      active: [
        {
          connection: create(ConnectionSchema, {
            id: 'one',
            chainList: ['proxy'],
            processInfo: { packageNames: ['app'] },
          }),
          uplinkBps: 10,
          downlinkBps: 20,
          uplinkTotal: 9007199254740993n,
          downlinkTotal: 2n,
        },
      ],
      closed: [],
    }
    const frozen = freezeConnectionSnapshot(live)
    live.active[0]!.uplinkBps = 30
    live.active[0]!.connection.chainList.push('new')
    live.active[0]!.connection.processInfo!.packageNames.push('second')
    expect(frozen.active[0]!.uplinkBps).toBe(10)
    expect(frozen.active[0]!.connection.chainList).toEqual(['proxy'])
    expect(frozen.active[0]!.connection.processInfo!.packageNames).toEqual(['app'])
    expect(freezeConnectionSnapshot(live).active[0]!.uplinkBps).toBe(30)
    expect(JSON.parse(stringifyNative(frozen)).active[0].uplinkTotal).toBe('9007199254740993')
  })

  it('replaces batched log history on reset, preserves enum levels, and bounds history', () => {
    const buffer = new NativeLogBuffer()
    buffer.append(create(LogSchema, { messages: [{ level: LogLevel.DEBUG, message: 'old' }] }))
    const frozen = buffer.rows.slice()
    buffer.append(
      create(LogSchema, {
        reset: true,
        messages: [
          { level: LogLevel.INFO, message: 'one' },
          { level: LogLevel.PANIC, message: 'two' },
        ],
      }),
    )
    expect(buffer.rows.map(({ type, payload }) => [type, payload])).toEqual([
      ['panic', 'two'],
      ['info', 'one'],
    ])
    expect(frozen[0]!.payload).toBe('old')
    buffer.append(
      create(LogSchema, {
        messages: Array.from({ length: 1001 }, (_, index) => ({ message: String(index) })),
      }),
    )
    expect(buffer.rows).toHaveLength(1000)
    expect(new Set(buffer.rows.map(({ id }) => id)).size).toBe(1000)
    buffer.clear()
    expect(buffer.rows).toEqual([])
  })

  it.each([
    [
      'sing-box colors and connection IDs',
      '\x1b[36mINFO\x1b[0m[0159] [\x1b[38;5;132m12345\x1b[0m 0ms] outbound: 节点连接',
      'INFO[0159] [12345 0ms] outbound: 节点连接',
    ],
    ['truecolor', '\x1b[38;2;255;128;0m节点\x1b[0m', '节点'],
    ['colon color parameters', '\x1b[38:2::255:128:0m节点\x1b[0m', '节点'],
    ['cursor and erase controls', '\x1b[?25l\x1b[2K\x1b[1GINFO\x1b[?25h', 'INFO'],
    ['CSI intermediate bytes', '\x1b[0 qINFO', 'INFO'],
    ['OSC with BEL', 'before\x1b]0;terminal title\x07after', 'beforeafter'],
    ['OSC with ST', '\x1b]0;terminal title\x1b\\INFO', 'INFO'],
    ['OSC hyperlinks', '\x1b]8;;https://example.com\x1b\\节点链接\x1b]8;;\x1b\\', '节点链接'],
    ['8-bit CSI', '\x9b31mERROR\x9b0m', 'ERROR'],
    ['8-bit OSC and ST', '\x9d8;;https://example.com\x9c节点\x9d8;;\x9c', '节点'],
    [
      'ordinary log text',
      '\n中文 [2001:db8::1]:443\t[31m正文 https://example.com\n',
      '\n中文 [2001:db8::1]:443\t[31m正文 https://example.com\n',
    ],
    ['literal escape text', String.raw`\x1b[31mERROR`, String.raw`\x1b[31mERROR`],
    ['incomplete CSI', '\x1b[38;2;1;2', '\x1b[38;2;1;2'],
    ['incomplete OSC', '\x1b]0;terminal title', '\x1b]0;terminal title'],
  ])('cleans %s before storing initial and live log entries', (_name, message, expected) => {
    const buffer = new NativeLogBuffer()
    const initial = create(LogSchema, {
      reset: true,
      messages: [{ level: LogLevel.INFO, message }],
    })
    buffer.append(initial)
    expect(buffer.rows[0]).toMatchObject({ type: 'info', payload: expected })
    expect(initial.messages[0]?.message).toBe(message)

    buffer.append(create(LogSchema, { messages: [{ level: LogLevel.ERROR, message }] }))
    expect(buffer.rows.map(({ type, payload }) => [type, payload])).toEqual([
      ['error', expected],
      ['info', expected],
    ])
  })

  it('formats uint64 traffic totals without first losing integer precision', () => {
    expect(formatBytes(0n)).toBe('0 B')
    expect(formatBytes(1024n)).toBe('1 KB')
    expect(formatBytes(18446744073709551615n)).toBe('16 EB')
  })
})
