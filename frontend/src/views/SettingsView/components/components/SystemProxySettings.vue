<script lang="ts" setup>
import { useAppSettingsStore } from '@/stores'

const appSettings = useAppSettingsStore()
</script>

<template>
  <div class="px-8 py-12 text-18 font-bold">{{ $t('settings.systemProxy') }}</div>

  <Card>
    <div class="px-8 py-12 flex items-center justify-between">
      <div class="text-16 font-bold">{{ $t('settings.autoSetSystemProxy') }}</div>
      <Switch v-model="appSettings.app.autoSetSystemProxy" />
    </div>
  </Card>
</template>
