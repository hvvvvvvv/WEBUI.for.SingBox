import { ConnectionEventType } from '../../gen/native/daemon/started_service_pb'
import type { Connection, ConnectionEvents } from '../../gen/native/daemon/started_service_pb'
import type { NativeConnectionRow, NativeConnectionSnapshot } from '@/types/kernel'

export const NATIVE_INTERVAL = 1_000_000_000n
const maxClosedConnections = 1000
const rate = (delta: bigint) => (Number(delta) * 1e9) / Number(NATIVE_INTERVAL)

/** Reducer kept outside the UI so freezing the table never loses kernel events. */
export class NativeConnectionsState {
  private rows = new Map<string, NativeConnectionRow>()

  clear() {
    this.rows.clear()
  }

  clearClosed() {
    for (const [id, row] of this.rows) if (row.closedAt) this.rows.delete(id)
    return this.snapshot()
  }

  apply(batch: ConnectionEvents): NativeConnectionSnapshot {
    if (batch.reset) this.clear()
    // NEW/CLOSED can arrive between ticks. Only an explicit UPDATE changes a rate.
    for (const event of batch.events) {
      const id = event.id || event.connection?.id
      if (!id) continue
      let row = this.rows.get(id)
      if (event.connection) {
        row = this.fromConnection(event.connection)
        this.rows.set(id, row)
      }
      if (!row) continue
      if (event.type === ConnectionEventType.CONNECTION_EVENT_UPDATE) {
        row.uplinkTotal += event.uplinkDelta
        row.downlinkTotal += event.downlinkDelta
        row.uplinkBps = rate(event.uplinkDelta)
        row.downlinkBps = rate(event.downlinkDelta)
        row.connection = {
          ...row.connection,
          uplinkTotal: row.uplinkTotal,
          downlinkTotal: row.downlinkTotal,
          uplink: event.uplinkDelta,
          downlink: event.downlinkDelta,
        }
      }
      const closedAt = event.closedAt || event.connection?.closedAt || 0n
      if (event.type === ConnectionEventType.CONNECTION_EVENT_CLOSED || closedAt > 0n) {
        row.closedAt = closedAt > 0n ? new Date(Number(closedAt)) : row.closedAt || new Date()
        row.connection = { ...row.connection, closedAt: BigInt(row.closedAt.getTime()) }
        row.uplinkBps = 0
        row.downlinkBps = 0
      }
    }
    const closed = [...this.rows.entries()]
      .filter(([, row]) => row.closedAt)
      .sort((a, b) => b[1].closedAt!.getTime() - a[1].closedAt!.getTime())
    for (const [id] of closed.slice(maxClosedConnections)) this.rows.delete(id)
    return this.snapshot()
  }

  snapshot(): NativeConnectionSnapshot {
    const active: NativeConnectionRow[] = []
    const closed: NativeConnectionRow[] = []
    for (const row of this.rows.values()) {
      // Each notification has independent rows; a paused UI retains its frozen snapshot.
      const copy = { ...row }
      if (row.closedAt) closed.push(copy)
      else active.push(copy)
    }
    closed.sort((a, b) => b.closedAt!.getTime() - a.closedAt!.getTime())
    return { active, closed }
  }

  private fromConnection(connection: Connection): NativeConnectionRow {
    return {
      connection,
      uplinkTotal: connection.uplinkTotal,
      downlinkTotal: connection.downlinkTotal,
      uplinkBps: rate(connection.uplink),
      downlinkBps: rate(connection.downlink),
      closedAt: connection.closedAt > 0n ? new Date(Number(connection.closedAt)) : undefined,
    }
  }
}
