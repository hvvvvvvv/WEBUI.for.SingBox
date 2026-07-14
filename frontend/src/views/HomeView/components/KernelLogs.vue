<script setup lang="ts">
import { computed, h, ref, watch, withDirectives } from 'vue'
import { useI18n } from 'vue-i18n'

import { ClipboardSetText } from '@/bridge'
import vTips from '@/directives/tips'
import { useLogsStore } from '@/stores'
import { message } from '@/utils'

import Button from '@/components/Button/index.vue'

const logsStore = useLogsStore()
const { t } = useI18n()

const selectedLogIDs = ref<number[]>([])
const displayedLogs = computed(() => [...logsStore.kernelLogs].reverse())
const allSelected = computed(
  () =>
    displayedLogs.value.length > 0 &&
    displayedLogs.value.every((log) => selectedLogIDs.value.includes(log.id)),
)
const partiallySelected = computed(
  () => selectedLogIDs.value.length > 0 && !allSelected.value,
)

watch(
  () => logsStore.kernelLogs.map((log) => log.id),
  (ids) => {
    const available = new Set(ids)
    selectedLogIDs.value = selectedLogIDs.value.filter((id) => available.has(id))
  },
)

const toggleLog = (id: number) => {
  const index = selectedLogIDs.value.indexOf(id)
  if (index === -1) {
    selectedLogIDs.value.push(id)
  } else {
    selectedLogIDs.value.splice(index, 1)
  }
}

const toggleAll = () => {
  selectedLogIDs.value = allSelected.value ? [] : displayedLogs.value.map((log) => log.id)
}

const copySelectedLogs = async () => {
  const selected = new Set(selectedLogIDs.value)
  const content = displayedLogs.value
    .filter((log) => selected.has(log.id))
    .map((log) => log.message)
    .join('\n')
  if (!content) return
  await ClipboardSetText(content)
  message.success('common.success')
}

type KernelLogLevel = 'trace' | 'debug' | 'info' | 'warn' | 'error' | 'fatal' | 'panic'

const logLevelPattern = /(?:^|\s)(TRACE|DEBUG|INFO|WARN(?:ING)?|ERROR|FATAL|PANIC)(?=\[|\s|:)/i

const getLogLevel = (message: string): KernelLogLevel | undefined => {
  const level = message.match(logLevelPattern)?.[1]?.toLowerCase()
  if (level === 'warning') return 'warn'
  return level as KernelLogLevel | undefined
}

const getLogColor = (message: string) => {
  const level = getLogLevel(message)
  if (level === 'trace' || level === 'debug') return 'var(--level-0-color)'
  if (level === 'info') return 'var(--level-1-color)'
  if (level === 'warn') return 'var(--level-2-color)'
  if (level === 'error') return 'var(--level-3-color)'
  if (level === 'fatal' || level === 'panic') return 'var(--level-4-color)'
  return 'var(--color)'
}

const modalSlots = {
  toolbar: () =>
    withDirectives(
      h(Button, {
        type: 'text',
        icon: 'file',
        onClick: copySelectedLogs,
      }),
      [[vTips, 'common.copySelected']],
    ),
}

defineExpose({ modalSlots })
</script>

<template>
  <div class="h-full overflow-y-auto">
    <div class="flex items-center px-6 py-4">
      <input
        v-tips="allSelected ? 'common.deselectAll' : 'common.selectAll'"
        type="checkbox"
        class="kernel-log-checkbox shrink-0"
        :class="logsStore.isEmpty ? 'cursor-not-allowed' : 'cursor-pointer'"
        :checked="allSelected"
        :indeterminate="partiallySelected"
        :disabled="logsStore.isEmpty"
        :aria-label="t(allSelected ? 'common.deselectAll' : 'common.selectAll')"
        @change="toggleAll"
      />
    </div>
    <Empty v-if="logsStore.isEmpty" description="home.overview.noLogs" />
    <template v-else>
      <div
        v-for="log in displayedLogs"
        :key="log.id"
        :style="{
          background: 'var(--card-bg)',
          color: getLogColor(log.message),
        }"
        :class="{ selected: selectedLogIDs.includes(log.id) }"
        class="kernel-log flex items-start gap-8 text-12 my-4 py-4 px-6 rounded-4"
      >
        <input
          type="checkbox"
          class="kernel-log-checkbox shrink-0 mt-2 cursor-pointer"
          :checked="selectedLogIDs.includes(log.id)"
          :aria-label="t('common.select')"
          @change="toggleLog(log.id)"
        />
        <span class="select-text whitespace-pre-wrap break-all">{{ log.message }}</span>
      </div>
    </template>
  </div>
</template>

<style lang="less" scoped>
.kernel-log {
  border: 1px solid transparent;
  transition: border-color 0.15s ease;

  &.selected {
    border-color: var(--primary-color);
  }
}

.kernel-log-checkbox {
  width: 14px;
  height: 14px;
  accent-color: var(--primary-color);
}
</style>
