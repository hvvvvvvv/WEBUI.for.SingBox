import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { parse } from 'yaml'

import { ReadFile, WriteFile } from '@/bridge'
import { ProfilesFilePath } from '@/constant/app'
import * as Defaults from '@/constant/profile'
import { useAppSettingsStore } from '@/stores'
import { ignoredError, eventBus, stringifyNoFolding, migrateProfiles, sampleID } from '@/utils'

export const useProfilesStore = defineStore('profiles', () => {
  const appSettingsStore = useAppSettingsStore()

  const profiles = ref<IProfile[]>([])
  const currentProfile = computed(() => getProfileById(appSettingsStore.app.kernel.profile))

  const setupProfiles = async () => {
    const data = await ignoredError(ReadFile, ProfilesFilePath)
    if (data) {
      const loaded = parse(data)
      const list = Array.isArray(loaded) ? loaded : []
      profiles.value = list.map((item) => normalizeLoadedProfile(item))
    }

    await migrateProfiles(profiles.value, saveProfiles)
  }

  const saveProfiles = () => {
    return WriteFile(ProfilesFilePath, stringifyNoFolding(profiles.value))
  }

  const addProfile = async (p: IProfile) => {
    profiles.value.push(p)
    try {
      await saveProfiles()
    } catch (error) {
      const idx = profiles.value.indexOf(p)
      if (idx !== -1) {
        profiles.value.splice(idx, 1)
      }
      throw error
    }
  }

  const deleteProfile = async (id: string) => {
    const idx = profiles.value.findIndex((v) => v.id === id)
    if (idx === -1) return
    const backup = profiles.value.splice(idx, 1)[0]!
    try {
      await saveProfiles()
    } catch (error) {
      profiles.value.splice(idx, 0, backup)
      throw error
    }

    eventBus.emit('profileChange', { id })
  }

  const editProfile = async (id: string, p: IProfile) => {
    const idx = profiles.value.findIndex((v) => v.id === id)
    if (idx === -1) return
    const backup = profiles.value.splice(idx, 1, p)[0]!
    try {
      await saveProfiles()
    } catch (error) {
      profiles.value.splice(idx, 1, backup)
      throw error
    }

    eventBus.emit('profileChange', { id })
  }

  const getProfileById = (id: string) => profiles.value.find((v) => v.id === id)

  const getProfileTemplate = (name = ''): IProfile => {
    return {
      id: sampleID(),
      name: name,
      log: Defaults.DefaultLog(),
      experimental: Defaults.DefaultExperimental(),
      inbounds: Defaults.DefaultInbounds(),
      outbounds: Defaults.DefaultOutbounds(),
      route: Defaults.DefaultRoute(),
      dns: Defaults.DefaultDns(),
      mixin: Defaults.DefaultMixin(),
      script: Defaults.DefaultScript(),
    }
  }

  return {
    profiles,
    currentProfile,
    setupProfiles,
    saveProfiles,
    addProfile,
    editProfile,
    deleteProfile,
    getProfileById,
    getProfileTemplate,
  }
})

const normalizeLoadedProfile = (raw: any): IProfile => {
  const template: IProfile = {
    id: sampleID(),
    name: '',
    log: Defaults.DefaultLog(),
    experimental: Defaults.DefaultExperimental(),
    inbounds: Defaults.DefaultInbounds(),
    outbounds: Defaults.DefaultOutbounds(),
    route: Defaults.DefaultRoute(),
    dns: Defaults.DefaultDns(),
    mixin: Defaults.DefaultMixin(),
    script: Defaults.DefaultScript(),
  }

  const profile: any = {
    ...template,
    ...raw,
    log: { ...template.log, ...(raw?.log || {}) },
    experimental: normalizeExperimental(raw?.experimental, template.experimental),
    inbounds: normalizeInbounds(raw?.inbounds, template.inbounds),
    outbounds: normalizeOutbounds(raw?.outbounds, template.outbounds),
    route: normalizeRoute(raw?.route, template.route),
    dns: normalizeDns(raw?.dns, template.dns),
    mixin: normalizeMixin(raw?.mixin, template.mixin),
    script: normalizeScript(raw?.script, template.script),
  }

  profile.log.level = normalizeLogLevel(profile.log.level)
  return profile as IProfile
}

const normalizeExperimental = (exp: any, fallback: IExperimental): IExperimental => {
  const clashApiRaw = exp?.clash_api || exp?.clashapi || {}
  const cacheFileRaw = exp?.cache_file || exp?.cachefile || {}

  return {
    clash_api: {
      ...fallback.clash_api,
      ...clashApiRaw,
      external_controller: clashApiRaw.external_controller ?? clashApiRaw.externalcontroller ?? fallback.clash_api.external_controller,
      external_ui: clashApiRaw.external_ui ?? clashApiRaw.externalui ?? fallback.clash_api.external_ui,
      external_ui_download_url:
        clashApiRaw.external_ui_download_url ??
        clashApiRaw.externaluidownloadurl ??
        fallback.clash_api.external_ui_download_url,
      external_ui_download_detour:
        clashApiRaw.external_ui_download_detour ??
        clashApiRaw.externaluidownloaddetour ??
        fallback.clash_api.external_ui_download_detour,
      default_mode: clashApiRaw.default_mode ?? clashApiRaw.defaultmode ?? fallback.clash_api.default_mode,
      access_control_allow_origin:
        clashApiRaw.access_control_allow_origin ??
        clashApiRaw.accesscontrolalloworigin ??
        fallback.clash_api.access_control_allow_origin,
      access_control_allow_private_network:
        clashApiRaw.access_control_allow_private_network ??
        clashApiRaw.accesscontrolallowprivatenetwork ??
        fallback.clash_api.access_control_allow_private_network,
    },
    cache_file: {
      ...fallback.cache_file,
      ...cacheFileRaw,
      cache_id: cacheFileRaw.cache_id ?? cacheFileRaw.cacheid ?? fallback.cache_file.cache_id,
      store_fakeip: cacheFileRaw.store_fakeip ?? cacheFileRaw.storefakeip ?? fallback.cache_file.store_fakeip,
      store_rdrc: cacheFileRaw.store_rdrc ?? cacheFileRaw.storerdrc ?? fallback.cache_file.store_rdrc,
      rdrc_timeout: cacheFileRaw.rdrc_timeout ?? cacheFileRaw.rdrctimeout ?? fallback.cache_file.rdrc_timeout,
    },
  }
}

const normalizeInbounds = (inbounds: any[], fallback: IInbound[]): IInbound[] => {
  if (!Array.isArray(inbounds)) return fallback
  return inbounds.map((item) => {
    const type = normalizeInboundType(item?.type)
    const mixed = item?.mixed || {}
    const socks = item?.socks || {}
    const http = item?.http || {}
    const tun = item?.tun || {}

    return {
      ...item,
      type,
      mixed: normalizeInboundUser(mixed),
      socks: normalizeInboundUser(socks),
      http: normalizeInboundUser(http),
      tun: tun
        ? {
          ...tun,
          interface_name: tun.interface_name ?? tun.interfacename ?? '',
          auto_route: tun.auto_route ?? tun.autoroute ?? false,
          strict_route: tun.strict_route ?? tun.strictroute ?? false,
          route_address: tun.route_address ?? tun.routeaddress ?? [],
          route_exclude_address: tun.route_exclude_address ?? tun.routeexcludeaddress ?? [],
          endpoint_independent_nat:
            tun.endpoint_independent_nat ?? tun.endpointindependentnat ?? false,
          stack: normalizeTunStack(tun.stack),
        }
        : undefined,
    }
  })
}

const normalizeInboundUser = (raw: any) => {
  if (!raw || typeof raw !== 'object') return raw
  const listen = raw.listen || {}
  return {
    ...raw,
    listen: {
      ...listen,
      listen_port: listen.listen_port ?? listen.listenport ?? 0,
      tcp_fast_open: listen.tcp_fast_open ?? listen.tcpfastopen ?? false,
      tcp_multi_path: listen.tcp_multi_path ?? listen.tcpmultipath ?? false,
      udp_fragment: listen.udp_fragment ?? listen.udpfragment ?? false,
    },
  }
}

const normalizeOutbounds = (outbounds: any[], fallback: IOutbound[]): IOutbound[] => {
  if (!Array.isArray(outbounds)) return fallback
  return outbounds.map((item) => ({
    ...item,
    type: normalizeOutboundType(item?.type),
    interrupt_exist_connections:
      item?.interrupt_exist_connections ?? item?.interruptexistconnections ?? false,
  }))
}

const normalizeRoute = (route: any, fallback: IRoute): IRoute => {
  const r = route || {}
  const ruleSet = r.rule_set || r.ruleset || []
  return {
    ...fallback,
    ...r,
    rule_set: Array.isArray(ruleSet)
      ? ruleSet.map((item: any) => ({
        ...item,
        type: normalizeRulesetType(item?.type),
        format: normalizeRulesetFormat(item?.format),
        download_detour: item?.download_detour ?? item?.downloaddetour ?? '',
        update_interval: item?.update_interval ?? item?.updateinterval ?? '',
      }))
      : fallback.rule_set,
    rules: Array.isArray(r.rules)
      ? r.rules.map((item: any) => ({
        ...item,
        type: normalizeRuleType(item?.type),
        action: normalizeRuleAction(item?.action),
        strategy: normalizeStrategy(item?.strategy),
      }))
      : fallback.rules,
    auto_detect_interface: r.auto_detect_interface ?? r.autodetectinterface ?? fallback.auto_detect_interface,
    default_interface: r.default_interface ?? r.defaultinterface ?? fallback.default_interface,
    find_process: r.find_process ?? r.findprocess ?? fallback.find_process,
    default_domain_resolver: {
      ...fallback.default_domain_resolver,
      ...(r.default_domain_resolver || r.defaultdomainresolver || {}),
      client_subnet:
        (r.default_domain_resolver || r.defaultdomainresolver || {}).client_subnet ??
        (r.default_domain_resolver || r.defaultdomainresolver || {}).clientsubnet ??
        fallback.default_domain_resolver.client_subnet,
    },
  }
}

const normalizeDns = (dns: any, fallback: IDNS): IDNS => {
  const d = dns || {}
  return {
    ...fallback,
    ...d,
    disable_cache: d.disable_cache ?? d.disablecache ?? fallback.disable_cache,
    disable_expire: d.disable_expire ?? d.disableexpire ?? fallback.disable_expire,
    independent_cache: d.independent_cache ?? d.independentcache ?? fallback.independent_cache,
    client_subnet: d.client_subnet ?? d.clientsubnet ?? fallback.client_subnet,
    strategy: normalizeStrategy(d.strategy),
    servers: Array.isArray(d.servers)
      ? d.servers.map((item: any) => ({
        ...item,
        type: normalizeDnsServerType(item?.type),
        domain_resolver: item?.domain_resolver ?? item?.domainresolver ?? '',
        hosts_path: item?.hosts_path ?? item?.hostspath ?? [],
        server_port: item?.server_port ?? item?.serverport ?? '',
        inet4_range: item?.inet4_range ?? item?.inet4range ?? '',
        inet6_range: item?.inet6_range ?? item?.inet6range ?? '',
      }))
      : fallback.servers,
    rules: Array.isArray(d.rules)
      ? d.rules.map((item: any) => ({
        ...item,
        type: normalizeRuleType(item?.type),
        action: normalizeDnsRuleAction(item?.action),
        strategy: normalizeStrategy(item?.strategy),
        disable_cache: item?.disable_cache ?? item?.disablecache ?? false,
        client_subnet: item?.client_subnet ?? item?.clientsubnet ?? '',
      }))
      : fallback.rules,
  }
}

const normalizeMixin = (mixin: any, fallback: IMixin): IMixin => ({
  ...fallback,
  ...(mixin || {}),
  priority: normalizeMixinPriority(mixin?.priority),
  format: normalizeMixinFormat(mixin?.format),
})

const normalizeScript = (script: any, fallback: IScript): IScript => ({
  ...fallback,
  ...(script || {}),
})

const mapEnum = (value: any, mapping: Record<number, string>, fallback: string) => {
  if (typeof value === 'number' && mapping[value]) return mapping[value]
  if (typeof value === 'string' && value !== '') return value
  return fallback
}

const normalizeLogLevel = (v: any) =>
  mapEnum(v, { 1: 'trace', 2: 'debug', 3: 'info', 4: 'warn', 5: 'error', 6: 'fatal', 7: 'panic' }, 'info')
const normalizeInboundType = (v: any) =>
  mapEnum(v, { 1: 'mixed', 2: 'socks', 3: 'http', 4: 'tun' }, 'mixed')
const normalizeOutboundType = (v: any) =>
  mapEnum(v, { 1: 'direct', 2: 'block', 3: 'selector', 4: 'urltest' }, 'selector')
const normalizeTunStack = (v: any) => mapEnum(v, { 1: 'system', 2: 'gvisor', 3: 'mixed' }, 'mixed')
const normalizeRulesetType = (v: any) => mapEnum(v, { 1: 'inline', 2: 'local', 3: 'remote' }, 'inline')
const normalizeRulesetFormat = (v: any) => mapEnum(v, { 1: 'source', 2: 'binary' }, 'source')
const normalizeRuleAction = (v: any) =>
  mapEnum(v, { 1: 'route', 2: 'route-options', 3: 'reject', 4: 'hijack-dns', 5: 'sniff', 6: 'resolve' }, 'route')
const normalizeDnsRuleAction = (v: any) =>
  mapEnum(v, { 1: 'route', 2: 'route-options', 3: 'reject', 4: 'predefined' }, 'route')
const normalizeStrategy = (v: any) =>
  mapEnum(v, { 1: 'default', 2: 'prefer_ipv4', 3: 'prefer_ipv6', 4: 'ipv4_only', 5: 'ipv6_only' }, 'default')
const normalizeDnsServerType = (v: any) =>
  mapEnum(v, { 1: 'local', 2: 'hosts', 3: 'tcp', 4: 'udp', 5: 'tls', 6: 'https', 7: 'quic', 8: 'h3', 9: 'dhcp', 10: 'fakeip', 11: 'tailscale' }, 'local')
const normalizeMixinPriority = (v: any) => mapEnum(v, { 1: 'mixin', 2: 'gui' }, 'mixin')
const normalizeMixinFormat = (v: any) => mapEnum(v, { 1: 'json', 2: 'yaml' }, 'yaml')

const normalizeRuleType = (v: any) =>
  mapEnum(
    v,
    {
      1: 'inbound',
      2: 'network',
      3: 'protocol',
      4: 'domain',
      5: 'domain_suffix',
      6: 'domain_keyword',
      7: 'domain_regex',
      8: 'source_ip_cidr',
      9: 'ip_cidr',
      10: 'ip_is_private',
      11: 'source_port',
      12: 'source_port_range',
      13: 'port',
      14: 'port_range',
      15: 'process_name',
      16: 'process_path',
      17: 'process_path_regex',
      18: 'clash_mode',
      19: 'rule_set',
      20: 'ip_accept_any',
      21: 'inline',
      22: 'InsertionPoint',
    },
    'inline',
  )
