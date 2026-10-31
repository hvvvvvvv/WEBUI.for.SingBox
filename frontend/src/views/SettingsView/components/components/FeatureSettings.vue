<script lang="ts" setup>
import { useAppSettingsStore } from '@/stores'
const appSettings = useAppSettingsStore()
</script>

<template>
  <div class="px-8 py-12 text-18 font-bold">{{ $t('settings.features') }}</div>

  <Card class="feature-settings">
    <div class="px-8 py-12 flex items-center justify-between">
      <div class="text-16 font-bold">{{ $t('settings.debugOutline') }}</div>
      <Switch v-model="appSettings.app.debugOutline" />
    </div>
    <div class="px-8 py-12 flex items-center justify-between">
      <div class="text-16 font-bold">{{ $t('settings.debugNoAnimation') }}</div>
      <Switch v-model="appSettings.app.debugNoAnimation" />
    </div>
    <div class="px-8 py-12 flex items-center justify-between">
      <div class="text-16 font-bold">{{ $t('settings.debugNoRounded') }}</div>
      <Switch v-model="appSettings.app.debugNoRounded" />
    </div>
    <div class="px-8 py-12 flex items-center justify-between">
      <div class="text-16 font-bold">{{ $t('settings.debugUsePointer') }}</div>
      <Switch
        v-model="appSettings.app.debugUsePointer"
        :aria-label="$t('settings.debugUsePointer')"
      />
    </div>
    <div class="px-8 py-12 flex items-center justify-between">
      <div>
        <div class="text-16 font-bold">{{ $t('settings.debugModalSideBySide') }}</div>
        <div class="feature-hint">{{ $t('settings.modalSideBySideHint') }}</div>
      </div>
      <Switch
        v-model="appSettings.app.debugModalSideBySide"
        :aria-label="$t('settings.debugModalSideBySide')"
      />
    </div>
  </Card>
</template>

<style scoped lang="less">
.feature-settings :deep(.px-8.py-12) {
  gap: 12px;
}
.feature-settings :deep(.gui-switch) {
  flex-shrink: 0;
  min-width: 50px;
}
.feature-hint {
  font-size: 12px;
  opacity: 0.7;
  margin-top: 4px;
}
</style>
