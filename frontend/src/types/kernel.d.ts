import type {
  Connection,
  Group,
  GroupItem,
  Log,
  Status,
} from '../../gen/native/daemon/started_service_pb'

/** UI mode settings from the native mode service. */
export interface RuntimeKernelConfig {
  mode: string
  modeList: string[]
}

export interface NativeConnectionRow {
  connection: Connection
  uplinkBps: number
  downlinkBps: number
  uplinkTotal: bigint
  downlinkTotal: bigint
  closedAt?: Date
}

export interface NativeConnectionSnapshot {
  active: NativeConnectionRow[]
  closed: NativeConnectionRow[]
}

export type { Connection, Group, GroupItem, Log, Status }
