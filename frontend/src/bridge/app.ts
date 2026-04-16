import type { AppEnv } from '@/types/app'
import { apiCall } from './http'
import { sampleID } from '@/utils'

export const RestartApp = () => apiCall('/app/restart')

export const ExitApp = () => apiCall('/app/exit')

export const ShowMainWindow = () => apiCall('/app/showMainWindow')

export const UpdateTray = (tray: any) => apiCall('/tray/update', tray)

export const UpdateTrayMenus = (menus: any[]) => apiCall('/tray/updateMenus', menus)

export const UpdateTrayAndMenus = (tray: any, menus: any[]) =>
  apiCall('/tray/updateTrayAndMenus', tray, menus)

export const GetEnv = <T extends string | undefined = undefined>(
  key?: T,
): Promise<T extends string ? string : AppEnv> => {
  return apiCall('/app/env', key || '')
}

export const IsStartup = () => apiCall<boolean>('/app/isStartup')

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
