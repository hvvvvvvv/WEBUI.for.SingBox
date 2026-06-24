import { defineStore } from 'pinia'
import { ref } from 'vue'

import { createRpcClient } from '@/bridge'
import { eventBus } from '@/utils'
import { RuleSetService } from '../../gen/app/v1/rule_set_service_pb'

import type { RulesetFormat } from '@/enums/kernel'
import type { TaskResult } from '../../gen/app/v1/task_pb'

export interface RuleSet {
  id: string
  tag: string
  updateTime: number
  disabled: boolean
  type: 'Http' | 'File' | 'Manual'
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
  const service = createRpcClient(RuleSetService)

  const setupRulesets = async () => {
    const { rulesetsJson, hubJson } = await service.listRuleSets({})
    rulesets.value = parseList<RuleSet>(rulesetsJson)
    rulesetHub.value = hubJson ? JSON.parse(hubJson) : emptyHub()
  }

  const saveRulesets = async () => {
    const { rulesetsJson } = await service.saveRuleSets({
      rulesetsJson: rulesets.value.map((v) => JSON.stringify(v)),
    })
    rulesets.value = parseList<RuleSet>(rulesetsJson)
    eventBus.emit('rulesetsChange', undefined)
  }

  const addRuleset = async (r: RuleSet) => {
    const { rulesetJson } = await service.upsertRuleSet({ rulesetJson: JSON.stringify(r) })
    rulesets.value.push(JSON.parse(rulesetJson))
    eventBus.emit('rulesetChange', { id: r.id })
  }

  const deleteRuleset = async (id: string) => {
    await service.deleteRuleSet({ id })
    const idx = rulesets.value.findIndex((v) => v.id === id)
    idx !== -1 && rulesets.value.splice(idx, 1)
    eventBus.emit('rulesetChange', { id })
  }

  const editRuleset = async (id: string, r: RuleSet) => {
    const { rulesetJson } = await service.upsertRuleSet({
      rulesetJson: JSON.stringify({ ...r, id }),
    })
    const item = JSON.parse(rulesetJson) as RuleSet
    const idx = rulesets.value.findIndex((v) => v.id === id)
    if (idx === -1) {
      rulesets.value.push(item)
    } else {
      rulesets.value.splice(idx, 1, item)
    }
    eventBus.emit('rulesetChange', { id })
  }

  const updateRuleset = async (id: string): Promise<TaskResult[]> => {
    const r = rulesets.value.find((v) => v.id === id)
    if (r) r.updating = true
    try {
      const { results } = await service.updateRuleSet({ id })
      await setupRulesets()
      eventBus.emit('rulesetChange', { id })
      return results
    } finally {
      const next = rulesets.value.find((v) => v.id === id)
      if (next) next.updating = false
    }
  }

  const updateRulesets = async (): Promise<TaskResult[]> => {
    rulesets.value.forEach((v) => !v.disabled && (v.updating = true))
    try {
      const { results } = await service.updateAllRuleSets({})
      await setupRulesets()
      eventBus.emit('rulesetsChange', undefined)
      return results
    } finally {
      rulesets.value.forEach((v) => (v.updating = false))
    }
  }

  const updateRulesetHub = async () => {
    rulesetHubLoading.value = true
    try {
      const { hubJson } = await service.updateRuleSetHub({})
      rulesetHub.value = hubJson ? JSON.parse(hubJson) : emptyHub()
    } finally {
      rulesetHubLoading.value = false
    }
  }

  const getRulesetContent = async (id: string) => {
    const { content } = await service.getRuleSetContent({ id })
    return content
  }

  const saveRulesetContent = async (id: string, content: string) => {
    const { rulesetJson } = await service.saveRuleSetContent({ id, content })
    const item = JSON.parse(rulesetJson) as RuleSet
    const idx = rulesets.value.findIndex((v) => v.id === id)
    if (idx === -1) {
      rulesets.value.push(item)
    } else {
      rulesets.value.splice(idx, 1, item)
    }
    eventBus.emit('rulesetChange', { id })
  }

  const clearRulesetContent = async (id: string) => {
    const { rulesetJson } = await service.clearRuleSetContent({ id })
    const item = JSON.parse(rulesetJson) as RuleSet
    const idx = rulesets.value.findIndex((v) => v.id === id)
    if (idx !== -1) {
      rulesets.value.splice(idx, 1, item)
    }
    eventBus.emit('rulesetChange', { id })
  }

  const getRulesetById = (id: string) => rulesets.value.find((v) => v.id === id)

  return {
    rulesets,
    setupRulesets,
    saveRulesets,
    addRuleset,
    editRuleset,
    deleteRuleset,
    updateRuleset,
    updateRulesets,
    getRulesetById,
    getRulesetContent,
    saveRulesetContent,
    clearRulesetContent,

    rulesetHub,
    rulesetHubLoading,
    updateRulesetHub,
  }
})
