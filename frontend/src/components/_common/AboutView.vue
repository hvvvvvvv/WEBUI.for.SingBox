<script setup lang="ts">
import { useI18n } from 'vue-i18n'

import logo from '@/assets/logo'
import { BrowserOpenURL } from '@/bridge'
import { useAppStore, useEnvStore } from '@/stores'
import { APP_TITLE, APP_VERSION, PROJECT_URL } from '@/utils'

const { t } = useI18n()
const envStore = useEnvStore()
const appStore = useAppStore()


appStore.checkForUpdates()
</script>

<template>
  <div class="flex flex-col items-center pt-36">
    <img :src="logo" class="w-128" draggable="false" />
    <div class="py-8 font-bold">{{ APP_TITLE }}</div>
    <div class="flex items-center pb-8 my-4">
      <Button
          :loading="appStore.checkForUpdatesLoading"
          type="link"
          size="small"
          @click="appStore.checkForUpdates(true)"
        >
          Bridge: {{ envStore.env.appVersion }} - UI: {{ APP_VERSION }}
        </Button>
        <Button
          v-if="appStore.updatable"
          :loading="appStore.downloading"
          size="small"
          @click="appStore.downloadApp"
        >
          {{ t('about.new') }}: {{ appStore.remoteVersion }}
      </Button>
    </div>
    <div
      class="text-12 underline flex items-center cursor-pointer"
      @click="BrowserOpenURL(PROJECT_URL)"
    >
      <Icon icon="github" />GitHub
    </div>
  </div>
</template>
