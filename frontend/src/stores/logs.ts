import { defineStore } from 'pinia'
import { computed, ref } from 'vue'

interface TaskLogRecord<T = any> {
  name: string
  startTime: number
  endTime: number
  result: T
}

export interface KernelLogRecord {
  id: number
  message: string
}

const maxKernelLogLines = 1000
let nextKernelLogID = 1

export const useLogsStore = defineStore('logs', () => {
  const kernelLogs = ref<KernelLogRecord[]>([])
  const scheduledtasksLogs = ref<TaskLogRecord[]>([])

  const recordKernelLog = (msg: string) => {
    kernelLogs.value.unshift({ id: nextKernelLogID++, message: msg })
    if (kernelLogs.value.length > maxKernelLogLines) {
      kernelLogs.value.splice(maxKernelLogLines)
    }
  }

  const recordScheduledTasksLog = (log: TaskLogRecord) => scheduledtasksLogs.value.unshift(log)

  const isTasksLogEmpty = computed(() => scheduledtasksLogs.value.length === 0)

  const isEmpty = computed(() => kernelLogs.value.length === 0)

  const clearKernelLog = () => kernelLogs.value.splice(0)

  return {
    recordKernelLog,
    clearKernelLog,
    kernelLogs,
    isEmpty,
    scheduledtasksLogs,
    isTasksLogEmpty,
    recordScheduledTasksLog,
  }
})
