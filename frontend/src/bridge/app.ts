import type { AppEnv } from '@/types/app'
import { apiCall } from './http'

export const ExitApp = () => apiCall('/app/exit')

export const GetEnv = <T extends string | undefined = undefined>(
  key?: T,
): Promise<T extends string ? string : AppEnv> => {
  return apiCall('/app/env', key || '')
}

export const GetInterfaces = async () => {
  const { flag, data } = await apiCall<{ flag: boolean; data: string }>('/app/interfaces')
  if (!flag) {
    throw data
  }
  return data.split('|')
}

export const Notify = async (title: string, body: string) => {
  if (!('Notification' in window)) {
    throw new Error('Notifications not available in this browser')
  }
  if (Notification.permission !== 'granted') {
    const perm = await Notification.requestPermission()
    if (perm !== 'granted') {
      throw new Error('Notification permission denied')
    }
  }
  new Notification(title, { body })
}
