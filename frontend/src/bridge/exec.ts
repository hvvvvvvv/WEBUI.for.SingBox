import { apiCall } from './http'

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
