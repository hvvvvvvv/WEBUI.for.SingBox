import { defineStore } from 'pinia'
import { computed, ref } from 'vue'

interface KernelLogRecord {
  id: number
  message: string
}

const maxKernelLogLines = 1000
let nextKernelLogID = 1

export const useLogsStore = defineStore('logs', () => {
  const kernelLogs = ref<KernelLogRecord[]>([])

  const recordKernelLog = (msg: string) => {
    kernelLogs.value.unshift({ id: nextKernelLogID++, message: msg })
    if (kernelLogs.value.length > maxKernelLogLines) {
      kernelLogs.value.splice(maxKernelLogLines)
    }
  }

  const isEmpty = computed(() => kernelLogs.value.length === 0)

  const clearKernelLog = () => kernelLogs.value.splice(0)

  return {
    recordKernelLog,
    clearKernelLog,
    kernelLogs,
    isEmpty,
  }
})
