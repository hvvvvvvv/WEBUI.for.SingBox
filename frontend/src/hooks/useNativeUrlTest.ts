import { onScopeDispose, shallowRef, watch } from 'vue'

import { getNativeApiGeneration, onOutbounds, urlTest } from '@/api/kernel'
import type { Group, GroupItem } from '@/types/kernel'

const resultWaitMs = 10_000
const nativeTestConcurrency = 10

interface NativeUrlTestTimeout {
  tag: string
  isGroup: boolean
  remainingCount: number
}

interface Options {
  groups: () => Group[]
  outbounds: () => GroupItem[]
  pid: () => number
  running: () => boolean
  onTimeout: (timeout: NativeUrlTestTimeout) => void
}

export interface NativeUrlTestProgress {
  id: number
  tag: string
  total: number
  completed: number
  success: number
  failure: number
  remaining: number
  status: 'running' | 'completed' | 'timeout' | 'canceled' | 'error'
}

type History = Pick<GroupItem, 'urlTestTime' | 'urlTestDelay'>
type Task = {
  id: number
  tag: string
  isGroup: boolean
  pid: number
  generation: number
  targets: Map<string, Set<string>>
  baselines: Map<string, History>
  pending: Set<string>
  observed: Map<string, boolean>
  results: Map<string, boolean>
  accepted: boolean
  timer?: ReturnType<typeof setTimeout>
}

export const useNativeUrlTest = (options: Options) => {
  const tasks = shallowRef(new Map<number, Task>())
  const progress = shallowRef<NativeUrlTestProgress[]>([])
  const summaryTimers = new Map<number, ReturnType<typeof setTimeout>>()
  let nextTaskId = 0
  let disposed = false

  const publish = (task: Task, status: NativeUrlTestProgress['status'] = 'running') => {
    const success = [...task.results.values()].filter(Boolean).length
    const snapshot: NativeUrlTestProgress = {
      id: task.id,
      tag: task.tag,
      total: task.baselines.size,
      completed: task.results.size,
      success,
      failure: task.results.size - success,
      remaining: task.pending.size,
      status,
    }
    const index = progress.value.findIndex((entry) => entry.id === task.id)
    const next = [...progress.value]
    if (index === -1) next.push(snapshot)
    else next[index] = snapshot
    progress.value = next
  }
  const notify = () => {
    tasks.value = new Map(tasks.value)
    for (const task of tasks.value.values()) publish(task)
  }
  const dismiss = (id: number) => {
    clearTimeout(summaryTimers.get(id))
    summaryTimers.delete(id)
    progress.value = progress.value.filter((entry) => entry.id !== id)
  }
  const finish = (task: Task, status: NativeUrlTestProgress['status'] = 'completed') => {
    if (tasks.value.get(task.id) !== task) return
    clearTimeout(task.timer)
    publish(task, status)
    tasks.value.delete(task.id)
    summaryTimers.set(
      task.id,
      setTimeout(() => dismiss(task.id), 3_000),
    )
    notify()
  }
  const clear = () => {
    for (const task of tasks.value.values()) clearTimeout(task.timer)
    for (const timer of summaryTimers.values()) clearTimeout(timer)
    summaryTimers.clear()
    tasks.value = new Map()
    progress.value = []
  }
  const isCurrent = (task: Task) =>
    !disposed &&
    tasks.value.get(task.id) === task &&
    task.generation === getNativeApiGeneration() &&
    task.pid === options.pid() &&
    options.running()

  const unregister = onOutbounds((snapshot) => {
    for (const task of tasks.value.values()) {
      if (!isCurrent(task)) {
        finish(task, 'canceled')
        continue
      }
      for (const item of snapshot.outbounds) {
        const baseline = task.baselines.get(item.tag)
        if (!baseline || !task.pending.has(item.tag)) continue
        // Native failures delete history instead of publishing a failed-at time.
        // A previously empty history staying empty is not completion evidence.
        const deleted = baseline.urlTestTime > 0n && item.urlTestTime === 0n
        const updated =
          item.urlTestTime > 0n &&
          (item.urlTestTime > baseline.urlTestTime ||
            (item.urlTestTime === baseline.urlTestTime &&
              item.urlTestDelay !== baseline.urlTestDelay))
        if (!updated && !deleted) continue
        const successful = !deleted
        task.observed.set(item.tag, successful)
        if (task.accepted) {
          task.results.set(item.tag, successful)
          task.pending.delete(item.tag)
        }
      }
      if (task.accepted && task.pending.size === 0) finish(task)
    }
    notify()
  }, false)

  const stopWatching = watch([options.pid, options.running], clear, { flush: 'sync' })
  onScopeDispose(() => {
    disposed = true
    clear()
    stopWatching()
    unregister()
  })

  const start = async (tag: string): Promise<boolean> => {
    if (disposed || !options.running() || options.pid() <= 0) return false
    const groups = options.groups()
    const groupMap = new Map(groups.map((group) => [group.tag, group]))
    const targets = new Map<string, Set<string>>()
    const visiting = new Set<string>()
    const collect = (target: string): Set<string> => {
      if (visiting.has(target)) return new Set()
      const cached = targets.get(target)
      if (cached) return cached
      const group = groupMap.get(target)
      const leaves = new Set<string>()
      if (group) {
        visiting.add(target)
        for (const item of group.items) {
          for (const leaf of collect(item.tag)) leaves.add(leaf)
        }
        visiting.delete(target)
      } else {
        leaves.add(target)
      }
      targets.set(target, leaves)
      return leaves
    }
    const leaves = collect(tag)
    if (leaves.size === 0) return false
    for (const task of tasks.value.values()) {
      if (!isCurrent(task)) {
        finish(task, 'canceled')
        continue
      }
      // Reserve the whole batch until it ends, including already updated members.
      if ([...leaves].some((leaf) => task.baselines.has(leaf))) return false
    }

    const histories = new Map(
      groups.flatMap((group) => group.items.map((item) => [item.tag, item])),
    )
    for (const item of options.outbounds()) histories.set(item.tag, item)
    const baselines = new Map<string, History>()
    for (const leaf of leaves) {
      const item = histories.get(leaf)
      baselines.set(leaf, {
        urlTestTime: item?.urlTestTime ?? 0n,
        urlTestDelay: item?.urlTestDelay ?? 0,
      })
    }
    const task: Task = {
      id: ++nextTaskId,
      tag,
      isGroup: groupMap.has(tag),
      pid: options.pid(),
      generation: getNativeApiGeneration(),
      targets,
      baselines,
      pending: new Set(leaves),
      observed: new Map(),
      results: new Map(),
      accepted: false,
    }
    tasks.value.set(task.id, task)
    notify()
    const batches = task.isGroup ? Math.max(1, Math.ceil(leaves.size / nativeTestConcurrency)) : 1
    task.timer = setTimeout(() => {
      const current = isCurrent(task)
      const remainingCount = task.pending.size
      finish(task, 'timeout')
      if (current) options.onTimeout({ tag, isGroup: task.isGroup, remainingCount })
    }, resultWaitMs * batches)

    try {
      await urlTest(tag)
      if (!isCurrent(task)) {
        finish(task, 'canceled')
        return false
      }
      task.accepted = true
      for (const [leaf, successful] of task.observed) {
        task.results.set(leaf, successful)
        task.pending.delete(leaf)
      }
      if (task.pending.size === 0) finish(task)
      else notify()
      return true
    } catch (error) {
      const current = isCurrent(task)
      finish(task, 'error')
      if (current) throw error
      return false
    }
  }

  const isLoading = (tag: string) => {
    for (const task of tasks.value.values()) {
      if (!isCurrent(task)) continue
      const leaves = task.targets.get(tag)
      if (leaves && [...leaves].some((leaf) => task.pending.has(leaf))) return true
    }
    return false
  }

  // The core uses its own context after accepting URLTest. Stop observing this
  // batch; do not claim that canceling the UI stops already-submitted probes.
  const cancel = (id: number) => {
    const task = tasks.value.get(id)
    if (task) finish(task, 'canceled')
  }

  return { start, isLoading, progress, cancel, dismiss, clear }
}
