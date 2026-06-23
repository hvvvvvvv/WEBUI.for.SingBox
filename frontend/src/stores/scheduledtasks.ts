import { defineStore } from 'pinia'
import { ref } from 'vue'

import { createRpcClient } from '@/bridge'
import { ScheduledTaskService } from '../../gen/app/v1/app_service_pb'

import type { ScheduledTask } from '@/types/app'
import type { TaskResult } from '../../gen/app/v1/app_service_pb'

interface ScheduledTaskLogRecord {
  id: string
  name: string
  startTime: number
  endTime: number
  results: TaskResult[]
}

const parseList = <T>(items: string[]) => items.map((v) => JSON.parse(v) as T)

export const useScheduledTasksStore = defineStore('scheduledtasks', () => {
  const scheduledtasks = ref<ScheduledTask[]>([])
  const scheduledtasksLogs = ref<ScheduledTaskLogRecord[]>([])
  const service = createRpcClient(ScheduledTaskService)

  const setupScheduledTasks = async (options: { logs?: boolean } = { logs: true }) => {
    const { tasksJson } = await service.listScheduledTasks({})
    scheduledtasks.value = parseList<ScheduledTask>(tasksJson)
    if (options.logs) {
      await refreshScheduledTaskLogs()
    }
  }

  const saveScheduledTasks = async () => {
    const { tasksJson } = await service.saveScheduledTasks({
      tasksJson: scheduledtasks.value.map((v) => JSON.stringify(v)),
    })
    scheduledtasks.value = parseList<ScheduledTask>(tasksJson)
  }

  const addScheduledTask = async (s: ScheduledTask) => {
    const { taskJson } = await service.upsertScheduledTask({ taskJson: JSON.stringify(s) })
    scheduledtasks.value.push(JSON.parse(taskJson))
  }

  const deleteScheduledTask = async (id: string) => {
    await service.deleteScheduledTask({ id })
    const idx = scheduledtasks.value.findIndex((v) => v.id === id)
    idx !== -1 && scheduledtasks.value.splice(idx, 1)
  }

  const editScheduledTask = async (id: string, s: ScheduledTask) => {
    const { taskJson } = await service.upsertScheduledTask({
      taskJson: JSON.stringify({ ...s, id }),
    })
    const item = JSON.parse(taskJson) as ScheduledTask
    const idx = scheduledtasks.value.findIndex((v) => v.id === id)
    if (idx === -1) {
      scheduledtasks.value.push(item)
    } else {
      scheduledtasks.value.splice(idx, 1, item)
    }
  }

  const runScheduledTask = async (id: string): Promise<TaskResult[]> => {
    const { results, endTime } = await service.runScheduledTask({ id })
    const task = getScheduledTaskById(id)
    if (task) {
      task.lastTime = Number(endTime)
    }
    return results
  }

  const refreshScheduledTaskLogs = async (id = '') => {
    const { logs } = await service.listScheduledTaskLogs({ id })
    scheduledtasksLogs.value = logs.map((v) => ({
      id: v.id,
      name: v.name,
      startTime: Number(v.startTime),
      endTime: Number(v.endTime),
      results: v.results,
    }))
  }

  const clearScheduledTaskLogs = async () => {
    await service.clearScheduledTaskLogs({})
    scheduledtasksLogs.value.splice(0)
  }

  const nextScheduledTaskRuns = async (cron: string, count = 20) => {
    const { times } = await service.nextScheduledTaskRuns({ cron, count })
    return times.map((v) => Number(v))
  }

  const getScheduledTaskById = (id: string) => scheduledtasks.value.find((v) => v.id === id)

  return {
    scheduledtasks,
    scheduledtasksLogs,
    setupScheduledTasks,
    saveScheduledTasks,
    addScheduledTask,
    editScheduledTask,
    deleteScheduledTask,
    getScheduledTaskById,
    runScheduledTask,
    refreshScheduledTaskLogs,
    clearScheduledTaskLogs,
    nextScheduledTaskRuns,
  }
})
