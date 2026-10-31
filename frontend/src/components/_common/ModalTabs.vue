<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { useModalLayoutStore } from '@/stores/modalLayout'

const { t } = useI18n()
const layout = useModalLayoutStore()
</script>

<template>
  <div class="modal-tabs" role="tablist" :aria-label="t('settings.modalTabs')">
    <div v-for="item in layout.tabs" :key="item.id" class="modal-tab">
      <Button
        role="tab"
        :aria-selected="item.id === layout.activeTab"
        :type="item.id === layout.activeTab ? 'link' : 'text'"
        class="modal-tab-title"
        @click="item.activate"
        @auxclick.middle.prevent.stop="item.close"
        >{{ t(item.title()) }}</Button
      >
      <Button
        type="text"
        size="small"
        icon="close"
        :aria-label="t('common.close') + ': ' + t(item.title())"
        @click="item.close"
      />
    </div>
  </div>
</template>

<style scoped>
.modal-tabs {
  display: flex;
  padding: 6px 8px;
  overflow-x: auto;
  flex-shrink: 0;
}
.modal-tab {
  display: flex;
  align-items: center;
  min-width: 100px;
  max-width: 240px;
}
.modal-tab-title {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
}
</style>
