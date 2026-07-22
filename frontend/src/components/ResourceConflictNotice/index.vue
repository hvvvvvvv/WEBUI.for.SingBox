<script setup lang="ts">
import { useI18n } from 'vue-i18n'

import Button from '@/components/Button/index.vue'

defineProps<{
  kind: 'changed' | 'deleted'
  loading?: boolean
}>()

defineEmits<{
  reload: []
}>()

const { t } = useI18n()
</script>

<template>
  <div class="resource-conflict-notice flex items-center gap-12 rounded-6 p-12 mb-12 text-13">
    <span class="mr-auto">
      {{ t(kind === 'deleted' ? 'common.resourceDeleted' : 'common.resourceConflict') }}
    </span>
    <Button v-if="kind === 'changed'" type="link" :loading="loading" @click="$emit('reload')">
      {{ t('common.loadLatest') }}
    </Button>
  </div>
</template>

<style scoped lang="less">
.resource-conflict-notice {
  color: var(--primary-color);
  border: 1px solid color-mix(in srgb, var(--primary-color) 38%, transparent);
  background: color-mix(in srgb, var(--primary-color) 9%, transparent);
}
</style>
