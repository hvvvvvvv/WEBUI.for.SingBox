import { sameKernelMode } from './nativeKernelUi'

const queues = new Map<string, Promise<unknown>>()

/** Serialize repeated actions, including after a failed request. */
export const queueKernelAction = <T>(key: string, action: () => Promise<T>): Promise<T> => {
  const previous = queues.get(key) ?? Promise.resolve()
  const request = previous.catch(() => {}).then(action)
  queues.set(key, request)
  void request
    .finally(() => {
      if (queues.get(key) === request) queues.delete(key)
    })
    .catch(() => {})
  return request
}

export const selectThenClose = async (
  groupTag: string,
  outboundTag: string,
  autoClose: boolean,
  api: {
    getSnapshot: () => Promise<{ id: string; chainList: string[] }[]>
    select: (group: string, outbound: string) => Promise<unknown>
    close: (id: string) => Promise<unknown>
  },
) => {
  const targets = autoClose
    ? (await api.getSnapshot()).filter((connection) => connection.chainList.includes(groupTag))
    : []
  await api.select(groupTag, outboundTag)
  await Promise.all(targets.map((connection) => api.close(connection.id)))
}

export const changeModeThenClose = async (
  mode: string,
  api: {
    setMode: (mode: string) => Promise<{ currentMode: string }>
    closeAll: () => Promise<unknown>
  },
) => {
  const status = await api.setMode(mode)
  if (!sameKernelMode(status.currentMode, mode))
    throw new Error('Kernel mode change was not confirmed')
  await api.closeAll()
}
