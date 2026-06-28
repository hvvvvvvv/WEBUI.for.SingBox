import { defineStore } from 'pinia'
import { ref, watch } from 'vue'

import { createRpcClient } from '@/bridge'
import {
  Colors,
  DefaultCardColumns,
  DefaultConcurrencyLimit,
  DefaultControllerSensitivity,
  DefaultFontFamily,
  DefaultTestTimeout,
  DefaultTestURL,
} from '@/constant/app'
import { DefaultConnections } from '@/constant/kernel'
import {
  Theme,
  Lang,
  View,
  Color,
  ControllerCloseMode,
} from '@/enums/app'
import i18n from '@/lang'
import {
  debounce,
  deepClone,
} from '@/utils'
import { AppSettingsService } from '../../gen/app/v1/app_settings_service_pb'

import type { AppSettings, SessionInfo } from '@/types/app'

export const useAppSettingsStore = defineStore('app-settings', () => {
  const service = createRpcClient(AppSettingsService)

  let latestUserSettings: string

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

  const parseSettingsJSON = (settingsJson: string): Recordable => {
    if (!settingsJson) return {}
    return JSON.parse(settingsJson) || {}
  }

  const isRecord = (value: unknown): value is Recordable => {
    return !!value && typeof value === 'object' && !Array.isArray(value)
  }

  const normalizeSettings = (rawSettings: unknown, defaults: AppSettings): AppSettings => {
    const raw = isRecord(rawSettings) ? rawSettings : {}
    const settings = {
      lang: defaults.lang,
      theme: defaults.theme,
      color: defaults.color,
      primaryColor: defaults.primaryColor,
      secondaryColor: defaults.secondaryColor,
      fontFamily: defaults.fontFamily,
      profilesView: defaults.profilesView,
      subscribesView: defaults.subscribesView,
      rulesetsView: defaults.rulesetsView,
      scheduledtasksView: defaults.scheduledtasksView,
      connections: { ...defaults.connections },
      kernel: { ...defaults.kernel },
      debugOutline: defaults.debugOutline,
      debugNoAnimation: defaults.debugNoAnimation,
      debugNoRounded: defaults.debugNoRounded,
      debugBorder: defaults.debugBorder,
      pages: defaults.pages,
    } as any
    ;[
      'lang',
      'theme',
      'color',
      'primaryColor',
      'secondaryColor',
      'fontFamily',
      'profilesView',
      'subscribesView',
      'rulesetsView',
      'scheduledtasksView',
      'debugOutline',
      'debugNoAnimation',
      'debugNoRounded',
      'debugBorder',
      'pages',
    ].forEach((key) => {
      if (key in raw) {
        settings[key] = raw[key]
      }
    })

    settings.connections = { ...defaults.connections }
    if (isRecord(raw.connections)) {
      if (isRecord(raw.connections.visibility)) {
        settings.connections.visibility = raw.connections.visibility
      }
      if (Array.isArray(raw.connections.order)) {
        settings.connections.order = raw.connections.order
      }
    }

    settings.kernel = { ...defaults.kernel }
    if (isRecord(raw.kernel)) {
      ;[
        'realMemoryUsage',
        'autoClose',
        'unAvailable',
        'cardMode',
        'cardColumns',
        'sortByDelay',
        'testUrl',
        'testTimeout',
        'concurrencyLimit',
        'controllerCloseMode',
        'controllerSensitivity',
      ].forEach((key) => {
        if (key in raw.kernel) {
          ;(settings.kernel as any)[key] = raw.kernel[key]
        }
      })
    }

    return settings as AppSettings
  }

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
    connections: DefaultConnections(),
    kernel: {
      realMemoryUsage: false,
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
    },
    debugOutline: false,
    debugNoAnimation: false,
    debugNoRounded: false,
    debugBorder: false,
    pages: ['Overview', 'Profiles', 'Subscriptions']
  })

  const saveAppSettings = debounce(async (settingsJson: string) => {
    const result = await service.saveAppSettings({ settingsJson })
    return result.settingsJson
  }, 500)

  const setupAppSettings = async () => {
    const { settingsJson } = await service.getAppSettings({})
    const defaults = deepClone(app.value)
    let settings: AppSettings
    if (settingsJson) {
      const raw = parseSettingsJSON(settingsJson)
      settings = normalizeSettings(raw, defaults)
    } else {
      settings = defaults
    }

    app.value = settings
    latestUserSettings = stableStringify(app.value)
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
    const normalizedSettings = normalizeSettings(settings, deepClone(app.value))
    const lastModifiedSettings = stableStringify(normalizedSettings)
    if (latestUserSettings !== lastModifiedSettings) {
      saveAppSettings(JSON.stringify(normalizedSettings)).then((settingsJson) => {
        latestUserSettings = stableStringify(normalizeSettings(parseSettingsJSON(settingsJson as string), deepClone(app.value)))
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
