<script lang="ts" setup>
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

import { useAppConfigStore, useAppSettingsStore } from '@/stores'
import { message } from '@/utils'
import { parseCoreLogDays } from '@/utils/coreLogDays'

const { t } = useI18n()
const appSettings = useAppSettingsStore()
const appConfig = useAppConfigStore()

const coreLogDays = ref<number | string>(appConfig.config.coreLogDays)
const coreLogDaysSaving = ref(false)
watch(
  () => appConfig.config.coreLogDays,
  (value) => {
    coreLogDays.value = value
  },
)
const saveCoreLogDays = async (input: number | string) => {
  if (coreLogDaysSaving.value) return
  const value = parseCoreLogDays(input)
  if (value === undefined) {
    message.error(t('settings.coreLogDays.invalid'))
    coreLogDays.value = appConfig.config.coreLogDays
    return
  }
  if (value === appConfig.config.coreLogDays) return
  coreLogDaysSaving.value = true
  appConfig.config.coreLogDays = value
  try {
    await appConfig.saveNow()
  } catch (error: any) {
    coreLogDays.value = appConfig.config.coreLogDays
    message.error(error.message || String(error))
  } finally {
    coreLogDaysSaving.value = false
  }
}
</script>

<template>
  <div class="px-8 py-12 text-18 font-bold">{{ $t('settings.advanced') }}</div>

  <Card>
    <div class="px-8 py-12 flex items-center justify-between">
      <div class="text-16 font-bold">
        {{ $t('settings.rollingRelease') }}
        <span class="font-normal text-12">({{ $t('settings.needRestart') }})</span>
      </div>
      <div class="flex items-center gap-4">
        <Switch v-model="appConfig.config.rollingRelease" />
      </div>
    </div>
    <div class="px-8 py-12 flex items-center justify-between">
      <div class="text-16 font-bold">
        {{ $t('settings.autoRestartKernel.name') }}
        <span class="font-normal text-12">({{ $t('settings.autoRestartKernel.tips') }})</span>
      </div>
      <Switch v-model="appConfig.config.autoRestartKernel" />
    </div>
    <div class="px-8 py-12 flex items-center justify-between">
      <div class="text-16 font-bold">{{ $t('settings.autoStartKernel') }}</div>
      <Switch v-model="appConfig.config.autoStartKernel" />
    </div>
    <div class="px-8 py-12 flex items-center justify-between gap-8">
      <div class="text-16 font-bold">{{ $t('settings.realMemoryUsage') }}</div>
      <Switch
        v-model="appSettings.app.kernel.realMemoryUsage"
        :aria-label="$t('settings.realMemoryUsage')"
        class="shrink-0"
      />
    </div>
    <div class="px-8 py-12 flex items-center justify-between gap-8">
      <div class="text-16 font-bold">
        {{ $t('settings.coreLogDays.name') }}
        <span class="font-normal text-12">({{ $t('settings.coreLogDays.tips') }})</span>
      </div>
      <Input
        v-model="coreLogDays"
        type="number"
        editable
        :disabled="coreLogDaysSaving"
        :aria-label="$t('settings.coreLogDays.name')"
        @submit="saveCoreLogDays"
      >
        <template #editable="{ value }">{{ value }}</template>
      </Input>
    </div>
  </Card>
</template>
