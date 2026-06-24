import { defineStore } from 'pinia'
import { ref } from 'vue'

import { createRpcClient } from '@/bridge'
import { DefaultSubscribeScript } from '@/constant/app'
import { DefaultExcludeProtocols } from '@/constant/kernel'
import { RequestMethod } from '@/enums/app'
import { sampleID, eventBus } from '@/utils'
import { SubscriptionService } from '../../gen/app/v1/subscription_service_pb'

import type { Subscription } from '@/types/app'
import type { TaskResult } from '../../gen/app/v1/task_pb'

const parseList = <T>(items: string[]) => items.map((v) => JSON.parse(v) as T)

export const useSubscribesStore = defineStore('subscribes', () => {
  const subscribes = ref<Subscription[]>([])
  const service = createRpcClient(SubscriptionService)

  const setupSubscribes = async () => {
    const { subscriptionsJson } = await service.listSubscriptions({})
    subscribes.value = parseList<Subscription>(subscriptionsJson)
  }

  const saveSubscribes = async () => {
    const { subscriptionsJson } = await service.saveSubscriptions({
      subscriptionsJson: subscribes.value.map((v) => JSON.stringify(v)),
    })
    subscribes.value = parseList<Subscription>(subscriptionsJson)
    eventBus.emit('subscriptionsChange', undefined)
  }

  const addSubscribe = async (s: Subscription) => {
    const { subscriptionJson } = await service.upsertSubscription({
      subscriptionJson: JSON.stringify(s),
    })
    subscribes.value.push(JSON.parse(subscriptionJson))
    eventBus.emit('subscriptionChange', { id: s.id })
  }

  const importSubscribe = async (name: string, url: string) => {
    await addSubscribe(getSubscribeTemplate(name, { url }))
  }

  const deleteSubscribe = async (id: string) => {
    await service.deleteSubscription({ id })
    const idx = subscribes.value.findIndex((v) => v.id === id)
    idx !== -1 && subscribes.value.splice(idx, 1)
    eventBus.emit('subscriptionChange', { id })
  }

  const editSubscribe = async (id: string, s: Subscription) => {
    const { subscriptionJson } = await service.upsertSubscription({
      subscriptionJson: JSON.stringify({ ...s, id }),
    })
    const item = JSON.parse(subscriptionJson) as Subscription
    const idx = subscribes.value.findIndex((v) => v.id === id)
    if (idx === -1) {
      subscribes.value.push(item)
    } else {
      subscribes.value.splice(idx, 1, item)
    }
    eventBus.emit('subscriptionChange', { id })
  }

  const updateSubscribe = async (id: string): Promise<TaskResult[]> => {
    const s = subscribes.value.find((v) => v.id === id)
    if (s) s.updating = true
    try {
      const { results } = await service.updateSubscription({ id })
      await setupSubscribes()
      eventBus.emit('subscriptionChange', { id })
      return results
    } finally {
      const next = subscribes.value.find((v) => v.id === id)
      if (next) next.updating = false
    }
  }

  const updateSubscribes = async (): Promise<TaskResult[]> => {
    subscribes.value.forEach((v) => !v.disabled && (v.updating = true))
    try {
      const { results } = await service.updateAllSubscriptions({})
      await setupSubscribes()
      eventBus.emit('subscriptionsChange', undefined)
      return results
    } finally {
      subscribes.value.forEach((v) => (v.updating = false))
    }
  }

  const getSubscribeById = (id: string) => subscribes.value.find((v) => v.id === id)

  const getSubscribeTemplate = (name = '', options: { url?: string } = {}): Subscription => {
    const id = sampleID()
    return {
      id,
      name,
      upload: 0,
      download: 0,
      total: 0,
      expire: 0,
      updateTime: 0,
      type: 'Http',
      url: options.url || '',
      website: '',
      path: `data/subscribes/${id}.json`,
      include: '',
      exclude: '',
      includeProtocol: '',
      excludeProtocol: DefaultExcludeProtocols,
      proxyPrefix: '',
      disabled: false,
      inSecure: false,
      requestMethod: RequestMethod.Get,
      requestTimeout: 15,
      header: {
        request: {},
        response: {},
      },
      proxies: [],
      script: DefaultSubscribeScript,
    }
  }

  return {
    subscribes,
    setupSubscribes,
    saveSubscribes,
    addSubscribe,
    editSubscribe,
    deleteSubscribe,
    updateSubscribe,
    updateSubscribes,
    getSubscribeById,
    importSubscribe,
    getSubscribeTemplate,
  }
})
