import {
  closeAllConnections,
  closeConnection,
  getFreshConnectionsSnapshot,
  getNativeApiGeneration,
  selectOutbound,
  setClashMode,
} from '@/api/kernel'
import { RulesetFormat } from '@/enums/kernel'
import type { Group, GroupItem } from '@/types/kernel'
import { useAppSettingsStore, useKernelApiStore, useRulesetsStore } from '@/stores'
import { changeModeThenClose, queueKernelAction, selectThenClose } from './nativeKernelActions'
import { sameKernelMode } from './nativeKernelUi'

export const getZoomLevel = () => {
  const el = document.querySelector('.app-zoomed') as HTMLElement | null
  if (!el) return 1
  const style = getComputedStyle(el)
  return parseFloat(style.zoom) || 1
}

export const GetKernelProxy = async () => {
  if (useKernelApiStore().running) {
    const kernelProxy = useKernelApiStore().getProxyPort()
    if (kernelProxy !== undefined) {
      if (kernelProxy.proxyType === 'socks') {
        return `socks5://127.0.0.1:${kernelProxy.port}`
      }
      return `http://127.0.0.1:${kernelProxy.port}`
    }
  }

  return ''
}

// Others
const guardKernelInstance = (generation: number) => {
  if (generation !== getNativeApiGeneration()) throw new Error('Kernel instance changed')
}

export const handleUseProxy = (group: Group, proxy: GroupItem) => {
  const generation = getNativeApiGeneration()
  return queueKernelAction(`group:${group.tag}`, async () => {
    guardKernelInstance(generation)
    const kernelApiStore = useKernelApiStore()
    const current = kernelApiStore.groups.find((item) => item.tag === group.tag)
    if (!current?.selectable || current.selected === proxy.tag) return
    await selectThenClose(group.tag, proxy.tag, useAppSettingsStore().app.kernel.autoClose, {
      getSnapshot: getFreshConnectionsSnapshot,
      select: (tag, outbound) => {
        guardKernelInstance(generation)
        return selectOutbound(tag, outbound)
      },
      close: (id) => {
        guardKernelInstance(generation)
        return closeConnection(id)
      },
    })
    guardKernelInstance(generation)
    await kernelApiStore.refreshProviderProxies()
  })
}

export const handleChangeMode = (mode: string) => {
  const generation = getNativeApiGeneration()
  return queueKernelAction('mode', async () => {
    guardKernelInstance(generation)
    const kernelApiStore = useKernelApiStore()
    if (sameKernelMode(mode, kernelApiStore.config.mode)) return
    await changeModeThenClose(mode, {
      setMode: async (value) => {
        const status = await setClashMode(value)
        guardKernelInstance(generation)
        kernelApiStore.config.mode = status.currentMode
        kernelApiStore.config.modeList = status.modeList
        return status
      },
      closeAll: () => {
        guardKernelInstance(generation)
        return closeAllConnections()
      },
    })
    guardKernelInstance(generation)
    await kernelApiStore.refreshConfig()
  })
}

export const addToRuleSet = async (
  id: 'direct' | 'reject' | 'proxy',
  payloads: Record<string, any>[],
) => {
  const rulesetsStoe = useRulesetsStore()
  await rulesetsStoe.setupRulesets()

  let ruleset = rulesetsStoe.rulesets.find(
    (r) => r.tag === id && r.type === 'Manual' && r.format === RulesetFormat.Source,
  )
  if (!ruleset) {
    ruleset = {
      id,
      tag: id,
      updateTime: 0,
      type: 'Manual',
      format: RulesetFormat.Source,
      url: '',
      path: '',
      count: 0,
      disabled: false,
    }
    await rulesetsStoe.addRuleset(ruleset)
  }

  const { content: storedContent, revision } = await rulesetsStoe.getRulesetContentWithRevision(
    ruleset.id,
  )
  const content = storedContent || '{ "version": 2, "rules": [] }'
  const { rules = [] } = JSON.parse(content)
  rules[0] = rules[0] || {}
  payloads.forEach((payload) => {
    if (payload.domain) {
      rules[0].domain = [...new Set((rules[0].domain || []).concat(payload.domain))]
    } else if (payload.ip_cidr) {
      rules[0].ip_cidr = [...new Set((rules[0].ip_cidr || []).concat(payload.ip_cidr))]
    } else if (payload.process_path) {
      rules[0].process_path = [
        ...new Set((rules[0].process_path || []).concat(payload.process_path)),
      ]
    } else if (payload.domain_suffix) {
      rules[0].domain_suffix = [
        ...new Set((rules[0].domain_suffix || []).concat(payload.domain_suffix)),
      ]
    }
  })
  await rulesetsStoe.saveRulesetContent(
    ruleset.id,
    JSON.stringify({ version: 2, rules }, null, 2),
    revision,
  )
}
