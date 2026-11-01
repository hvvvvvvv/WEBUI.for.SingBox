<script setup lang="ts">
import { useI18n } from 'vue-i18n'

import logo from '@/assets/logo'
import { useAppStore } from '@/stores'
import { APP_TITLE, APP_VERSION } from '@/utils/env'

const { t } = useI18n()
const appStore = useAppStore()

appStore.checkForUpdates()
</script>

<template>
  <div class="flex flex-col items-center py-12">
    <img :src="logo" :alt="APP_TITLE" class="w-64" draggable="false" />
    <div class="py-8 font-bold">{{ APP_TITLE }}</div>
    <div class="flex items-center justify-center flex-wrap">
      <Button
        :loading="appStore.checkForUpdatesLoading"
        type="text"
        size="small"
        @click="appStore.checkForUpdates(true)"
      >
        Bridge: {{ appStore.currentVersion || '--' }} - UI: {{ APP_VERSION || '--' }}
      </Button>
      <Button
        v-if="appStore.updateReady"
        :loading="appStore.downloading"
        size="small"
        @click="appStore.applyAppUpdate"
      >
        {{ t('about.applyUpdate') }}: {{ appStore.updatedVersion }}
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
  </div>
</template>
