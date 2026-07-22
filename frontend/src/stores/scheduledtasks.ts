import { defineStore } from 'pinia'
import { reactive, ref } from 'vue'

import { createRpcClient } from '@/bridge'
import { ScheduledTaskService } from '../../gen/app/v1/scheduled_task_service_pb'
import {
  applyMutationState,
  applyResourceSnapshot,
  createLocalResourceState,
  expectedItemRevision,
  expectedOrderRevision,
} from './resourceSync'

import type { ScheduledTask } from '@/types/app'
import type { ExpectedRevision } from '../../gen/common/v1/sync_pb'
import type { TaskLog, TaskResult } from '../../gen/app/v1/task_pb'

type Revision = Pick<ExpectedRevision, 'instanceId' | 'revision'>

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
  const resourceState = reactive(createLocalResourceState())
  const service = createRpcClient(ScheduledTaskService)
  let setupRequestID = 0
  let latestAppliedSetupRequestID = 0

  const parseLogs = (logs: TaskLog[]) =>
    logs.map((v) => ({
      id: v.id,
      name: v.name,
      startTime: Number(v.startTime),
      endTime: Number(v.endTime),
      results: v.results,
    }))

  const upsertScheduledTask = (item: ScheduledTask) => {
    const idx = scheduledtasks.value.findIndex((value) => value.id === item.id)
    if (idx === -1) scheduledtasks.value.push(item)
    else scheduledtasks.value.splice(idx, 1, item)
  }

  const setupScheduledTasks = async (options: { logs?: boolean } = { logs: true }) => {
    const requestID = ++setupRequestID
    const { tasksJson, state } = await service.listScheduledTasks({})
    const tasks = parseList<ScheduledTask>(tasksJson)
    let logs: ScheduledTaskLogRecord[] | undefined
    if (options.logs) {
      const response = await service.listScheduledTaskLogs({ id: '' })
      logs = parseLogs(response.logs)
    }
    if (
      state?.instanceId &&
      resourceState.instanceId &&
      state.instanceId !== resourceState.instanceId &&
      requestID < latestAppliedSetupRequestID
    ) {
      return
    }
    if (applyResourceSnapshot(resourceState, state)) {
      latestAppliedSetupRequestID = Math.max(latestAppliedSetupRequestID, requestID)
      scheduledtasks.value = tasks
      if (logs) scheduledtasksLogs.value = logs
    }
  }

  const applyScheduledTaskMutation = async (
    state: Parameters<typeof applyMutationState>[1],
    options: { id?: string; deleted?: boolean } = {},
  ) => {
    if (applyMutationState(resourceState, state, options)) return true
    if (state?.instanceId && state.instanceId !== resourceState.instanceId) {
      await setupScheduledTasks({ logs: false })
    }
    return false
  }

  const reorderScheduledTasks = async (
    ids: string[],
    revision: Revision = expectedOrderRevision(resourceState),
    fallbackIDs: string[] = [],
  ) => {
    try {
      const { ids: orderedIDs, state } = await service.reorderScheduledTasks({
        ids,
        expectedOrderRevision: revision,
      })
      if (!(await applyScheduledTaskMutation(state))) return
      const byId = new Map(scheduledtasks.value.map((item) => [item.id, item]))
      const ordered = orderedIDs.flatMap((id) => byId.get(id) || [])
      if (ordered.length !== scheduledtasks.value.length) {
        await setupScheduledTasks({ logs: false })
        return
      }
      scheduledtasks.value.splice(0, scheduledtasks.value.length, ...ordered)
    } catch (error) {
      try {
        await setupScheduledTasks({ logs: false })
      } catch {
        const byId = new Map(scheduledtasks.value.map((item) => [item.id, item]))
        const fallback = fallbackIDs.flatMap((id) => byId.get(id) || [])
        if (fallback.length === scheduledtasks.value.length) {
          scheduledtasks.value.splice(0, scheduledtasks.value.length, ...fallback)
        }
      }
      throw error
    }
  }

  const addScheduledTask = async (s: ScheduledTask) => {
    const { taskJson, state } = await service.createScheduledTask({
      taskJson: JSON.stringify(s),
    })
    if (!(await applyScheduledTaskMutation(state, { id: s.id }))) return
    upsertScheduledTask(JSON.parse(taskJson))
  }

  const deleteScheduledTask = async (
    id: string,
    revision: Revision = expectedItemRevision(resourceState, id),
  ) => {
    const { state } = await service.deleteScheduledTask({ id, expectedRevision: revision })
    if (!(await applyScheduledTaskMutation(state, { id, deleted: true }))) return
    const idx = scheduledtasks.value.findIndex((v) => v.id === id)
    if (idx !== -1) scheduledtasks.value.splice(idx, 1)
  }

  const editScheduledTask = async (
    id: string,
    s: ScheduledTask,
    revision: Revision = expectedItemRevision(resourceState, id),
  ) => {
    const { taskJson, state } = await service.updateScheduledTask({
      taskJson: JSON.stringify({ ...s, id }),
      expectedRevision: revision,
    })
    if (!(await applyScheduledTaskMutation(state, { id }))) return
    upsertScheduledTask(JSON.parse(taskJson) as ScheduledTask)
  }

  const runScheduledTask = async (id: string): Promise<TaskResult[]> => {
    const { results, endTime, state } = await service.runScheduledTask({ id })
    if (await applyScheduledTaskMutation(state)) {
      const task = getScheduledTaskById(id)
      if (task) task.lastTime = Number(endTime)
    }
    return results
  }

  const refreshScheduledTaskLogs = async (id = '') => {
    const { logs } = await service.listScheduledTaskLogs({ id })
    scheduledtasksLogs.value = parseLogs(logs)
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
  const getScheduledTaskRevision = (id: string) => expectedItemRevision(resourceState, id)
  const getScheduledTasksOrderRevision = () => expectedOrderRevision(resourceState)

  return {
    scheduledtasks,
    scheduledtasksLogs,
    resourceState,
    setupScheduledTasks,
    reorderScheduledTasks,
    addScheduledTask,
    editScheduledTask,
    deleteScheduledTask,
    getScheduledTaskById,
    getScheduledTaskRevision,
    getScheduledTasksOrderRevision,
    runScheduledTask,
    refreshScheduledTaskLogs,
    clearScheduledTaskLogs,
    nextScheduledTaskRuns,
  }
})
