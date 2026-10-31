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
    class="settings-tabs h-full"
  >
    <template #extra>
      <Button type="text" @click="appStore.showAbout = true">
        {{ t('router.about') }}
      </Button>
    </template>
  </Tabs>
</template>

<style scoped>
.settings-tabs {
  min-width: 0;
  min-height: 0;
}
.settings-tabs > :deep(.gui-tabs-tab + div) {
  min-width: 0;
  min-height: 0;
}
@media (max-width: 700px) {
  .settings-tabs {
    flex-direction: column;
  }
  .settings-tabs > :deep(.gui-tabs-tab) {
    width: 100% !important;
    min-width: 0;
    flex: none;
    flex-direction: row;
    flex-wrap: nowrap;
    justify-content: flex-start;
    gap: 4px;
    overflow-x: auto;
    overscroll-behavior-x: contain;
    padding: 4px 0 8px;
    margin-bottom: 8px;
  }
  .settings-tabs > :deep(.gui-tabs-tab > .gui-button) {
    flex: 0 0 auto;
    white-space: nowrap;
  }
  .settings-tabs > :deep(.gui-tabs-tab + div) {
    width: 100% !important;
    flex: 1;
  }
}
</style>
