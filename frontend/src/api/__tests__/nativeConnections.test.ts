import { create } from '@bufbuild/protobuf'
import { describe, expect, it } from 'vitest'
import {
  ConnectionEventType as Type,
  ConnectionEventsSchema,
} from '../../../gen/native/daemon/started_service_pb'
import { NativeConnectionsState } from '../nativeConnections'

const batch = (values: Parameters<typeof create<typeof ConnectionEventsSchema>>[1]) =>
  create(ConnectionEventsSchema, values)

describe('native connection reducer', () => {
  it('distinguishes closed entries in reset, preserves bigint totals, and merges sparse updates', () => {
    const state = new NativeConnectionsState()
    const total = 9_007_199_254_740_993n
    let snapshot = state.apply(
      batch({
        reset: true,
        events: [
          { connection: { id: 'live', uplinkTotal: total, downlinkTotal: 10n } },
          {
            type: Type.CONNECTION_EVENT_CLOSED,
            connection: { id: 'closed', closedAt: 1728000000000n },
          },
        ],
      }),
    )
    expect(snapshot.active.map((row) => row.connection.id)).toEqual(['live'])
    expect(snapshot.closed[0]?.closedAt?.getTime()).toBe(1728000000000)
    const frozen = snapshot.active[0]!
    snapshot = state.apply(
      batch({
        events: [
          { id: 'live', type: Type.CONNECTION_EVENT_UPDATE, uplinkDelta: 5n, downlinkDelta: 7n },
        ],
      }),
    )
    expect(snapshot.active[0]?.uplinkTotal).toBe(total + 5n)
    expect(snapshot.active[0]?.uplinkBps).toBe(5)
    expect(snapshot.active[0]?.downlinkBps).toBe(7)
    expect(frozen.uplinkTotal).toBe(total)
    expect(frozen.uplinkBps).toBe(0)
    snapshot = state.apply(batch({ events: [{ connection: { id: 'other' } }] }))
    expect(snapshot.active.find((row) => row.connection.id === 'live')?.uplinkBps).toBe(5)
    snapshot = state.apply(
      batch({
        events: [
          { id: 'live', type: Type.CONNECTION_EVENT_UPDATE, uplinkDelta: 0n, downlinkDelta: 0n },
        ],
      }),
    )
    expect(snapshot.active[0]?.uplinkBps).toBe(0)
    expect(snapshot.active[0]?.downlinkBps).toBe(0)
    expect(snapshot.active[0]?.uplinkTotal).toBe(total + 5n)
  })

  it('closes by ID without losing metadata, ignores unknown sparse IDs, and rebuilds on reset', () => {
    const state = new NativeConnectionsState()
    state.apply(
      batch({ reset: true, events: [{ connection: { id: 'live', domain: 'example.com' } }] }),
    )
    const snapshot = state.apply(
      batch({
        events: [
          { id: 'missing', type: Type.CONNECTION_EVENT_UPDATE, uplinkDelta: 3n },
          { id: 'live', type: Type.CONNECTION_EVENT_CLOSED, closedAt: 1728000000000n },
        ],
      }),
    )
    expect(snapshot.active).toHaveLength(0)
    expect(snapshot.closed[0]?.connection.domain).toBe('example.com')
    expect(snapshot.closed[0]?.uplinkBps).toBe(0)
    expect(state.apply(batch({ reset: true })).closed).toHaveLength(0)
  })

  it('caps closed history at 1000 and clears only closed rows', () => {
    const state = new NativeConnectionsState()
    const snapshot = state.apply(
      batch({
        reset: true,
        events: [
          ...Array.from({ length: 1001 }, (_, i) => ({
            connection: { id: `closed-${i}`, closedAt: BigInt(i + 1) },
          })),
          { connection: { id: 'active' } },
        ],
      }),
    )
    expect(snapshot.closed).toHaveLength(1000)
    expect(snapshot.closed.at(-1)?.connection.id).toBe('closed-1')
    expect(state.clearClosed()).toEqual({ active: snapshot.active, closed: [] })
  })
})
