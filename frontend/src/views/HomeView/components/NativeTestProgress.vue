<script setup lang="ts">
import type { NativeUrlTestProgress } from '@/hooks/useNativeUrlTest'

defineProps<{ progress: NativeUrlTestProgress }>()
defineEmits<{ cancel: []; dismiss: [] }>()
</script>

<template>
  <div class="native-test-progress rounded-8 p-8 mb-8" role="status" aria-live="polite">
    <div class="flex flex-wrap items-center gap-8">
      <span class="font-bold">{{ progress.tag }}</span>
      <span>{{ $t(`home.controller.testStatus.${progress.status}`) }}</span>
      <span>{{
        $t('home.controller.testCounts', {
          completed: progress.completed,
          total: progress.total,
          success: progress.success,
          failure: progress.failure,
        })
      }}</span>
      <span v-if="progress.status === 'timeout'">
        {{ $t('home.controller.testUnconfirmed', { count: progress.remaining }) }}
      </span>
      <Button
        v-if="progress.status === 'running'"
        type="text"
        size="small"
        class="ml-auto"
        @click="$emit('cancel')"
      >
        {{ $t('home.controller.stopWaiting') }}
      </Button>
      <Button
        v-else
        type="text"
        size="small"
        icon="close"
        class="ml-auto"
        :aria-label="$t('common.close')"
        @click="$emit('dismiss')"
      />
    </div>
    <progress
      :max="progress.total"
      :value="progress.completed"
      :aria-label="$t('home.overview.delayTest')"
    />
    <div
      v-if="progress.status === 'running' || progress.status === 'canceled'"
      class="text-12 opacity-70"
    >
      {{ $t('home.controller.nativeTestCancelHint') }}
    </div>
  </div>
</template>

<style scoped>
.native-test-progress {
  color: var(--card-color);
  background: var(--card-bg);
  border: 1px solid color-mix(in srgb, var(--primary-color) 35%, transparent);
  overflow-wrap: anywhere;
}
progress {
  display: block;
  width: 100%;
  height: 4px;
  margin: 8px 0;
  overflow: hidden;
  border: 0;
  border-radius: 4px;
  background: color-mix(in srgb, var(--primary-color) 12%, transparent);
  accent-color: var(--primary-color);
}
progress::-webkit-progress-bar {
  background: color-mix(in srgb, var(--primary-color) 12%, transparent);
}
progress::-webkit-progress-value {
  background: var(--primary-color);
  transition: width 0.2s;
}
progress::-moz-progress-bar {
  background: var(--primary-color);
}
</style>
