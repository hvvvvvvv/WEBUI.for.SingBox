import { AppSystemService } from '../../gen/app/v1/app_system_service_pb'
import { createRpcClient } from './rpc'

const appSystemService = createRpcClient(AppSystemService)

export const GetInterfaces = async () => {
  const { interfaces } = await appSystemService.getInterfaces({})
  return interfaces
}

export const GetPlatform = async () => {
  const { os } = await appSystemService.getPlatform({})
  return os
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
