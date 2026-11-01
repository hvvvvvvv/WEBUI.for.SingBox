import { onActivated, onBeforeUnmount, onDeactivated, onMounted } from 'vue'

/** Cached pages retain their state, but only visible pages need live snapshots. */
export const useActiveSubscription = (subscribe: () => () => void) => {
  let unsubscribe: (() => void) | undefined
  const start = () => {
    unsubscribe ??= subscribe()
  }
  const stop = () => {
    unsubscribe?.()
    unsubscribe = undefined
  }
  onMounted(start)
  onActivated(start)
  onDeactivated(stop)
  onBeforeUnmount(stop)
}
