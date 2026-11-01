// @vitest-environment jsdom
import { createApp, defineComponent, h, KeepAlive, nextTick, ref } from 'vue'
import { expect, it } from 'vitest'
import { useActiveSubscription } from '@/hooks/useActiveSubscription'
import { NativeSubscription } from '@/api/nativeSubscription'

it('aborts the hidden cached page stream and opens one fresh stream on return', async () => {
  const requests: AbortSignal[] = []
  const received: number[] = []
  const channel = new NativeSubscription(async function* (signal) {
    requests.push(signal)
    yield requests.length
    await new Promise<void>((resolve) =>
      signal.addEventListener('abort', () => resolve(), { once: true }),
    )
    yield 999 // A late response from the old stream must never reach the returning page.
  })
  channel.setInstance(new AbortController().signal)
  const visible = ref(true)
  const page = defineComponent({
    setup() {
      useActiveSubscription(() => channel.subscribe((value) => received.push(value)))
      return () => h('div', 'page')
    },
  })
  const other = defineComponent({ render: () => h('div', 'other') })
  const host = document.createElement('div')
  const app = createApp({
    render: () => h(KeepAlive, null, () => (visible.value ? h(page) : h(other))),
  })
  try {
    app.mount(host)
    await nextTick()
    await Promise.resolve()
    expect(requests).toHaveLength(1)
    expect(received).toEqual([1])
    visible.value = false
    await nextTick()
    expect(requests[0]!.aborted).toBe(true)
    visible.value = true
    await nextTick()
    await Promise.resolve()
    expect(requests).toHaveLength(2)
    expect(received).toEqual([1, 2])
  } finally {
    app.unmount()
  }
  expect(requests[1]!.aborted).toBe(true)
})
