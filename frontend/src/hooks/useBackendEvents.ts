import { EventsOn, Notify } from '@/bridge'
import i18n from '@/lang'
import {
  useLogsStore,
  useProfilesStore,
  useRulesetsStore,
  useScheduledTasksStore,
  useSubscribesStore,
} from '@/stores'
import { eventBus, message } from '@/utils'

type RuntimeChangeKind = 'subscription' | 'subscriptions' | 'ruleset' | 'rulesets'
type RuntimeDomain = 'subscriptions' | 'rulesets'
type PendingRuntimeChanges = {
  all: boolean
  ids: Set<string>
}

let backendEventsInitialized = false

export const useBackendEvents = () => {
  const logsStore = useLogsStore()
  const profilesStore = useProfilesStore()
  const rulesetsStore = useRulesetsStore()
  const scheduledTasksStore = useScheduledTasksStore()
  const subscribesStore = useSubscribesStore()
  const { t } = i18n.global

  const pendingRuntimeChanges: Record<RuntimeDomain, PendingRuntimeChanges> = {
    subscriptions: { all: false, ids: new Set<string>() },
    rulesets: { all: false, ids: new Set<string>() },
  }
  let runtimeRefreshTimer: ReturnType<typeof setTimeout> | undefined
  let runtimeRefreshQueue = Promise.resolve()
  let scheduledTaskQueue = Promise.resolve()
  const notifiedTaskRuns = new Map<string, number>()

  const flushRuntimeDomain = async (domain: RuntimeDomain) => {
    const pending = pendingRuntimeChanges[domain]
    const all = pending.all
    const ids = [...pending.ids]
    pending.all = false
    pending.ids.clear()
    if (!all && ids.length === 0) return

    if (domain === 'subscriptions') {
      await subscribesStore.setupSubscribes()
      if (all) {
        eventBus.emit('subscriptionsChange', undefined)
      } else {
        ids.forEach((id) => eventBus.emit('subscriptionChange', { id }))
      }
      return
    }

    await rulesetsStore.setupRulesets()
    if (all) {
      eventBus.emit('rulesetsChange', undefined)
    } else {
      ids.forEach((id) => eventBus.emit('rulesetChange', { id }))
    }
  }

  const flushRuntimeChanges = async () => {
    await Promise.all([
      flushRuntimeDomain('subscriptions').catch((error) => {
        console.error('refresh subscriptions after runtimeChange: ', error)
      }),
      flushRuntimeDomain('rulesets').catch((error) => {
        console.error('refresh rulesets after runtimeChange: ', error)
      }),
    ])
  }

  const scheduleRuntimeRefresh = () => {
    if (runtimeRefreshTimer !== undefined) return
    runtimeRefreshTimer = setTimeout(() => {
      runtimeRefreshTimer = undefined
      runtimeRefreshQueue = runtimeRefreshQueue.then(flushRuntimeChanges).catch((error) => {
        console.error('flush runtimeChange events: ', error)
      })
    }, 50)
  }

  const handleRuntimeChange = (kind?: unknown, id?: unknown) => {
    if (typeof kind !== 'string') return
    const normalizedKind = kind as RuntimeChangeKind
    if (!['subscription', 'subscriptions', 'ruleset', 'rulesets'].includes(normalizedKind)) {
      return
    }

    const domain: RuntimeDomain = normalizedKind.startsWith('subscription')
      ? 'subscriptions'
      : 'rulesets'
    const pending = pendingRuntimeChanges[domain]
    if (normalizedKind === domain) {
      pending.all = true
      pending.ids.clear()
    } else if (typeof id === 'string' && id !== '' && !pending.all) {
      pending.ids.add(id)
    } else {
      return
    }
    scheduleRuntimeRefresh()
  }

  const refreshScheduledTask = async (taskID: string) => {
    await Promise.all([
      scheduledTasksStore.setupScheduledTasks({ logs: false }),
      scheduledTasksStore.refreshScheduledTaskLogs(),
    ])

    const latestLog = scheduledTasksStore.scheduledtasksLogs.find((log) => log.id === taskID)
    if (!latestLog || latestLog.endTime <= (notifiedTaskRuns.get(taskID) || 0)) return
    notifiedTaskRuns.set(taskID, latestLog.endTime)

    const task = scheduledTasksStore.getScheduledTaskById(taskID)
    if (!task?.notification) return

    const successes = latestLog.results.filter((result) => result.ok).length
    const failures = latestLog.results.length - successes
    await Notify(
      t('scheduledtasks.notificationTitle', { name: task.name || latestLog.name || taskID }),
      t('scheduledtasks.notificationBody', { successes, failures }),
    )
  }

  const setupBackendEvents = () => {
    if (backendEventsInitialized) return
    backendEventsInitialized = true

    EventsOn('profileChange', async (data?: { id?: string }) => {
      try {
        await profilesStore.setupProfiles()
        eventBus.emit('profileChange', { id: data?.id || '' })
      } catch (error: any) {
        message.error(error.message || error)
      }
    })

    EventsOn('kernelLog', (log?: unknown) => {
      if (typeof log === 'string') logsStore.recordKernelLog(log)
    })

    EventsOn('runtimeChange', handleRuntimeChange)

    EventsOn('scheduledTaskFinished', (taskID?: unknown) => {
      if (typeof taskID !== 'string' || taskID === '') return
      scheduledTaskQueue = scheduledTaskQueue
        .then(() => refreshScheduledTask(taskID))
        .catch((error) => {
          console.error('refresh scheduled task after completion: ', error)
        })
    })

    EventsOn(
      'kernelCrashed',
      (data?: { pid?: number; reason?: string; phase?: 'runtime' | 'startup' | 'shutdown' }) => {
        if (data?.phase !== 'runtime') return
        const reason =
          typeof data.reason === 'string' && data.reason !== ''
            ? data.reason
            : t('kernel.crashUnknownReason')
        message.error(t('kernel.crashed', { reason }), 5_000)
      },
    )
  }

  return { setupBackendEvents }
}
