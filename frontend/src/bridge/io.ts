import { apiCall } from './http'

interface IOOptions {
  Mode?: 'Binary' | 'Text'
  Range?: string
}

export const WriteFile = async (path: string, content: string, options: IOOptions = {}) => {
  const { flag, data } = await apiCall<{ flag: boolean; data: string }>('/file/write', path, content, { Mode: 'Text', Range: '', ...options })
  if (!flag) {
    throw data
  }
  return data
}

export const ReadFile = async (path: string, options: IOOptions = {}) => {
  const { flag, data } = await apiCall<{ flag: boolean; data: string }>('/file/read', path, { Mode: 'Text', Range: '', ...options })
  if (!flag) {
    throw data
  }
  return data
}

export const MoveFile = async (source: string, target: string) => {
  const { flag, data } = await apiCall<{ flag: boolean; data: string }>('/file/move', source, target)
  if (!flag) {
    throw data
  }
  return data
}

export const RemoveFile = async (path: string) => {
  const { flag, data } = await apiCall<{ flag: boolean; data: string }>('/file/remove', path)
  if (!flag) {
    throw data
  }
  return data
}

export const CopyFile = async (source: string, target: string) => {
  const { flag, data } = await apiCall<{ flag: boolean; data: string }>('/file/copy', source, target)
  if (!flag) {
    throw data
  }
  return data
}

export const FileExists = async (path: string) => {
  const { flag, data } = await apiCall<{ flag: boolean; data: string }>('/file/exists', path)
  if (!flag) {
    throw data
  }
  return data === 'true'
}

export const AbsolutePath = async (path: string) => {
  const { flag, data } = await apiCall<{ flag: boolean; data: string }>('/file/absolutePath', path)
  if (!flag) {
    throw data
  }
  return data
}

export const MakeDir = async (path: string) => {
  const { flag, data } = await apiCall<{ flag: boolean; data: string }>('/file/makeDir', path)
  if (!flag) {
    throw data
  }
  return data
}

export const ReadDir = async (path: string) => {
  const { flag, data } = await apiCall<{ flag: boolean; data: string }>('/file/readDir', path)
  if (!flag) {
    throw data
  }
  return data
    .split('|')
    .filter((v) => v)
    .map((v) => {
      const [name, size, isDir] = v.split(',') as [string, string, string]
      return { name, size: Number(size), isDir: isDir === 'true' }
    })
}

export const OpenDir = async (path: string) => {
  const { flag, data } = await apiCall<{ flag: boolean; data: string }>('/file/openDir', path)
  if (!flag) {
    throw data
  }
  return data
}

export const OpenURI = async (uri: string) => {
  const { flag, data } = await apiCall<{ flag: boolean; data: string }>('/file/openURI', uri)
  if (!flag) {
    throw data
  }
  return data
}

export const UnzipZIPFile = async (path: string, output: string) => {
  const { flag, data } = await apiCall<{ flag: boolean; data: string }>('/file/unzipZIP', path, output)
  if (!flag) {
    throw data
  }
  return data
}

export const UnzipGZFile = async (path: string, output: string) => {
  const { flag, data } = await apiCall<{ flag: boolean; data: string }>('/file/unzipGZ', path, output)
  if (!flag) {
    throw data
  }
  return data
}

export const UnzipTarGZFile = async (path: string, output: string) => {
  const { flag, data } = await apiCall<{ flag: boolean; data: string }>('/file/unzipTarGZ', path, output)
  if (!flag) {
    throw data
  }
  return data
}
