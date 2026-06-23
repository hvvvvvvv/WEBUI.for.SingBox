<script setup lang="ts">
import { ref, computed } from 'vue'
import { useI18n } from 'vue-i18n'

import { useScheduledTasksStore } from '@/stores'
import { buildSmartRegExp, formatDate } from '@/utils'

import type { Column } from '@/components/Table/index.vue'

interface Props {
  id?: string
}

const props = withDefaults(defineProps<Props>(), { id: '' })

const { t } = useI18n()
const scheduledTasksStore = useScheduledTasksStore()

const selectedTaskName = ref(scheduledTasksStore.getScheduledTaskById(props.id)?.name)
const keywords = ref('')

const columns: Column[] = [
  {
    title: 'scheduledtasks.name',
    align: 'center',
    key: 'name',
  },
  {
    title: 'scheduledtasks.startTime',
    align: 'center',
    key: 'startTime',
    customRender: ({ value }) => formatDate(value, 'YYYY-MM-DD HH:mm:ss'),
  },
  {
    title: 'scheduledtasks.endTime',
    align: 'center',
    key: 'endTime',
    customRender: ({ value }) => formatDate(value, 'YYYY-MM-DD HH:mm:ss'),
  },
  {
    title: 'scheduledtasks.duration',
    align: 'center',
    key: 'endTime',
    sort: (a, b) => b.endTime - b.startTime - (a.endTime - a.startTime),
    customRender: ({ value, record }) => {
      return ((value - record.startTime) / 1000).toFixed(2) + 's'
    },
  },
  {
    title: 'scheduledtasks.result',
    align: 'center',
    key: 'results',
  },
]

const taskOptions = computed(() =>
  [{ label: 'All', value: '' }].concat(
    ...scheduledTasksStore.scheduledtasks.map((v) => ({
      label: v.name,
      value: v.name,
    })),
  ),
)

const filteredLogs = computed(() => {
  return scheduledTasksStore.scheduledtasksLogs.filter((v) => {
    const p = selectedTaskName.value ? v.name === selectedTaskName.value : true
    const k = buildSmartRegExp(keywords.value, 'i').test(JSON.stringify(v.results))
    return p && k
  })
})

const clearLogs = () => scheduledTasksStore.clearScheduledTaskLogs()

scheduledTasksStore.refreshScheduledTaskLogs(props.id)
</script>

<template>
  <div class="h-full flex flex-col">
    <div class="flex items-center">
      <span class="mr-4">
        {{ t('scheduledtasks.name') }}
        :
      </span>
      <Select v-model="selectedTaskName" :options="taskOptions" size="small" />
      <Input
        v-model="keywords"
        clearable
        size="small"
        :placeholder="t('common.keywords')"
        class="ml-8 flex-1"
      />
      <Button
        v-tips="'common.clear'"
        icon="clear"
        size="small"
        type="text"
        class="ml-8"
        @click="clearLogs"
      />
    </div>

    <Empty v-if="filteredLogs.length === 0" />

    <Table v-else :columns="columns" :data-source="filteredLogs" sort="start" class="mt-8">
      <template #results="{ record }">
        <div class="flex flex-col gap-6 text-left whitespace-normal min-w-240">
          <div
            v-for="item in record.results"
            :key="item.id + item.name + item.result"
            class="flex items-start gap-6"
          >
            <span :style="{ color: item.ok ? 'greenyellow' : 'red' }">●</span>
            <span>
              <span v-if="item.name" class="opacity-70">[{{ item.name }}]</span>
              {{ item.result || '--' }}
            </span>
          </div>
        </div>
      </template>
    </Table>
  </div>
</template>
