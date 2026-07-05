import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

import { BrowserOpenURL, createRpcClient, EventsOff, EventsOn } from '@/bridge'
import { Branch } from '@/enums/app'
import { useAppConfigStore, useKernelApiStore } from '@/stores'
import { confirm, message, sampleID } from '@/utils'
import { KernelBranch } from '../../gen/app/v1/app_pb'
import { KernelRuntimeService } from '../../gen/kernel/v1/kernel_runtime_service_pb'

const StablePage = 'https://github.com/SagerNet/sing-box/releases/latest'
const AlphaPage = 'https://github.com/SagerNet/sing-box/releases'

export const useCoreBranch = (isAlpha = false) => {
  const branch = isAlpha ? KernelBranch.ALPHA : KernelBranch.MAIN

  const localVersion = ref('')
  const remoteVersion = ref('')
  const versionDetail = ref('')
  const releasePageUrl = ref(isAlpha ? AlphaPage : StablePage)
  const remoteAssetTrusted = ref(true)

  const localVersionLoading = ref(false)
  const remoteVersionLoading = ref(false)
  const downloading = ref(false)
  const downloadCompleted = ref(false)
  const downloadProgress = ref('')
  const cancelDownload = ref<() => void | Promise<void>>()

  const rollbackable = ref(false)

  const { t } = useI18n()
  const appConfig = useAppConfigStore()
  const kernelApiStore = useKernelApiStore()
  const kernelService = createRpcClient(KernelRuntimeService)

  const restartable = computed(() => {
    const { branch } = appConfig.config
    if (!kernelApiStore.running) return false
    return localVersion.value && downloadCompleted.value && (branch === Branch.Alpha) === isAlpha
  })

  const updatable = computed(
    () => remoteVersion.value && localVersion.value !== remoteVersion.value,
  )

  const applyLocalVersion = (value: {
    localVersion: string
    versionDetail: string
    rollbackable: boolean
  }) => {
    localVersion.value = value.localVersion
    versionDetail.value = value.versionDetail
    rollbackable.value = value.rollbackable
  }

  const getLocalVersion = async (showTips = false) => {
    localVersionLoading.value = true
    try {
      const res = await kernelService.getCoreBranchLocalVersion({ branch })
      applyLocalVersion(res)
      return res.localVersion
    } catch (error: any) {
      console.log(error)
      showTips && message.error(error)
    } finally {
      localVersionLoading.value = false
    }
    return ''
  }

  const getRemoteVersion = async (showTips = false) => {
    remoteVersionLoading.value = true
    try {
      const res = await kernelService.getCoreBranchRemoteVersion({ branch })
      releasePageUrl.value = res.releasePageUrl || releasePageUrl.value
      remoteAssetTrusted.value = res.trustedAsset
      return res.remoteVersion
    } catch (error: any) {
      console.log(error)
      showTips && message.error(error)
    } finally {
      remoteVersionLoading.value = false
    }
    return ''
  }

  const downloadCore = async (allowUntrustedAsset = false) => {
    let allowUntrusted = allowUntrustedAsset === true
    downloading.value = true
    downloadProgress.value = ''
    cancelDownload.value = undefined
    let canceled = false
    const progressEvent = sampleID()

    try {
      if (!allowUntrusted) {
        await refreshRemoteVersion()
        if (!remoteAssetTrusted.value) {
          await confirm('common.warning', 'settings.kernel.risk', {
            type: 'text',
            okText: 'settings.kernel.stillDownload',
          })
          allowUntrusted = true
        }
      }

      EventsOn(progressEvent, (progress: number, total: number) => {
        if (total <= 0) {
          downloadProgress.value = t('common.downloading')
          return
        }
        downloadProgress.value = t('common.downloading') + ((progress / total) * 100).toFixed(2) + '%'
      })

      cancelDownload.value = async () => {
        canceled = true
        await kernelService.cancelCoreDownload({ progressEvent })
        cancelDownload.value = undefined
      }

      const res = await kernelService.downloadCore({
        branch,
        progressEvent,
        allowUntrustedAsset: allowUntrusted,
      })
      applyLocalVersion(res)
      downloadCompleted.value = true
      message.success('common.success')
    } catch (error: any) {
      console.log(error)
      if (!canceled) message.error(error.message || error)
      downloadCompleted.value = false
    } finally {
      EventsOff(progressEvent)
      downloading.value = false
      cancelDownload.value = undefined
    }
  }

  const restartCore = async () => {
    if (!kernelApiStore.running) return
    try {
      await kernelApiStore.restartCore()
      downloadCompleted.value = false
    } catch (error: any) {
      message.error(error)
    }
  }

  const refreshLocalVersion = async (showTips = false) => {
    localVersion.value = await getLocalVersion(showTips)
  }

  const refreshRemoteVersion = async (showTips = false) => {
    remoteVersion.value = await getRemoteVersion(showTips)
  }

  const rollbackCore = async () => {
    await confirm('common.warning', 'settings.kernel.rollback')
    const res = await kernelService.rollbackCore({ branch })
    applyLocalVersion(res)
    message.success('common.success')
  }

  const clearCoreCache = async () => {
    await kernelService.clearCoreCache({ branch })
    message.success('common.success')
  }

  const openReleasePage = () => {
    BrowserOpenURL(releasePageUrl.value || (isAlpha ? AlphaPage : StablePage))
  }

  watch(
    () => appConfig.config.branch,
    () => (downloadCompleted.value = false),
  )

  refreshLocalVersion()
  refreshRemoteVersion()

  return {
    restartable,
    updatable,
    rollbackable,
    versionDetail,
    localVersion,
    localVersionLoading,
    remoteVersion,
    remoteVersionLoading,
    downloading,
    downloadProgress,
    refreshLocalVersion,
    refreshRemoteVersion,
    downloadCore,
    cancelDownload,
    restartCore,
    rollbackCore,
    clearCoreCache,
    openReleasePage,
  }
}
