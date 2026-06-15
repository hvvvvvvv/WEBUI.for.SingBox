<script lang="ts" setup>
import { ref } from 'vue'

import { ExitApp } from '@/bridge'
import { useAppSettingsStore, useEnvStore } from '@/stores'
import {
  confirm,
  message,
  CheckPermissions,
  SwitchPermissions,
  RunWithPowerShell,
  IsAutoStartEnabled,
  EnableAutoStart,
  DisableAutoStart,
} from '@/utils'
import { OS } from '@/enums/app'

const appSettings = useAppSettingsStore()
const envStore = useEnvStore()

const isAdmin = ref(false)
const isAutoStart = ref(false)

const restartApp = async (admin = false) => {
  if (admin) {
    await RunWithPowerShell(envStore.env.appPath, [], { admin, wait: false })
  } else {
    await RunWithPowerShell('explorer', [envStore.env.appPath], { wait: false })
  }
  await ExitApp()
}

const onPermChange = async (v: boolean) => {
  try {
    await SwitchPermissions(v)
    if (v !== envStore.env.isPrivileged) {
      const ok = await confirm('Notice', 'Restart the application now?').catch(() => 0)
      ok && (await restartApp(v))
    }
  } catch (error: any) {
    message.error(error)
    console.log(error)
  }
}

const onTaskSchChange = async (v: boolean) => {
  isAutoStart.value = !v

  try {
    await (v ? EnableAutoStart(appSettings.app.startupDelay) : DisableAutoStart())
    isAutoStart.value = v
  } catch (error: any) {
    console.error(error)
    message.error(error)
  }
}

const onStartupDelayChange = async (delay: number) => {
  if (appSettings.app.startupDelay !== delay) {
    try {
      await EnableAutoStart(delay)
      appSettings.app.startupDelay = delay
    } catch (error: any) {
      console.error(error)
      message.error(error)
    }
  }
}

IsAutoStartEnabled().then((res) => {
  isAutoStart.value = res
})

if (envStore.env.os === OS.Windows) {
  CheckPermissions().then((admin) => {
    isAdmin.value = admin
  })
}
</script>

<template>
  <div class="px-8 py-12 text-18 font-bold">{{ $t('settings.behavior') }}</div>

  <Card>
    <div v-platform="[OS.Windows]" class="px-8 py-12 flex items-center justify-between">
      <div class="text-16 font-bold">
        {{ $t('settings.admin') }}
        <span class="font-normal text-12">({{ $t('settings.needRestart') }})</span>
      </div>
      <div class="flex items-center gap-4">
        <Button
          v-if="envStore.env.isPrivileged !== isAdmin"
          v-tips="'titlebar.restart'"
          type="primary"
          icon="refresh"
          size="small"
          @click="() => restartApp(isAdmin)"
        />
        <Switch v-model="isAdmin" @change="onPermChange" />
      </div>
    </div>
    <div class="px-8 py-12 flex items-center justify-between">
      <div class="text-16 font-bold">
        {{ $t('settings.startup.name') }}
        <span v-platform="[OS.Windows]" class="font-normal text-12">
          ({{ $t('settings.needAdmin') }})
        </span>
      </div>
      <Switch v-model="isAutoStart" @change="onTaskSchChange" />
    </div>
    <div
      v-if="isAutoStart"
      v-platform="[OS.Windows]"
      class="px-8 py-12 flex items-center justify-between"
    >
      <div class="text-16 font-bold">
        {{ $t('settings.startup.startupDelay') }}
        <span class="font-normal text-12">({{ $t('settings.needAdmin') }})</span>
      </div>
      <Input
        :model-value="appSettings.app.startupDelay"
        :min="10"
        :max="180"
        editable
        type="number"
        @submit="onStartupDelayChange"
      >
        <template #suffix="{ showInput }">
          <span class="ml-4" @click="showInput">{{ $t('settings.startup.delay') }}</span>
        </template>
      </Input>
    </div>
  </Card>
</template>
