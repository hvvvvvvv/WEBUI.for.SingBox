<script setup lang="ts">
import { nextTick } from 'vue'
import logo from '@/assets/logo'
import { apiCall } from '@/bridge/http'
import { useAppSettingsStore, useKernelApiStore } from '@/stores'
import { APP_TITLE } from '@/utils'

const appSettingsStore = useAppSettingsStore()
const kernelApiStore = useKernelApiStore()

const handleLogout = async () => {
  await apiCall('/auth/logout').catch(() => {})
  appSettingsStore.sessionInfo.cacheToken = ''
  appSettingsStore.sessionInfo.requireLogin = true
  await nextTick()
  location.reload()
}
</script>

<template>
  <div class="app-titlebar flex items-center py-8 gap-8 px-12">
    <img class="w-24 h-24" draggable="false" :src="logo" />

    <div
      :style="{
        color: kernelApiStore.running ? 'var(--primary-color)' : 'var(--color)',
      }"
      class="font-bold w-full h-full flex items-center text-14"
    >
      {{ APP_TITLE }}
      <Icon
        v-if="kernelApiStore.starting || kernelApiStore.stopping || kernelApiStore.restarting"
        :size="14"
        icon="loading"
        class="rotation mx-4"
      />
    </div>

    <div v-if="appSettingsStore.sessionInfo.authEnabled" class="flex items-center">
      <Button
        v-tips="'auth.logout'"
        type="text"
        icon="logout"
        size="small"
        @click.stop="handleLogout"
      />
    </div>
  </div>
</template>

<style scoped>
@media (max-width: 700px) {
  .app-titlebar {
    min-height: 44px;
    flex-shrink: 0;
    padding: env(safe-area-inset-top) 12px 0;
  }
  .app-titlebar > img {
    flex-shrink: 0;
  }
  .app-titlebar > .font-bold {
    min-width: 0;
    overflow-wrap: anywhere;
  }
  .app-titlebar :deep(.gui-button) {
    min-width: 44px;
    min-height: 44px;
    box-sizing: border-box;
  }
}
</style>
