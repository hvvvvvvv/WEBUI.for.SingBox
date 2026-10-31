// @vitest-environment jsdom
import { createApp, defineComponent, h, nextTick, ref, type App } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { Subscription } from '@/types/app'

const mocks = vi.hoisted(() => ({
  load: vi.fn(),
  save: vi.fn(),
  message: vi.fn(),
  submit: vi.fn(),
}))
vi.mock('@/stores', () => ({
  isResourceConflict: () => false,
  isResourceNotFound: () => false,
  useSubscribesStore: () => ({
    getSubscriptionContentWithRevision: mocks.load,
    saveSubscriptionContent: mocks.save,
  }),
}))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/utils', async () => ({
  ...(await import('@/utils/others')),
  message: { error: mocks.message },
}))
import ProxiesEditor from '@/views/SubscribesView/components/ProxiesEditor.vue'

describe('subscription source editor immediate save', () => {
  let app: App
  let host: HTMLDivElement
  const revision = { instanceId: 'instance', revision: 7n }
  beforeEach(() => {
    vi.resetAllMocks()
    mocks.load.mockResolvedValue({ content: '[]', revision })
    mocks.save.mockResolvedValue(undefined)
    host = document.createElement('div')
    document.body.append(host)
  })
  afterEach(() => {
    app.unmount()
    host.remove()
  })
  const mount = async () => {
    const editor = ref<any>()
    const sub = { id: 'manual', name: 'Manual', type: 'Manual', proxies: [] } as unknown as Subscription
    app = createApp({
      render: () =>
        h('div', [
          h(ProxiesEditor, { ref: editor, sub }),
          h('footer', editor.value?.modalSlots.submit?.()),
        ]),
    })
    app.provide('submit', mocks.submit)
    app.provide('cancel', vi.fn())
    app.component('Icon', { render: () => null })
    app.component(
      'CodeViewer',
      defineComponent({
        props: ['modelValue'],
        setup(props, { expose }) {
          const area = ref<HTMLTextAreaElement>()
          expose({ getContent: () => area.value?.value ?? props.modelValue })
          // Keep v-model unchanged to reproduce CodeMirror's debounce window.
          return () => h('textarea', { ref: area, value: props.modelValue })
        },
      }),
    )
    app.mount(host)
    await nextTick()
    await vi.waitFor(() => expect(host.querySelector('textarea')!.value).toBe('[]'))
    // Native user clicks occur in a later event-loop task than listener attachment.
    await new Promise((resolve) => setTimeout(resolve, 1))
  }
  const save = () => host.querySelector<HTMLElement>('footer [role=button]')!.click()

  it('saves the current document before the debounced model update', async () => {
    await mount()
    host.querySelector('textarea')!.value = JSON.stringify([
      { type: 'direct', tag: 'edited', __id_in_gui: 'existing-node' },
    ])
    save()
    await vi.waitFor(() => expect(mocks.save).toHaveBeenCalledOnce())
    expect(mocks.save).toHaveBeenCalledWith(
      'manual',
      JSON.stringify([{ type: 'direct', tag: 'edited' }], null, 2),
      ['existing-node'],
      revision,
    )
    expect(mocks.submit).toHaveBeenCalledOnce()
    expect(mocks.message).not.toHaveBeenCalled()
  })

  it('rejects an invalid current document instead of saving the stale empty model', async () => {
    await mount()
    host.querySelector('textarea')!.value = '[invalid json'
    save()
    await vi.waitFor(() => expect(mocks.message).toHaveBeenCalledOnce())
    expect(mocks.save).not.toHaveBeenCalled()
    expect(mocks.submit).not.toHaveBeenCalled()
    expect(host.querySelector('textarea')!.value).toBe('[invalid json')
  })
})
