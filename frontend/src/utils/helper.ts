import { deleteConnection, getConnections, useProxy } from '@/api/kernel'
import { RulesetFormat } from '@/enums/kernel'
import { useAppSettingsStore, useKernelApiStore, useRulesetsStore } from '@/stores'

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
export const handleUseProxy = async (group: any, proxy: any) => {
  if (group.type !== 'Selector' || group.now === proxy.name) return
  const promises: Promise<null>[] = []
  const appSettings = useAppSettingsStore()
  const kernelApiStore = useKernelApiStore()
  if (appSettings.app.kernel.autoClose) {
    const { connections } = await getConnections()
    promises.push(
      ...(connections || [])
        .filter((v) => v.chains.includes(group.name))
        .map((v) => deleteConnection(v.id)),
    )
  }
  await useProxy(encodeURIComponent(group.name), proxy.name)
  await Promise.all(promises)
  await kernelApiStore.refreshProviderProxies()
}

export const handleChangeMode = async (mode: 'direct' | 'global' | 'rule') => {
  const kernelApiStore = useKernelApiStore()

  if (mode === kernelApiStore.config.mode) return

  kernelApiStore.updateConfig('mode', mode)

  const { connections } = await getConnections()
  const promises = (connections || []).map((v) => deleteConnection(v.id))
  await Promise.all(promises)
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
