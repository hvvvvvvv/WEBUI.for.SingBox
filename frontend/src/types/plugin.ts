export type PluginTrigger = 'on::subscribe' | 'on::generate' | 'on::manual'

export interface PluginSetting {
  key: string
  title: string
  description: string
  component: 'Switch' | 'Radio' | 'Input' | 'TextArea'
  value: boolean | string
  options?: string[]
}

export interface Plugin {
  id: string
  name: string
  description: string
  version: string
  triggers: PluginTrigger[]
  disabled: boolean
  source: string
  configuration?: Record<string, unknown>
  configuration_schema?: PluginSetting[]
  manual_target?: 'subscription' | 'diagnostic'
  conflicts?: string[]
}

export interface PluginSnapshot {
  plugins: Plugin[]
  revision: number
}
export interface PluginCatalog {
  plugins: Plugin[]
  source: 'bundled' | 'cache' | 'remote'
  repository: string
  warning?: string
}
export interface PluginRunResult {
  logs: string[]
  before?: number
  after?: number
  subscription_id?: string
  report?: unknown
}
