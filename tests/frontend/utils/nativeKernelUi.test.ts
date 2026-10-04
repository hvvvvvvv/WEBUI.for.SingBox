import { create } from '@bufbuild/protobuf'
import { describe, expect, it } from 'vitest'

import { ConnectionSchema, LogSchema, LogLevel } from '@gen/native/daemon/started_service_pb'
import { freezeConnectionSnapshot, NativeLogBuffer, stringifyNative } from '@/utils/nativeKernelUi'
import type { NativeConnectionSnapshot } from '@/types/kernel'

describe('native kernel UI', () => {
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
    ['OSC hyperlinks', '\x1b]8;;https://example.com\x1b\\节点链接\x1b]8;;\x1b\\', '节点链接'],
    ['incomplete CSI', '\x1b[38;2;1;2', '\x1b[38;2;1;2'],
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
})
