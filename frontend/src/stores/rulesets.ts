import { defineStore } from 'pinia'
import { reactive, ref } from 'vue'

import { createRpcClient } from '@/bridge'
import { RuleSetService } from '../../gen/app/v1/rule_set_service_pb'
import {
  applyMutationState,
  applyResourceSnapshot,
  createLocalResourceState,
  expectedItemRevision,
  expectedOrderRevision,
} from './resourceSync'

import type { RulesetFormat } from '@/enums/kernel'
import type { ExpectedRevision } from '../../gen/common/v1/sync_pb'
import type { TaskResult } from '../../gen/app/v1/task_pb'

type Revision = Pick<ExpectedRevision, 'instanceId' | 'revision'>

export interface RuleSet {
  id: string
  tag: string
  updateTime: number
  disabled: boolean
  type: 'Http' | 'Manual'
  format: RulesetFormat
  path: string
  url: string
  count: number
  updating?: boolean
}

export interface RuleSetHub {
  geosite: string
  geoip: string
  list: { name: string; type: 'geosite' | 'geoip'; description: string; count: number }[]
}

const parseList = <T>(items: string[]) => items.map((v) => JSON.parse(v) as T)
const emptyHub = (): RuleSetHub => ({ geosite: '', geoip: '', list: [] })

export const useRulesetsStore = defineStore('rulesets', () => {
  const rulesets = ref<RuleSet[]>([])
  const rulesetHub = ref<RuleSetHub>(emptyHub())
  const rulesetHubLoading = ref(false)
  const resourceState = reactive(createLocalResourceState())
  const service = createRpcClient(RuleSetService)
  let setupRequestID = 0
  let latestAppliedSetupRequestID = 0

  const replaceRulesets = (items: RuleSet[]) => {
    rulesets.value.splice(0, rulesets.value.length, ...items)
  }

  const upsertRuleset = (item: RuleSet) => {
    const idx = rulesets.value.findIndex((value) => value.id === item.id)
    if (idx === -1) rulesets.value.push(item)
    else rulesets.value.splice(idx, 1, item)
  }

  const setupRulesets = async () => {
    const requestID = ++setupRequestID
    const { rulesetsJson, hubJson, state } = await service.listRuleSets({})
    const items = parseList<RuleSet>(rulesetsJson)
    const hub = hubJson ? JSON.parse(hubJson) : emptyHub()
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
      replaceRulesets(items)
      rulesetHub.value = hub
    }
  }

  const applyRulesetMutation = async (
    state: Parameters<typeof applyMutationState>[1],
    options: { id?: string; deleted?: boolean } = {},
  ) => {
    if (applyMutationState(resourceState, state, options)) return true
    if (state?.instanceId && state.instanceId !== resourceState.instanceId) {
      await setupRulesets()
    }
    return false
  }

  const reorderRulesets = async (
    ids: string[],
    revision: Revision = expectedOrderRevision(resourceState),
    fallbackIDs: string[] = [],
  ) => {
    try {
      const { ids: orderedIDs, state } = await service.reorderRuleSets({
        ids,
        expectedOrderRevision: revision,
      })
      if (!(await applyRulesetMutation(state))) return
      const byId = new Map(rulesets.value.map((item) => [item.id, item]))
      const ordered = orderedIDs.flatMap((id) => byId.get(id) || [])
      if (ordered.length !== rulesets.value.length) {
        await setupRulesets()
        return
      }
      replaceRulesets(ordered)
    } catch (error) {
      try {
        await setupRulesets()
      } catch {
        const byId = new Map(rulesets.value.map((item) => [item.id, item]))
        const fallback = fallbackIDs.flatMap((id) => byId.get(id) || [])
        if (fallback.length === rulesets.value.length) replaceRulesets(fallback)
      }
      throw error
    }
  }

  const addRuleset = async (r: RuleSet) => {
    const { rulesetJson, state } = await service.createRuleSet({
      rulesetJson: JSON.stringify(r),
    })
    if (!(await applyRulesetMutation(state, { id: r.id }))) return
    upsertRuleset(JSON.parse(rulesetJson))
  }

  const deleteRuleset = async (
    id: string,
    revision: Revision = expectedItemRevision(resourceState, id),
  ) => {
    const { state } = await service.deleteRuleSet({ id, expectedRevision: revision })
    if (!(await applyRulesetMutation(state, { id, deleted: true }))) return
    const idx = rulesets.value.findIndex((v) => v.id === id)
    if (idx !== -1) rulesets.value.splice(idx, 1)
  }

  const editRuleset = async (
    id: string,
    r: RuleSet,
    revision: Revision = expectedItemRevision(resourceState, id),
  ) => {
    const { rulesetJson, state } = await service.updateRuleSetConfig({
      rulesetJson: JSON.stringify({ ...r, id }),
      expectedRevision: revision,
    })
    if (!(await applyRulesetMutation(state, { id }))) return
    upsertRuleset(JSON.parse(rulesetJson) as RuleSet)
  }

  const updateRuleset = async (id: string): Promise<TaskResult[]> => {
    const r = rulesets.value.find((v) => v.id === id)
    if (r) r.updating = true
    try {
      const { results, state } = await service.updateRuleSet({ id })
      await applyRulesetMutation(state)
      await setupRulesets()
      return results
    } finally {
      const next = rulesets.value.find((v) => v.id === id)
      if (next) next.updating = false
    }
  }

  const updateRulesets = async (): Promise<TaskResult[]> => {
    rulesets.value.forEach((v) => !v.disabled && (v.updating = true))
    try {
      const { results, state } = await service.updateAllRuleSets({})
      await applyRulesetMutation(state)
      await setupRulesets()
      return results
    } finally {
      rulesets.value.forEach((v) => (v.updating = false))
    }
  }

  const updateRulesetHub = async () => {
    rulesetHubLoading.value = true
    try {
      const { hubJson, state } = await service.updateRuleSetHub({})
      if (await applyRulesetMutation(state)) {
        rulesetHub.value = hubJson ? JSON.parse(hubJson) : emptyHub()
      }
    } finally {
      rulesetHubLoading.value = false
    }
  }

  const previewRuleSetHub = async (index: number, format: RulesetFormat) => {
    const { content } = await service.previewRuleSetHub({ index, format })
    return content
  }

  const getRulesetContentWithRevision = async (id: string) => {
    const { content, revision } = await service.getRuleSetContent({ id })
    return { content, revision }
  }

  const getRulesetContent = async (id: string) => (await getRulesetContentWithRevision(id)).content

  const saveRulesetContent = async (
    id: string,
    content: string,
    revision: Revision = expectedItemRevision(resourceState, id),
  ) => {
    const { rulesetJson, state } = await service.saveRuleSetContent({
      id,
      content,
      expectedRevision: revision,
    })
    if (!(await applyRulesetMutation(state, { id }))) return
    upsertRuleset(JSON.parse(rulesetJson) as RuleSet)
  }

  const clearRulesetContent = async (
    id: string,
    revision: Revision = expectedItemRevision(resourceState, id),
  ) => {
    const { rulesetJson, state } = await service.clearRuleSetContent({
      id,
      expectedRevision: revision,
    })
    if (!(await applyRulesetMutation(state, { id }))) return
    upsertRuleset(JSON.parse(rulesetJson) as RuleSet)
  }

  const getRulesetById = (id: string) => rulesets.value.find((v) => v.id === id)
  const getRulesetRevision = (id: string) => expectedItemRevision(resourceState, id)
  const getRulesetsOrderRevision = () => expectedOrderRevision(resourceState)

  return {
    rulesets,
    resourceState,
    setupRulesets,
    reorderRulesets,
    addRuleset,
    editRuleset,
    deleteRuleset,
    updateRuleset,
    updateRulesets,
    getRulesetById,
    getRulesetRevision,
    getRulesetsOrderRevision,
    getRulesetContent,
    getRulesetContentWithRevision,
    saveRulesetContent,
    clearRulesetContent,
    rulesetHub,
    rulesetHubLoading,
    updateRulesetHub,
    previewRuleSetHub,
  }
})
