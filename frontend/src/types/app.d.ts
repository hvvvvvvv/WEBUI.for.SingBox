import { h, ref, type VNode } from 'vue'

import type {
  Lang,
  Theme,
  Color,
  View,
  Branch,
  ControllerCloseMode,
  ScheduledTasksType,
  RequestMethod,
  OS,
} from '@/enums/app'

export interface AppEnv {
  appName: string
  appVersion: string
  basePath: string
  appPath: string
  os: OS
  arch: string
  libc: string
  isPrivileged: boolean
}

export interface Menu {
  label: string
  handler?: (...args: any) => void
  separator?: boolean
  children?: Menu[]
}

export interface MenuItem {
  type: 'item' | 'separator'
  text?: string
  tooltip?: string
  event?: (() => void) | string
  children?: MenuItem[]
  hidden?: boolean
  checked?: boolean
}

export interface AppSettings {
  lang: Lang | string
  theme: Theme
  color: Color
  primaryColor: string
  secondaryColor: string
  fontFamily: string
  profilesView: View
  subscribesView: View
  rulesetsView: View
  scheduledtasksView: View
  autoSetSystemProxy: boolean
  autoStartKernel: boolean
  autoRestartKernel: boolean
  userAgent: string
  startupDelay: number
  connections: {
    visibility: Record<string, boolean>
    order: string[]
  }
  kernel: {
    realMemoryUsage: boolean
    branch: Branch
    profile: string
    autoClose: boolean
    unAvailable: boolean
    cardMode: boolean
    cardColumns: number
    sortByDelay: boolean
    testUrl: string
    testTimeout: number
    concurrencyLimit: number
    controllerCloseMode: ControllerCloseMode
    controllerSensitivity: number
    main: {
      env: Recordable
      args: string[]
    }
    alpha: {
      env: Recordable
      args: string[]
    }
  }
  githubApiToken: string
  multipleInstance: boolean
  rollingRelease: boolean
  debugOutline: boolean
  debugNoAnimation: boolean
  debugNoRounded: false
  debugBorder: boolean
  pages: string[]
}

export interface SessionInfo {
  authEnabled: boolean
  cacheToken: string
  requireLogin: boolean
}

export interface ScheduledTask {
  id: string
  name: string
  type: ScheduledTasksType
  subscriptions: string[]
  rulesets: string[]
  script: string
  cron: string
  notification: boolean
  disabled: boolean
  lastTime: number
}

export interface Subscription {
  id: string
  name: string
  upload: number
  download: number
  total: number
  expire: number
  updateTime: number
  type: 'Http' | 'File' | 'Manual'
  url: string
  website: string
  path: string
  include: string
  exclude: string
  includeProtocol: string
  excludeProtocol: string
  proxyPrefix: string
  disabled: boolean
  inSecure: boolean
  proxies: { id: string; tag: string; type: string }[]
  requestMethod: RequestMethod
  requestTimeout: number
  header: {
    request: Recordable
    response: Recordable
  }
  script: string
  // Not Config
  updating?: boolean
}

// Custom Action
export interface CustomActionApi {
  h: typeof h
  ref: typeof ref
}
type CustomActionProps = Recordable
type CustomActionSlots = Recordable<
  ((api: CustomActionApi) => VNode | string | number | boolean) | VNode | string | number | boolean
>
export interface CustomAction<P = CustomActionProps, S = CustomActionSlots> {
  id?: string
  component: string
  componentProps?: P | ((api: CustomActionApi) => P)
  componentSlots?: S | ((api: CustomActionApi) => S)
}
export type CustomActionFn = ((api: CustomActionApi) => CustomAction) & {
  id?: string
}
