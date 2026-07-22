import { EventsOn, Notify, onWebSocketConnected } from '@/bridge'
import i18n from '@/lang'
import {
  useLogsStore,
  useProfilesStore,
  useRulesetsStore,
  useScheduledTasksStore,
  useSubscribesStore,
  eventRevisionApplied,
  type ResourceChangedEvent,
  type ResourceDomain,
} from '@/stores'
import { message } from '@/utils'

type ConfigDomain = ResourceDomain

let backendEventsInitialized = false

export const useBackendEvents = () => {
  const logsStore = useLogsStore()
  const profilesStore = useProfilesStore()
  const rulesetsStore = useRulesetsStore()
  const scheduledTasksStore = useScheduledTasksStore()
  const subscribesStore = useSubscribesStore()
  const { t } = i18n.global

  let scheduledTaskQueue = Promise.resolve()
  let configRefreshTimer: ReturnType<typeof setTimeout> | undefined
  let configSyncPromise: Promise<void> | undefined
  const pendingConfigDomains = new Set<ConfigDomain>()
  const pendingResourceEvents = new Map<ConfigDomain, ResourceChangedEvent>()
  const notifiedTaskRuns = new Map<string, number>()

  const refreshConfigDomain = async (domain: ConfigDomain) => {
    switch (domain) {
      case 'profiles':
        await profilesStore.setupProfiles()
        break
      case 'subscriptions':
        await subscribesStore.setupSubscribes()
        break
      case 'rulesets':
        await rulesetsStore.setupRulesets()
        break
      case 'scheduledTasks':
        await scheduledTasksStore.setupScheduledTasks()
        break
    }
  }

  const drainConfigSync = async () => {
    const failures = new Map<ConfigDomain, unknown>()
    while (pendingConfigDomains.size > 0) {
      const domains = [...pendingConfigDomains]
      pendingConfigDomains.clear()
      const results = await Promise.allSettled(domains.map(refreshConfigDomain))
      results.forEach((result, index) => {
        const domain = domains[index]!
        if (result.status === 'rejected') failures.set(domain, result.reason)
        else failures.delete(domain)
      })
    }
    const firstError = failures.values().next()
    if (!firstError.done) throw firstError.value
  }

  const syncConfigStores = (
    domains: ConfigDomain[] = ['profiles', 'subscriptions', 'rulesets', 'scheduledTasks'],
  ) => {
    domains.forEach((domain) => pendingConfigDomains.add(domain))
    if (!configSyncPromise) {
      configSyncPromise = drainConfigSync().finally(() => {
        configSyncPromise = undefined
        if (pendingConfigDomains.size > 0) {
          void syncConfigStores([]).catch((error) => {
            console.error('refresh configuration queued during synchronization: ', error)
          })
        }
      })
    }
    return configSyncPromise
  }

  const resourceState = (domain: ConfigDomain) => {
    if (domain === 'profiles') return profilesStore.resourceState
    if (domain === 'subscriptions') return subscribesStore.resourceState
    if (domain === 'rulesets') return rulesetsStore.resourceState
    return scheduledTasksStore.resourceState
  }

  const scheduleResourceRefresh = (event: ResourceChangedEvent) => {
    if (eventRevisionApplied(resourceState(event.domain), event)) return
    const pending = pendingResourceEvents.get(event.domain)
    if (
      !pending ||
      pending.instanceId !== event.instanceId ||
      event.stateRevision > pending.stateRevision
    ) {
      pendingResourceEvents.set(event.domain, event)
    }
    if (configRefreshTimer !== undefined) return
    configRefreshTimer = setTimeout(() => {
      configRefreshTimer = undefined
      const events = [...pendingResourceEvents.values()]
      pendingResourceEvents.clear()
      const domains = events
        .filter((item) => !eventRevisionApplied(resourceState(item.domain), item))
        .map((item) => item.domain)
      if (domains.length === 0) return
      void syncConfigStores(domains)
        .then(() => {
          events.forEach((item) => {
            if (!eventRevisionApplied(resourceState(item.domain), item)) {
              scheduleResourceRefresh(item)
            }
          })
        })
        .catch((error: any) => {
          console.error('refresh configuration after resourceChanged: ', error)
          message.error(error.message || error)
        })
    }, 50)
  }

  const refreshScheduledTask = async (taskID: string, notify: boolean) => {
    await scheduledTasksStore.refreshScheduledTaskLogs()

    const latestLog = scheduledTasksStore.scheduledtasksLogs.find((log) => log.id === taskID)
    if (!latestLog || latestLog.endTime <= (notifiedTaskRuns.get(taskID) || 0)) return
    notifiedTaskRuns.set(taskID, latestLog.endTime)

    const task = scheduledTasksStore.getScheduledTaskById(taskID)
    if (!notify) return

    const successes = latestLog.results.filter((result) => result.ok).length
    const failures = latestLog.results.length - successes
    await Notify(
      t('scheduledtasks.notificationTitle', { name: task?.name || latestLog.name || taskID }),
      t('scheduledtasks.notificationBody', { successes, failures }),
    )
  }

  const setupBackendEvents = () => {
    if (backendEventsInitialized) return
    backendEventsInitialized = true

    onWebSocketConnected(() => {
      void syncConfigStores().catch((error) => {
        console.error('refresh configuration after websocket connection: ', error)
      })
    })

    EventsOn('kernelLog', (log?: unknown) => {
      if (typeof log === 'string') logsStore.recordKernelLog(log)
    })

    EventsOn('resourceChanged', (event?: unknown) => {
      if (!event || typeof event !== 'object') return
      const value = event as Partial<ResourceChangedEvent>
      if (!['profiles', 'subscriptions', 'rulesets', 'scheduledTasks'].includes(value.domain || ''))
        return
      if (
        !value.instanceId ||
        typeof value.stateRevision !== 'number' ||
        !['upsert', 'delete', 'reorder', 'runtime'].includes(value.operation || '')
      ) {
        return
      }
      scheduleResourceRefresh({
        domain: value.domain as ResourceDomain,
        operation: value.operation as ResourceChangedEvent['operation'],
        ids: Array.isArray(value.ids)
          ? value.ids.filter((id): id is string => typeof id === 'string')
          : [],
        instanceId: value.instanceId,
        stateRevision: value.stateRevision,
      })
    })

    EventsOn('scheduledTaskFinished', (taskID?: unknown, notify?: unknown) => {
      if (typeof taskID !== 'string' || taskID === '') return
      scheduledTaskQueue = scheduledTaskQueue
        .then(() => refreshScheduledTask(taskID, notify === true))
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

    EventsOn('kernelAutoRestartFailed', () => {
      message.error('kernel.restartFailedCheckLogs', 5_000)
    })
  }

  return { setupBackendEvents, syncConfigStores }
}
