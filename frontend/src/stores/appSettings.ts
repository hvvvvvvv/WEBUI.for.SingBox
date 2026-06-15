import { defineStore } from 'pinia'
import { ref, watch } from 'vue'
import { parse, stringify } from 'yaml'

import { ReadFile, WriteFile } from '@/bridge'
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
  Lang,
  View,
  Color,
  ControllerCloseMode,
  Branch,
} from '@/enums/app'
import i18n, { loadLocale } from '@/lang'
import { useAppStore } from '@/stores'
import {
  debounce,
  ignoredError,
  deepClone,
} from '@/utils'

import type { AppSettings, SessionInfo } from '@/types/app'

export const useAppSettingsStore = defineStore('app-settings', () => {
  const appStore = useAppStore()

  let latestUserSettings: string

  const getSessionInfo = (): SessionInfo => {
    const raw = sessionStorage.getItem('sessionInfo')
    const defaults: SessionInfo = { authEnabled: false, cacheToken: '', requireLogin: false }
    if (!raw) return defaults
    try {
      const parsed = JSON.parse(raw)
      return parsed ?? defaults
    } catch {
      return defaults
    }
  }

  const sessionInfo = ref<SessionInfo>(getSessionInfo())

  watch(sessionInfo, (newInfo) => {
    sessionStorage.setItem('sessionInfo', JSON.stringify(newInfo))
  }, { deep: true })

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
    scheduledtasksView: View.Grid,
    autoSetSystemProxy: true,
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
    githubApiToken: '',
    multipleInstance: false,
    rollingRelease: true,
    debugOutline: false,
    debugNoAnimation: false,
    debugNoRounded: false,
    debugBorder: false,
    pages: ['Overview', 'Profiles', 'Subscriptions']
  })

  const saveAppSettings = debounce((config: string) => {
    WriteFile(UserFilePath, config)
  }, 500)

  const setupAppSettings = async () => {
    const data = await ignoredError(ReadFile, UserFilePath)
    const defaults = deepClone(app.value)
    let settings: AppSettings
    if (data) {
      const raw = parse(data) || {}
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
  }
  watch(themeMode, setAppTheme, { immediate: true })

  return { setupAppSettings, app, themeMode, sessionInfo }
})
