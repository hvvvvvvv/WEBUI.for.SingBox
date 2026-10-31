import { onActivated, onDeactivated, onMounted, onUnmounted, ref, watch } from 'vue'

interface CoreMemoryOptions {
  enabled: () => boolean
  running: () => boolean
  pid: () => number
  load: (signal: AbortSignal) => Promise<bigint>
}

// RSS needs an extra backend request; native status memory remains the default.
export const useCoreMemory = (options: CoreMemoryOptions) => {
  const rss = ref<bigint>()
  const unavailable = ref(false)
  const active = ref(false)
  let generation = 0
  let timer: ReturnType<typeof setTimeout> | undefined
  let request: AbortController | undefined

  const stop = () => {
    generation++
    if (timer !== undefined) clearTimeout(timer)
    timer = undefined
    request?.abort()
    request = undefined
    rss.value = undefined
    unavailable.value = false
  }
  watch(
    [active, options.enabled, options.running, options.pid],
    ([isActive, enabled, running, pid]) => {
      stop()
      if (!isActive || !enabled || !running || pid <= 0) return
      const current = generation
      const sample = async () => {
        const controller = new AbortController()
        request = controller
        try {
          const value = await options.load(controller.signal)
          if (current !== generation || controller.signal.aborted) return
          rss.value = value
          unavailable.value = false
        } catch {
          if (current !== generation || controller.signal.aborted) return
          rss.value = undefined
          unavailable.value = true
        } finally {
          if (current === generation) {
            request = undefined
            timer = setTimeout(() => void sample(), 30_000)
          }
        }
      }
      void sample()
    },
    { immediate: true, flush: 'sync' },
  )
  onMounted(() => {
    active.value = true
  })
  onActivated(() => {
    active.value = true
  })
  onDeactivated(() => {
    active.value = false
  })
  onUnmounted(stop)
  return { rss, unavailable }
}
