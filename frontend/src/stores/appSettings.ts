import { defineStore } from 'pinia'
import { ref, watch } from 'vue'
import { parse, stringify } from 'yaml'

import {
  ReadFile,
  WriteFile,
  WindowSetSystemDefaultTheme,
  WindowIsMaximised,
  WindowIsMinimised,
} from '@/bridge'
import {
  Colors,
  DefaultCardColumns,
  DefaultConcurrencyLimit,
  DefaultControllerSensitivity,
  DefaultFontFamily,
  DefaultTestTimeout,
  DefaultTestURL,
  UserFilePath,
} from '@/constant/app'
import { DefaultConnections, DefaultCoreConfig } from '@/constant/kernel'
import {
  Theme,
  WindowStartState,
  Lang,
  View,
  Color,
  WebviewGpuPolicy,
  ControllerCloseMode,
  Branch,
} from '@/enums/app'
import i18n, { loadLocale } from '@/lang'
import { useAppStore, useEnvStore } from '@/stores'
import {
  debounce,
  updateTrayAndMenus,
  ignoredError,
  GetSystemProxyBypass,
  deepClone,
} from '@/utils'

import type { AppSettings } from '@/types/app'

export const useAppSettingsStore = defineStore('app-settings', () => {
  const appStore = useAppStore()
  const envStore = useEnvStore()

  let latestUserSettings: string
  let extraFields: Record<string, any> = {}

  const app = ref<AppSettings>({
    lang: Lang.EN,
    theme: Theme.Auto,
    color: Color.Default,
    primaryColor: '#000',
    secondaryColor: '#545454',
    fontFamily: DefaultFontFamily,
    profilesView: View.Grid,
    subscribesView: View.Grid,
    rulesetsView: View.Grid,
    pluginsView: View.Grid,
    scheduledtasksView: View.Grid,
    windowStartState: WindowStartState.Normal,
    webviewGpuPolicy: WebviewGpuPolicy.OnDemand,
    width: 0,
    height: 0,
    exitOnClose: true,
    closeKernelOnExit: true,
    autoSetSystemProxy: true,
    proxyBypassList: '',
    autoStartKernel: false,
    autoRestartKernel: false,
    userAgent: '',
    startupDelay: 30,
    connections: DefaultConnections(),
    kernel: {
      realMemoryUsage: false,
      branch: Branch.Main,
      profile: '',
      autoClose: true,
      unAvailable: true,
      cardMode: true,
      cardColumns: DefaultCardColumns,
      sortByDelay: false,
      testUrl: DefaultTestURL,
      testTimeout: DefaultTestTimeout,
      concurrencyLimit: DefaultConcurrencyLimit,
      controllerCloseMode: ControllerCloseMode.All,
      controllerSensitivity: DefaultControllerSensitivity,
      main: undefined as any,
      alpha: undefined as any,
    },
    pluginSettings: {},
    githubApiToken: '',
    multipleInstance: false,
    addPluginToMenu: false,
    addGroupToMenu: false,
    rollingRelease: true,
    debugOutline: false,
    debugNoAnimation: false,
    debugNoRounded: false,
    debugBorder: false,
    pages: ['Overview', 'Profiles', 'Subscriptions', 'Plugins'],
  })

  const saveAppSettings = debounce((config: string) => {
    // Merge back any extra fields not managed by the frontend (e.g. authSecret)
    if (Object.keys(extraFields).length > 0) {
      const obj = parse(config) || {}
      Object.assign(obj, extraFields)
      config = stringify(obj)
    }
    WriteFile(UserFilePath, config)
  }, 500)

  const setupAppSettings = async () => {
    const data = await ignoredError(ReadFile, UserFilePath)
    const defaults = deepClone(app.value)
    let settings: AppSettings
    if (data) {
      const raw = parse(data) || {}
      // Preserve fields that the frontend doesn't manage (e.g. authSecret)
      const knownKeys = new Set(Object.keys(defaults))
      extraFields = {}
      for (const key of Object.keys(raw)) {
        if (!knownKeys.has(key)) {
          extraFields[key] = raw[key]
        }
      }
      // Merge file values onto defaults so missing fields are filled in
      settings = { ...defaults, ...raw } as AppSettings
      // Deep-merge nested objects that must not be fully replaced by a partial value
      if (raw.kernel) {
        settings.kernel = { ...defaults.kernel, ...raw.kernel }
      }
      if (raw.connections) {
        settings.connections = { ...defaults.connections, ...raw.connections }
      }
    } else {
      settings = defaults
    }

    await appStore.loadLocales(false, false)

    if (!settings.kernel?.main) {
      if (!settings.kernel) settings.kernel = {} as any
      settings.kernel.main = DefaultCoreConfig()
      settings.kernel.alpha = DefaultCoreConfig()
    }
    if (!settings.proxyBypassList) {
      settings.proxyBypassList = await GetSystemProxyBypass()
    }

    app.value = settings
    latestUserSettings = stringify(app.value)
  }

  const applyAppSettings = {
    theme(theme: Theme) {
      const isAuto = theme === Theme.Auto
      if (isAuto) {
        themeMode.value = mediaQueryList.matches ? Theme.Dark : Theme.Light
      } else {
        themeMode.value = theme
      }
    },
    lang(lang: string) {
      i18n.global.locale.value = lang
      if (!i18n.global.availableLocales.includes(lang)) {
        loadLocale(lang)
      }
    },
    color(color: Color, primary: string, secondary: string) {
      if (color !== Color.Custom) {
        ;({ primary, secondary } = Colors[color] ?? { primary, secondary })
      }
      document.documentElement.style.setProperty('--primary-color', primary)
      document.documentElement.style.setProperty('--secondary-color', secondary)
    },
    feature(outline: boolean, noAnimation: boolean, noRounded: boolean, border: boolean) {
      document.body.setAttribute('feature-outline', String(outline))
      document.body.setAttribute('feature-no-animation', String(noAnimation))
      document.body.setAttribute('feature-no-rounded', String(noRounded))
      document.body.setAttribute('feature-border', String(border))
    },
    fontFamily(fontFamily: string) {
      document.body.style.fontFamily = fontFamily
    },
    windowSize(width: number, height: number) {
      app.value.width = width
      app.value.height = height
    },
    systemProxyBypass() {
      if (envStore.systemProxy) {
        envStore.setSystemProxy()
      }
    },
  }

  /* Apply AppSettings */
  const onAppSettingsChange = (settings: AppSettings) => {
    applyAppSettings.theme(settings.theme)
    applyAppSettings.color(settings.color, settings.primaryColor, settings.secondaryColor)
    applyAppSettings.lang(settings.lang)
    applyAppSettings.fontFamily(settings.fontFamily)
    applyAppSettings.feature(
      settings.debugOutline,
      settings.debugNoAnimation,
      settings.debugNoRounded,
      settings.debugBorder,
    )
    const lastModifiedSettings = stringify(settings)
    if (latestUserSettings !== lastModifiedSettings) {
      saveAppSettings(lastModifiedSettings).then(() => {
        latestUserSettings = lastModifiedSettings
      })
    } else {
      saveAppSettings.cancel()
    }
  }
  watch(app, onAppSettingsChange, { deep: true })

  /* Apply AppTheme */
  const themeMode = ref<Theme.Light | Theme.Dark>(Theme.Light)
  const mediaQueryList = window.matchMedia('(prefers-color-scheme: dark)')
  mediaQueryList.addEventListener('change', ({ matches }) => {
    if (app.value.theme === Theme.Auto) {
      themeMode.value = matches ? Theme.Dark : Theme.Light
    }
  })
  const setAppTheme = (theme: Theme.Dark | Theme.Light) => {
    if (document.startViewTransition) {
      document.startViewTransition(() => {
        document.body.setAttribute('theme-mode', theme)
      })
    } else {
      document.body.setAttribute('theme-mode', theme)
    }
    WindowSetSystemDefaultTheme()
  }
  watch(themeMode, setAppTheme, { immediate: true })

  /* Apply WindowSize */
  const onWindowSizeChange = debounce(async () => {
    const [isMinimised, isMaximised] = await Promise.all([WindowIsMinimised(), WindowIsMaximised()])
    if (!isMinimised && !isMaximised) {
      const w = document.documentElement.clientWidth
      const h = document.documentElement.clientHeight
      applyAppSettings.windowSize(w, h)
    }
  }, 1000)
  window.addEventListener('resize', onWindowSizeChange)

  /* Apply TrayAndMenus */
  watch(
    [
      themeMode,
      appStore.locales,
      () => app.value.color,
      () => app.value.lang,
      () => app.value.addPluginToMenu,
    ],
    updateTrayAndMenus,
  )

  /* Apply SystemProxyBypass */
  const setSystemProxyBypass = debounce(() => {
    applyAppSettings.systemProxyBypass()
  }, 3000)
  watch(() => app.value.proxyBypassList, setSystemProxyBypass)

  const setExtraField = (key: string, value: any) => {
    if (value === undefined || value === null || value === '') {
      delete extraFields[key]
    } else {
      extraFields[key] = value
    }
  }

  return { setupAppSettings, app, themeMode, setExtraField }
})
