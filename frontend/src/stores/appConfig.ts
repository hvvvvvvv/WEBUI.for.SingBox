import { defineStore } from 'pinia'
import { ref, watch } from 'vue'

import { createRpcClient } from '@/bridge'
import { DefaultCoreConfig } from '@/constant/kernel'
import { Branch } from '@/enums/app'
import { debounce, deepClone } from '@/utils'
import {
  KernelBranch,
  type AppConfig as ProtoAppConfig,
  type CoreRuntimeConfig as ProtoCoreRuntimeConfig,
} from '../../gen/app/v1/app_pb'
import { AppConfigService } from '../../gen/app/v1/app_config_service_pb'

import type { AppConfig, CoreRuntimeConfig } from '@/types/app'

export const useAppConfigStore = defineStore('app-config', () => {
  const service = createRpcClient(AppConfigService)

  let latestConfig: string

  const stableStringify = (value: any): string => {
    if (Array.isArray(value)) {
      return `[${value.map((item) => stableStringify(item)).join(',')}]`
    }
    if (value && typeof value === 'object') {
      return `{${Object.keys(value)
        .sort()
        .map((key) => `${JSON.stringify(key)}:${stableStringify(value[key])}`)
        .join(',')}}`
    }
    return JSON.stringify(value)
  }

  const normalizeCoreConfig = (value?: Partial<CoreRuntimeConfig>): CoreRuntimeConfig => {
    const defaults = DefaultCoreConfig()
    return {
      env: Object.entries(value?.env || {}).reduce((result, [key, item]) => {
        result[key] = String(item)
        return result
      }, {} as Record<string, string>),
      args: Array.isArray(value?.args) && value.args.length > 0 ? [...value.args] : [...defaults.args],
    }
  }

  const defaultConfig = (): AppConfig => ({
    autoStartKernel: false,
    autoRestartKernel: false,
    userAgent: '',
    githubApiToken: '',
    rollingRelease: true,
    branch: Branch.Main,
    profile: '',
    main: normalizeCoreConfig(),
    alpha: normalizeCoreConfig(),
  })

  const protoBranchToBranch = (branch: KernelBranch): Branch => {
    return branch === KernelBranch.ALPHA ? Branch.Alpha : Branch.Main
  }

  const branchToProtoBranch = (branch: Branch): KernelBranch => {
    return branch === Branch.Alpha ? KernelBranch.ALPHA : KernelBranch.MAIN
  }

  const protoCoreConfigToCoreConfig = (value?: ProtoCoreRuntimeConfig): CoreRuntimeConfig => {
    return normalizeCoreConfig({
      env: value?.env || {},
      args: value?.args || [],
    })
  }

  const coreConfigToProtoCoreConfig = (value: CoreRuntimeConfig): ProtoCoreRuntimeConfig => {
    const normalized = normalizeCoreConfig(value)
    return {
      $typeName: 'app.v1.CoreRuntimeConfig',
      env: normalized.env,
      args: normalized.args,
    }
  }

  const protoConfigToConfig = (value?: ProtoAppConfig): AppConfig => {
    const defaults = defaultConfig()
    if (!value) return defaults
    return {
      autoStartKernel: value.autoStartKernel,
      autoRestartKernel: value.autoRestartKernel,
      userAgent: value.userAgent,
      githubApiToken: value.githubApiToken,
      rollingRelease: value.rollingRelease,
      branch: protoBranchToBranch(value.branch),
      profile: value.profile,
      main: protoCoreConfigToCoreConfig(value.main),
      alpha: protoCoreConfigToCoreConfig(value.alpha),
    }
  }

  const configToProtoConfig = (value: AppConfig): ProtoAppConfig => {
    const normalized = normalizeConfig(value)
    return {
      $typeName: 'app.v1.AppConfig',
      autoStartKernel: normalized.autoStartKernel,
      autoRestartKernel: normalized.autoRestartKernel,
      userAgent: normalized.userAgent,
      githubApiToken: normalized.githubApiToken,
      rollingRelease: normalized.rollingRelease,
      branch: branchToProtoBranch(normalized.branch),
      profile: normalized.profile,
      main: coreConfigToProtoCoreConfig(normalized.main),
      alpha: coreConfigToProtoCoreConfig(normalized.alpha),
    }
  }

  const normalizeConfig = (value: AppConfig): AppConfig => ({
    autoStartKernel: !!value.autoStartKernel,
    autoRestartKernel: !!value.autoRestartKernel,
    userAgent: value.userAgent || '',
    githubApiToken: value.githubApiToken || '',
    rollingRelease: !!value.rollingRelease,
    branch: value.branch === Branch.Alpha ? Branch.Alpha : Branch.Main,
    profile: value.profile || '',
    main: normalizeCoreConfig(value.main),
    alpha: normalizeCoreConfig(value.alpha),
  })

  const config = ref<AppConfig>(defaultConfig())

  const saveAppConfig = debounce(async (value: AppConfig) => {
    const result = await service.saveAppConfig({ config: configToProtoConfig(value) })
    return protoConfigToConfig(result.config)
  }, 500)

  const setupAppConfig = async () => {
    const result = await service.getAppConfig({})
    config.value = protoConfigToConfig(result.config)
    latestConfig = stableStringify(config.value)
  }

  watch(config, (value) => {
    const normalized = normalizeConfig(value)
    const current = stableStringify(normalized)
    if (latestConfig !== current) {
      saveAppConfig(deepClone(normalized)).then((saved) => {
        latestConfig = stableStringify(saved)
      })
    } else {
      saveAppConfig.cancel()
    }
  }, { deep: true })

  return {
    config,
    setupAppConfig,
  }
})
