import { AppSystemService } from '../../gen/app/v1/app_system_service_pb'
import { createRpcClient } from './rpc'

const appSystemService = createRpcClient(AppSystemService)

const serviceStartTimeRequestTimeout = 3_000

export const GetServiceStartTime = async () => {
  const controller = new AbortController()
  const timer = window.setTimeout(() => controller.abort(), serviceStartTimeRequestTimeout)
  try {
    const response = await fetch(`/api/app/start-time?_=${Date.now()}`, {
      cache: 'no-store',
      signal: controller.signal,
    })
    if (!response.ok) {
      throw new Error(`Service start time request failed: ${response.status} ${response.statusText}`)
    }
    const data = (await response.json()) as { started_at?: unknown }
    if (
      typeof data.started_at !== 'number' ||
      !Number.isSafeInteger(data.started_at) ||
      data.started_at <= 0
    ) {
      throw new Error('Service start time response is invalid')
    }
    return data.started_at
  } finally {
    window.clearTimeout(timer)
  }
}

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
