<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'

import { useAppStore } from '@/stores'

import GeneralSettings from './components/GeneralSettings.vue'
import CoreSettings from './components/CoreSettings.vue'
import PluginSettings from './components/PluginSettings.vue'
import SecuritySettings from './components/SecuritySettings.vue'

const settings = [
  {
    key: 'general',
    tab: 'settings.general',
    component: GeneralSettings,
  },
  {
    key: 'security',
    tab: 'auth.security',
    component: SecuritySettings,
  },
  {
    key: 'kernel',
    tab: 'router.kernel',
    component: CoreSettings,
  },
  {
    key: 'plugins',
    tab: 'router.plugins',
    component: PluginSettings,
  },
] as const

const activeKey = ref(settings[0].key)

const { t } = useI18n()
const appStore = useAppStore()
</script>

<template>
  <Tabs
    v-model:active-key="activeKey"
    :items="settings"
    tab-width="15%"
    content-width="85%"
    class="settings-layout h-full"
  >
    <template #extra>
      <Button type="text" @click="appStore.showAbout = true">
        {{ t('router.about') }}
      </Button>
    </template>
  </Tabs>
</template>

<style scoped>
@media (max-width: 700px) {
  .settings-layout {
    flex-direction: column;
    min-height: 0;
  }
  .settings-layout :deep(.gui-tabs-tab) {
    width: 100% !important;
    flex-direction: row;
    flex-shrink: 0;
    overflow-x: auto;
    margin-bottom: 8px;
  }
  .settings-layout :deep(.gui-tabs-tab > .gui-button) {
    flex: 0 0 auto;
    min-height: 44px;
  }
  .settings-layout :deep(.gui-tabs-content) {
    width: 100% !important;
    flex: 1;
    min-width: 0;
    min-height: 0;
  }
  .settings-layout :deep(.gui-tabs-content > .pr-20) {
    padding-right: 4px;
  }
  .settings-layout :deep(.gui-switch) {
    flex-shrink: 0;
  }
  .settings-layout :deep(.personalization-settings .gui-radio) {
    max-width: 100%;
    overflow-x: auto;
  }
  .settings-layout :deep(.personalization-settings .gui-input) {
    max-width: 100%;
  }
  .settings-layout :deep(.network-actions > .gui-button) {
    min-height: 44px;
    flex: 1 1 120px;
  }
}
</style>
