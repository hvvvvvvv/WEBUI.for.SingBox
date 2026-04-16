import { apiCall } from './http'
import { EventsOn, EventsOff } from './ws'

import { sampleID } from '@/utils'

interface ExecOptions {
  PidFile?: string
  Convert?: boolean
  Env?: Record<string, any>
  StopOutputKeyword?: string
  WorkingDirectory?: string
  convert?: boolean
  env?: Record<string, any>
  stopOutputKeyword?: string
}

const mergeExecOptions = (options: ExecOptions) => {
  const mergedExecOpts = {
    PidFile: options.PidFile ?? '',
    Convert: options.Convert ?? options.convert ?? false,
    Env: options.Env ?? options.env ?? {},
    StopOutputKeyword: options.StopOutputKeyword ?? options.stopOutputKeyword ?? '',
    WorkingDirectory: options.WorkingDirectory ?? '',
  }
  return mergedExecOpts
}

export const Exec = async (path: string, args: string[], options: ExecOptions = {}) => {
  const { flag, data } = await apiCall<{ flag: boolean; data: string }>('/exec/run', path, args, mergeExecOptions(options))
  if (!flag) {
    throw data
  }
  return data
}

export const ExecBackground = async (
  path: string,
  args: string[] = [],
  onOut?: (out: string) => void,
  onEnd?: () => void,
  options: ExecOptions = {},
) => {
  const outEvent = (onOut && sampleID()) || ''
  const endEvent = (onEnd && sampleID()) || (outEvent && sampleID()) || ''

  const { flag, data } = await apiCall<{ flag: boolean; data: string }>(
    '/exec/background',
    path,
    args,
    outEvent,
    endEvent,
    mergeExecOptions(options),
  )
  if (!flag) {
    throw data
  }

  if (outEvent) {
    EventsOn(outEvent, onOut!)
  }

  if (endEvent) {
    EventsOn(endEvent, () => {
      outEvent && EventsOff(outEvent)
      EventsOff(endEvent)
      onEnd?.()
    })
  }

  return Number(data)
}

export const ProcessInfo = async (pid: number) => {
  const { flag, data } = await apiCall<{ flag: boolean; data: string }>('/exec/processInfo', pid)
  if (!flag) {
    throw data
  }
  return data
}

export const ProcessMemory = async (pid: number) => {
  const { flag, data } = await apiCall<{ flag: boolean; data: string }>('/exec/processMemory', pid)
  if (!flag) {
    throw data
  }
  return Number(data)
}

export const KillProcess = async (pid: number, timeout = 10) => {
  const { flag, data } = await apiCall<{ flag: boolean; data: string }>('/exec/killProcess', pid, timeout)
  if (!flag) {
    throw data
  }
  return data
}
