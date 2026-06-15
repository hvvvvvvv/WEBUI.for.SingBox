<script setup lang="ts">
import logo from '@/assets/logo'
import { apiCall } from '@/bridge/http'
import { useAppSettingsStore, useKernelApiStore, useEnvStore, useAppStore } from '@/stores'
import { APP_TITLE, APP_VERSION } from '@/utils'

import { OS } from '@/enums/app'

const appSettingsStore = useAppSettingsStore()
const kernelApiStore = useKernelApiStore()
const envStore = useEnvStore()
const appStore = useAppStore()

const isDarwin = envStore.env.os === OS.Darwin
const handleLogout = async () => {
  await apiCall('/auth/logout').catch(() => {})
  location.reload()
}



</script>

<template>
  <div class="flex items-center py-8 gap-8 px-12">
    <img v-if="!isDarwin" class="w-24 h-24" draggable="false" :src="logo" />

    <div
      :class="isDarwin ? 'justify-center py-4 text-12' : 'text-14'"
      :style="{
        color: kernelApiStore.running ? 'var(--primary-color)' : 'var(--color)',
      }"
      class="font-bold w-full h-full flex items-center"
    >
      {{ APP_TITLE }} {{ APP_VERSION }}
      <CustomAction :actions="appStore.customActions.title_bar" />
      <Icon
        v-if="kernelApiStore.starting || kernelApiStore.stopping || kernelApiStore.restarting"
        :size="14"
        icon="loading"
        class="rotation mx-4"
      />
    </div>

    <div v-if="appSettingsStore.sessionInfo.authEnabled" class="flex items-center">
      <Button v-tips="'auth.logout'" type="text" icon="logout" size="small" @click.stop="handleLogout" />
    </div>

  </div>
</template>
