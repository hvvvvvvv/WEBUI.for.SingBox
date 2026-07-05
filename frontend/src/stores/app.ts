import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'

import { createRpcClient, EventsOff, EventsOn } from '@/bridge'
import { LanguageOptions } from '@/constant/app'
import { message, sampleID } from '@/utils'

import type { CustomAction, CustomActionFn, Menu } from '@/types/app'
import { AppUpdateService } from '../../gen/app/v1/app_update_service_pb'

export const useAppStore = defineStore('app', () => {
  const updateService = createRpcClient(AppUpdateService)
  const isAppReloading = ref(false)

  /* Global Menu */
  const menuShow = ref(false)
  const menuList = ref<Menu[]>([])
  const menuPosition = ref({
    x: 0,
    y: 0,
  })

  /* Global Tips */
  const tipsShow = ref(false)
  const tipsMessage = ref('')
  const tipsPosition = ref({
    x: 0,
    y: 0,
  })

  /* Modal Stack */
  const modalStack: (() => void)[] = []
  const modalZIndexCounter = 999

  /* i18n */
  const locales = ref<{ label: string; value: string }[]>(LanguageOptions)

  /* Actions */
  const customActions = ref({
    core_state: [] as (CustomAction | CustomActionFn)[],
    title_bar: [] as (CustomAction | CustomActionFn)[],
    profiles_header: [] as (CustomAction | CustomActionFn)[],
    subscriptions_header: [] as (CustomAction | CustomActionFn)[],
  })
  const addCustomActions = (
    target: keyof typeof customActions.value,
    actions: CustomAction | CustomAction[] | CustomActionFn | CustomActionFn[],
  ) => {
    if (!customActions.value[target]) throw new Error('Target does not exist: ' + target)
    const _actions = Array.isArray(actions) ? actions : [actions]
    _actions.forEach((action) => !action.id && (action.id = sampleID()))
    customActions.value[target].push(..._actions)
    const remove = () => {
      customActions.value[target] = customActions.value[target].filter(
        (a) => !_actions.some((added) => added.id === a.id),
      )
    }
    return remove
  }
  const removeCustomActions = (target: keyof typeof customActions.value, id: string | string[]) => {
    if (!customActions.value[target]) throw new Error('Target does not exist: ' + target)
    const ids = Array.isArray(id) ? id : [id]
    customActions.value[target] = customActions.value[target].filter((a) => !ids.includes(a.id!))
  }

  const { t } = useI18n()

  /* About Page */
  const showAbout = ref(false)
  const checkForUpdatesLoading = ref(false)
  const downloading = ref(false)
  const currentVersion = ref('')
  const updatedVersion = ref('')
  const remoteVersion = ref('')
  const updatable = ref(false)
  const updateReady = computed(
    () =>
      currentVersion.value !== '' &&
      updatedVersion.value !== '' &&
      currentVersion.value !== updatedVersion.value,
  )

  const applyVersionState = (value: {
    currentVersion: string
    updatedVersion: string
    latestVersion?: string
    updatable?: boolean
  }) => {
    currentVersion.value = value.currentVersion
    updatedVersion.value = value.updatedVersion
    if (value.latestVersion !== undefined) {
      remoteVersion.value = value.latestVersion
    }
    if (value.updatable !== undefined) {
      updatable.value = value.updatable
    }
  }

  const setupAppVersion = async () => {
    const res = await updateService.getAppVersion({})
    applyVersionState(res)
    if (!remoteVersion.value) {
      remoteVersion.value = res.updatedVersion || res.currentVersion
    }
  }

  const downloadApp = async () => {
    downloading.value = true
    const progressEvent = sampleID()
    try {
      const { update, destroy } = message.info('common.downloading', 10 * 60 * 1_000, () => {
        updateService.cancelAppUpdate({ progressEvent })
      })

      EventsOn(progressEvent, (progress: number, total: number) => {
        if (total <= 0) {
          update(t('common.downloading'))
          return
        }
        update(t('common.downloading') + ((progress / total) * 100).toFixed(2) + '%')
      })

      const res = await updateService.downloadAppUpdate({ progressEvent }).finally(destroy)
      applyVersionState({
        currentVersion: res.currentVersion,
        updatedVersion: res.updatedVersion,
        latestVersion: res.latestVersion,
        updatable: false,
      })
      message.success('about.updateSuccessfulRestart')
    } catch (error: any) {
      console.log(error)
      message.error(error.message || error, 5_000)
    } finally {
      EventsOff(progressEvent)
      downloading.value = false
    }
  }

  const checkForUpdates = async (showTips = false) => {
    if (checkForUpdatesLoading.value || downloading.value) return
    checkForUpdatesLoading.value = true
    try {
      const res = await updateService.checkAppUpdate({})
      applyVersionState(res)

      if (showTips) {
        message.info(updatable.value ? 'about.newVersion' : 'about.latestVersion')
      }
    } catch (error: any) {
      console.error(error)
      message.error(error.message || error)
    }
    checkForUpdatesLoading.value = false
  }

  const applyAppUpdate = async () => {
    try {
      await updateService.applyAppUpdate({})
      message.info('about.updateSuccessfulRestart', 10_000)
    } catch (error: any) {
      console.error(error)
      message.error(error.message || error, 5_000)
    }
  }

  return {
    isAppReloading,
    menuShow,
    menuPosition,
    menuList,
    tipsShow,
    tipsMessage,
    tipsPosition,
    modalStack,
    modalZIndexCounter,
    showAbout,
    checkForUpdatesLoading,
    downloading,
    currentVersion,
    updatedVersion,
    remoteVersion,
    updatable,
    updateReady,
    setupAppVersion,
    checkForUpdates,
    downloadApp,
    applyAppUpdate,
    customActions,
    addCustomActions,
    removeCustomActions,
    locales,
  }
})
