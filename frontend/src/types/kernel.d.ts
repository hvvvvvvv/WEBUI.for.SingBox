import type {
  Connection,
  Group,
  GroupItem,
  Log,
  Status,
} from '../../gen/native/daemon/started_service_pb'

/** UI settings synthesized from the current runtime profile and native mode service. */
export interface RuntimeKernelConfig {
  port: number
  'socks-port': number
  'mixed-port': number
  'interface-name': string
  'allow-lan': boolean
  mode: string
  modeList: string[]
  tun: { enable: boolean; stack: string; device: string }
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
