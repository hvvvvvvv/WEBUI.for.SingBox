import { defineStore } from 'pinia'
import { reactive, ref } from 'vue'

import { createRpcClient } from '@/bridge'
import { DefaultSubscribeScript } from '@/constant/app'
import { DefaultExcludeProtocols } from '@/constant/kernel'
import { RequestMethod } from '@/enums/app'
import { sampleID } from '@/utils'
import { SubscriptionService } from '../../gen/app/v1/subscription_service_pb'
import {
  applyMutationState,
  applyResourceSnapshot,
  createLocalResourceState,
  expectedItemRevision,
  expectedOrderRevision,
} from './resourceSync'

import type { Subscription } from '@/types/app'
import type { ExpectedRevision } from '../../gen/common/v1/sync_pb'
import type { TaskResult } from '../../gen/app/v1/task_pb'

type Revision = Pick<ExpectedRevision, 'instanceId' | 'revision'>

const parseSubscription = (value: string): Subscription => {
  const subscription = JSON.parse(value) as Subscription
  subscription.proxies ??= []
  return subscription
}

const parseSubscriptions = (items: string[]) => items.map(parseSubscription)

export const useSubscribesStore = defineStore('subscribes', () => {
  const subscribes = ref<Subscription[]>([])
  const resourceState = reactive(createLocalResourceState())
  const service = createRpcClient(SubscriptionService)
  let setupRequestID = 0
  let latestAppliedSetupRequestID = 0

  const replaceSubscribes = (items: Subscription[]) => {
    subscribes.value.splice(0, subscribes.value.length, ...items)
  }

  const upsertLocalSubscribe = (item: Subscription) => {
    const idx = subscribes.value.findIndex((value) => value.id === item.id)
    if (idx === -1) subscribes.value.push(item)
    else subscribes.value.splice(idx, 1, item)
  }

  const setupSubscribes = async () => {
    const requestID = ++setupRequestID
    const { subscriptionsJson, state } = await service.listSubscriptions({})
    const items = parseSubscriptions(subscriptionsJson)
    if (
      state?.instanceId &&
      resourceState.instanceId &&
      state.instanceId !== resourceState.instanceId &&
      requestID < latestAppliedSetupRequestID
    ) {
      return
    }
    if (applyResourceSnapshot(resourceState, state)) {
      latestAppliedSetupRequestID = Math.max(latestAppliedSetupRequestID, requestID)
      replaceSubscribes(items)
    }
  }

  const applySubscribeMutation = async (
    state: Parameters<typeof applyMutationState>[1],
    options: { id?: string; deleted?: boolean } = {},
  ) => {
    if (applyMutationState(resourceState, state, options)) return true
    if (state?.instanceId && state.instanceId !== resourceState.instanceId) {
      await setupSubscribes()
    }
    return false
  }

  const reorderSubscribes = async (
    ids: string[],
    revision: Revision = expectedOrderRevision(resourceState),
    fallbackIDs: string[] = [],
  ) => {
    try {
      const { ids: orderedIDs, state } = await service.reorderSubscriptions({
        ids,
        expectedOrderRevision: revision,
      })
      if (!(await applySubscribeMutation(state))) return
      const byID = new Map(subscribes.value.map((item) => [item.id, item]))
      const ordered = orderedIDs.flatMap((id) => byID.get(id) || [])
      if (ordered.length !== subscribes.value.length) {
        await setupSubscribes()
        return
      }
      replaceSubscribes(ordered)
    } catch (error) {
      try {
        await setupSubscribes()
      } catch {
        const byID = new Map(subscribes.value.map((item) => [item.id, item]))
        const fallback = fallbackIDs.flatMap((id) => byID.get(id) || [])
        if (fallback.length === subscribes.value.length) replaceSubscribes(fallback)
      }
      throw error
    }
  }

  const addSubscribe = async (subscription: Subscription) => {
    const { subscriptionJson, state } = await service.createSubscription({
      subscriptionJson: JSON.stringify(subscription),
    })
    if (!(await applySubscribeMutation(state, { id: subscription.id }))) return
    upsertLocalSubscribe(parseSubscription(subscriptionJson))
  }

  const importSubscribe = async (name: string, url: string) => {
    await addSubscribe(getSubscribeTemplate(name, { url }))
  }

  const deleteSubscribe = async (
    id: string,
    revision: Revision = expectedItemRevision(resourceState, id),
  ) => {
    const { state } = await service.deleteSubscription({ id, expectedRevision: revision })
    if (!(await applySubscribeMutation(state, { id, deleted: true }))) return
    const idx = subscribes.value.findIndex((value) => value.id === id)
    if (idx !== -1) subscribes.value.splice(idx, 1)
  }

  const editSubscribe = async (
    id: string,
    subscription: Subscription,
    revision: Revision = expectedItemRevision(resourceState, id),
  ) => {
    const { subscriptionJson, state } = await service.updateSubscriptionConfig({
      subscriptionJson: JSON.stringify({ ...subscription, id }),
      expectedRevision: revision,
    })
    if (!(await applySubscribeMutation(state, { id }))) return
    upsertLocalSubscribe(parseSubscription(subscriptionJson))
  }

  const updateSubscribe = async (id: string): Promise<TaskResult[]> => {
    const subscription = subscribes.value.find((value) => value.id === id)
    if (subscription) subscription.updating = true
    try {
      const { results, state } = await service.updateSubscription({ id })
      await applySubscribeMutation(state)
      await setupSubscribes()
      return results
    } finally {
      const next = subscribes.value.find((value) => value.id === id)
      if (next) next.updating = false
    }
  }

  const updateSubscribes = async (): Promise<TaskResult[]> => {
    subscribes.value.forEach((value) => !value.disabled && (value.updating = true))
    try {
      const { results, state } = await service.updateAllSubscriptions({})
      await applySubscribeMutation(state)
      await setupSubscribes()
      return results
    } finally {
      subscribes.value.forEach((value) => (value.updating = false))
    }
  }

  const getSubscribeById = (id: string) => subscribes.value.find((value) => value.id === id)

  const getSubscriptionContentWithRevision = async (id: string) => {
    const { content, revision } = await service.getSubscriptionContent({ id })
    return { content, revision }
  }

  const getSubscriptionContent = async (id: string) =>
    (await getSubscriptionContentWithRevision(id)).content

  const saveSubscriptionContent = async (
    id: string,
    content: string,
    proxyIds: string[],
    revision: Revision = expectedItemRevision(resourceState, id),
  ) => {
    const { subscriptionJson, state } = await service.saveSubscriptionContent({
      id,
      content,
      proxyIds,
      expectedRevision: revision,
    })
    if (!(await applySubscribeMutation(state, { id }))) return
    const item = parseSubscription(subscriptionJson)
    upsertLocalSubscribe(item)
    return item
  }

  const getSubscribeRevision = (id: string) => expectedItemRevision(resourceState, id)
  const getSubscribesOrderRevision = () => expectedOrderRevision(resourceState)

  const getSubscribeTemplate = (name = '', options: { url?: string } = {}): Subscription => ({
    id: sampleID(),
    name,
    upload: 0,
    download: 0,
    total: 0,
    expire: 0,
    updateTime: 0,
    type: 'Http',
    url: options.url || '',
    website: '',
    include: '',
    exclude: '',
    includeProtocol: '',
    excludeProtocol: DefaultExcludeProtocols,
    proxyPrefix: '',
    disabled: false,
    inSecure: false,
    enableNodeConversion: true,
    requestMethod: RequestMethod.Get,
    requestTimeout: 15,
    header: { request: {}, response: {} },
    proxies: [],
    script: DefaultSubscribeScript,
  })

  return {
    subscribes,
    resourceState,
    setupSubscribes,
    reorderSubscribes,
    addSubscribe,
    editSubscribe,
    deleteSubscribe,
    updateSubscribe,
    updateSubscribes,
    getSubscribeById,
    getSubscribeRevision,
    getSubscribesOrderRevision,
    getSubscriptionContent,
    getSubscriptionContentWithRevision,
    saveSubscriptionContent,
    importSubscribe,
    getSubscribeTemplate,
  }
})
