// @vitest-environment jsdom
import { createApp, defineComponent, h, nextTick, ref, type App, type Component } from 'vue'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import type { Subscription } from '@/types/app'

const mocks = vi.hoisted(() => ({
  load: vi.fn(),
  save: vi.fn(),
  edit: vi.fn(),
  setup: vi.fn(),
  get: vi.fn(),
  revision: vi.fn(),
  message: vi.fn(),
  submit: vi.fn(),
}))
vi.mock('@/stores', async () => ({
  ...(await import('@/stores/resourceSync')),
  useSubscribesStore: () => ({
    getSubscriptionContentWithRevision: mocks.load,
    saveSubscriptionContent: mocks.save,
    editSubscribe: mocks.edit,
    setupSubscribes: mocks.setup,
    getSubscribeById: mocks.get,
    getSubscribeRevision: mocks.revision,
  }),
}))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/utils', async () => ({
  ...(await import('@/utils/others')),
  message: { error: mocks.message },
}))
vi.mock('@/hooks', async () => await import('@/hooks/useBool'))
vi.mock('@/bridge', () => ({ ClipboardSetText: vi.fn() }))
import ProxiesView from '@/views/SubscribesView/components/ProxiesView.vue'
import ProxiesEditor from '@/views/SubscribesView/components/ProxiesEditor.vue'
import SubscribeScript from '@/views/SubscribesView/components/SubscribeScript.vue'

let app: App
let host: HTMLDivElement
const revision = { instanceId: 'instance', revision: 7n }
const proxy = { tag: 'original', type: 'direct', domain_resolver: 'dns-remote' }
const sub = () =>
  ({
    id: 'manual',
    name: 'Manual',
    script: 'old script',
    proxies: [{ id: 'node-a', tag: proxy.tag, type: proxy.type }],
  }) as unknown as Subscription
beforeEach(() => {
  vi.resetAllMocks()
  vi.useFakeTimers()
  mocks.load.mockResolvedValue({ content: JSON.stringify([proxy]), revision })
  mocks.get.mockReturnValue(sub())
  mocks.revision.mockReturnValue(revision)
  mocks.setup.mockResolvedValue(undefined)
  host = document.createElement('div')
  document.body.append(host)
})
afterEach(() => {
  app.unmount()
  host.remove()
  vi.clearAllTimers()
  vi.useRealTimers()
})

const mount = async (component: Component = ProxiesView) => {
  const editor = ref<any>()
  app = createApp({
    render: () =>
      h('div', [
        h(component, { ref: editor, sub: sub(), revision, id: 'manual' }),
        h('footer', editor.value?.modalSlots.submit?.()),
      ]),
  })
  app.provide('submit', mocks.submit)
  app.provide('cancel', vi.fn())
  for (const name of ['Icon', 'Input', 'Select', 'Empty'])
    app.component(name, { render: () => null })
  app.component(
    'Button',
    defineComponent({
      setup:
        (_, { slots }) =>
        () =>
          h('button', slots.default?.()),
    }),
  )
  app.component(
    'Card',
    defineComponent({ props: ['title'], setup: (props) => () => h('article', props.title) }),
  )
  app.component(
    'Modal',
    defineComponent({
      props: ['open', 'onOk'],
      emits: ['update:open'],
      setup:
        (props, { slots, emit }) =>
        () =>
          props.open
            ? h('section', [
                slots.default?.(),
                h(
                  'button',
                  {
                    class: 'detail-save',
                    onClick: async () => {
                      if ((await props.onOk()) !== false) emit('update:open', false)
                    },
                  },
                  'detail-save',
                ),
              ])
            : null,
    }),
  )
  app.component(
    'CodeViewer',
    defineComponent({
      props: ['modelValue'],
      setup(props, { expose }) {
        const area = ref<HTMLTextAreaElement>()
        expose({ getContent: () => area.value?.value ?? props.modelValue })
        return () => h('textarea', { ref: area, value: props.modelValue })
      },
    }),
  )
  app.directive('draggable', {})
  app.directive('menu', {
    mounted(el, binding) {
      el.menus = binding.value
    },
    updated(el, binding) {
      el.menus = binding.value
    },
  })
  app.mount(host)
  await nextTick()
  await vi.advanceTimersByTimeAsync(1)
}
const save = async () => {
  host.querySelector<HTMLElement>('footer [role=button]')!.click()
  await vi.advanceTimersByTimeAsync(1)
}
const editProxy = async () => {
  await (host.querySelector('article') as any).menus
    .find((menu: any) => menu.label === 'common.edit')
    .handler()
  await nextTick()
  await vi.advanceTimersByTimeAsync(1)
}

it('refuses to combine old visible nodes with a newer content revision', async () => {
  mocks.load.mockResolvedValue({
    content: JSON.stringify([proxy, { tag: 'remote-added', type: 'direct' }]),
    revision: { ...revision, revision: 8n },
  })
  await mount()
  await save()
  expect(mocks.save).not.toHaveBeenCalled()
  expect(mocks.submit).not.toHaveBeenCalled()
  expect(host.textContent).toContain('common.resourceConflict')
  expect(host.textContent).toContain('original')
})

it('saves the current node document immediately, retaining its ID and updating its protocol', async () => {
  await mount()
  await editProxy()
  const edited = { ...proxy, tag: 'edited', type: 'socks', server: '127.0.0.1', server_port: 1080 }
  host.querySelector('textarea')!.value = JSON.stringify(edited)
  host.querySelector<HTMLButtonElement>('.detail-save')!.click()
  await vi.advanceTimersByTimeAsync(1)
  expect(host.querySelector('section')).toBeNull()
  await save()
  expect(mocks.save).toHaveBeenCalledExactlyOnceWith(
    'manual',
    JSON.stringify([edited], null, 2),
    ['node-a'],
    revision,
  )
  expect(mocks.load).toHaveBeenCalledOnce()
})

it.each(['null', '[]', '{invalid'])(
  'keeps the node dialog open for invalid input %s',
  async (input) => {
    await mount()
    await editProxy()
    host.querySelector('textarea')!.value = input
    host.querySelector<HTMLButtonElement>('.detail-save')!.click()
    await vi.advanceTimersByTimeAsync(1)
    expect(host.querySelector('textarea')!.value).toBe(input)
    expect(mocks.message).toHaveBeenCalledOnce()
    expect(mocks.save).not.toHaveBeenCalled()
    expect(host.querySelector('article')!.textContent).toBe('original')
  },
)

it('saves subscription scripts before their model debounce fires', async () => {
  await mount(SubscribeScript)
  host.querySelector('textarea')!.value =
    'function onSubscribe(proxies) { return proxies.slice(1) }'
  await save()
  expect(mocks.edit).toHaveBeenCalledExactlyOnceWith(
    'manual',
    expect.objectContaining({
      script: 'function onSubscribe(proxies) { return proxies.slice(1) }',
    }),
    revision,
  )
})

it('does not allow source editing or saving before the initial content request completes', async () => {
  const pending = Promise.withResolvers<any>()
  mocks.load.mockReturnValue(pending.promise)
  await mount(ProxiesEditor)
  expect(host.querySelector('textarea')).toBeNull()
  await save()
  expect(mocks.save).not.toHaveBeenCalled()
  pending.resolve({ content: JSON.stringify([proxy]), revision })
  await vi.advanceTimersByTimeAsync(1)
  expect(host.querySelector('textarea')!.value).toContain('node-a')
})

it('reports a failed initial content fetch without an unhandled rejection or allowing an empty save', async () => {
  mocks.load.mockRejectedValue(new Error('offline'))
  await mount(ProxiesEditor)
  await save()
  expect(mocks.message).toHaveBeenCalledWith('offline')
  expect(mocks.save).not.toHaveBeenCalled()
})
