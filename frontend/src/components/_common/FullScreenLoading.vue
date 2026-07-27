<script setup lang="ts">
import { onBeforeUnmount, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'

defineProps<{ message: string }>()

const { t } = useI18n()

const blockKeyboardInteraction = (event: KeyboardEvent) => {
  event.preventDefault()
  event.stopImmediatePropagation()
}

onMounted(() => window.addEventListener('keydown', blockKeyboardInteraction, true))
onBeforeUnmount(() => window.removeEventListener('keydown', blockKeyboardInteraction, true))
</script>

<template>
  <Teleport to="body">
    <div
      class="full-screen-loading fixed inset-0 flex items-center justify-center backdrop-blur-sm"
      role="status"
      aria-live="polite"
      aria-busy="true"
      @touchmove.prevent
      @wheel.prevent
    >
      <div
        class="full-screen-loading-content flex flex-col items-center gap-16 px-32 py-24 rounded-8 shadow"
      >
        <Icon icon="loading" :size="28" color="var(--primary-color)" class="rotation" />
        <div class="text-14 text-center">{{ t(message) }}</div>
      </div>
    </div>
  </Teleport>
</template>

<style lang="less" scoped>
.full-screen-loading {
  z-index: 1000000;
  color: var(--color);
  background-color: var(--modal-mask-bg);
  pointer-events: auto;
}

.full-screen-loading-content {
  color: var(--card-color);
  background-color: var(--modal-bg);
}
</style>
