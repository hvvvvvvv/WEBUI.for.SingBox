<script setup lang="ts">
import { onMounted, onUnmounted } from 'vue'
import { useModalLayoutStore } from '@/stores/modalLayout'
import ModalTabs from './ModalTabs.vue'

const layout = useModalLayoutStore()
onMounted(() => (layout.hostReady = true))
onUnmounted(() => (layout.hostReady = false))
</script>

<template>
  <div v-show="layout.splitActive" class="app-modal-panel">
    <ModalTabs />
    <div id="app-modal-content" class="relative flex-1 min-h-0 overflow-hidden" />
  </div>
</template>

<style scoped>
.app-modal-panel {
  width: 50%;
  min-width: 0;
  display: flex;
  flex-direction: column;
  border-left: 1px solid var(--card-bg);
}
</style>
