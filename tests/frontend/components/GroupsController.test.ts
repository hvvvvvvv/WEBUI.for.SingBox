// @vitest-environment jsdom
import { create } from '@bufbuild/protobuf'
import { createApp, defineComponent, h, nextTick, reactive, type App } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import {
  GroupItemSchema,
  GroupSchema,
  OutboundListSchema,
  type OutboundList,
} from '@gen/native/daemon/started_service_pb'

const mocks = vi.hoisted(() => ({
  urlTest: vi.fn(),
  onOutbounds: vi.fn(),
  settings: {} as any,
  store: {} as any,
  error: vi.fn(),
}))
vi.mock('@/api/kernel', () => ({
  urlTest: mocks.urlTest,
  onOutbounds: mocks.onOutbounds,
  getNativeApiGeneration: () => 1,
}))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/stores', () => ({
  useAppSettingsStore: () => mocks.settings,
  useKernelApiStore: () => mocks.store,
  useProfilesStore: () => ({ currentProfile: { outbounds: [] } }),
}))
vi.mock('@/utils', () => ({
  handleUseProxy: vi.fn().mockResolvedValue(undefined),
  buildSmartRegExp: (value: string, flags: string) => new RegExp(value, flags),
  message: { error: mocks.error, warn: vi.fn(), success: vi.fn() },
}))

import GroupsController from '@/views/HomeView/components/GroupsController.vue'

describe('group controls and latency progress', () => {
  let app: App
  let host: HTMLDivElement
  let emit: (value: OutboundList) => void
  const node = (tag: string, time: bigint, delay: number) =>
    create(GroupItemSchema, { tag, type: 'shadowsocks', urlTestTime: time, urlTestDelay: delay })
  beforeEach(() => {
    vi.resetAllMocks()
    mocks.onOutbounds.mockImplementation((callback: typeof emit) => {
      emit = callback
      return vi.fn()
    })
    mocks.settings = reactive({
      app: {
        kernel: {
          autoClose: true,
          unAvailable: true,
          cardMode: true,
          sortByDelay: false,
          cardColumns: 5,
          controllerSensitivity: 2,
          controllerCloseMode: 'All',
        },
      },
    })
    const nodes = [node('slow', 10n, 200), node('fast', 10n, 50)]
    mocks.store = reactive({
      pid: 1,
      running: true,
      groups: [
        create(GroupSchema, {
          tag: 'group',
          items: nodes,
          selected: 'slow',
          type: 'selector',
          selectable: true,
        }),
      ],
      outbounds: nodes,
      subscribeGroups: () => vi.fn(),
    })
    host = document.createElement('div')
    document.body.append(host)
  })
  afterEach(() => {
    app?.unmount()
    host.remove()
  })
  const mount = async () => {
    app = createApp(GroupsController)
    app.config.globalProperties.$t = (key: string, params?: Record<string, unknown>) =>
      key + (params ? JSON.stringify(params) : '')
    app.directive('tips', {})
    app.component(
      'Button',
      defineComponent({
        props: ['icon', 'loading'],
        setup:
          (props, { slots }) =>
          () =>
            h('button', { 'data-icon': props.icon }, slots.default?.()),
      }),
    )
    app.component(
      'Switch',
      defineComponent({
        props: ['modelValue', 'label'],
        emits: ['update:modelValue'],
        setup:
          (props, { emit }) =>
          () =>
            h(
              'button',
              {
                'data-switch': props.label,
                onClick: () => emit('update:modelValue', !props.modelValue),
              },
              props.label,
            ),
      }),
    )
    app.component(
      'Card',
      defineComponent({
        props: ['title'],
        setup:
          (props, { slots }) =>
          () =>
            h('article', { 'data-title': props.title }, slots.default?.()),
      }),
    )
    app.component(
      'Modal',
      defineComponent({
        props: ['open'],
        setup:
          (props, { slots }) =>
          () =>
            props.open && h('section', { 'data-modal': '' }, [slots.action?.(), slots.default?.()]),
      }),
    )
    for (const name of ['Icon', 'Input', 'Radio', 'Empty'])
      app.component(name, { render: () => null })
    app.mount(host)
    await nextTick()
    await new Promise((resolve) => setTimeout(resolve, 1))
  }

  it('shows an immediate top progress bar before acknowledgment, updates counts, and cancels only its wait', async () => {
    let resolve!: (value: object) => void
    mocks.urlTest.mockReturnValue(
      new Promise((accept) => {
        resolve = accept
      }),
    )
    await mount()
    host.querySelector<HTMLButtonElement>('button[data-icon="speedTest"]')!.click()
    await nextTick()
    const bar = host.querySelector('progress')!
    expect(bar).not.toBeNull()
    expect(bar.value).toBe(0)
    expect(bar.max).toBe(2)
    expect(mocks.urlTest).toHaveBeenCalledExactlyOnceWith('group')
    expect(host.textContent).toContain('testStatus.running')
    resolve({})
    await nextTick()
    emit(create(OutboundListSchema, { outbounds: [node('slow', 11n, 180), node('fast', 10n, 50)] }))
    await nextTick()
    expect(bar.value).toBe(1)
    expect(host.textContent).toContain('"success":1,"failure":0')
    ;[...host.querySelectorAll('button')]
      .find((entry) => entry.textContent === 'home.controller.stopWaiting')!
      .click()
    await nextTick()
    expect(host.textContent).toContain('testStatus.canceled')
    emit(create(OutboundListSchema, { outbounds: [node('slow', 11n, 180), node('fast', 11n, 40)] }))
    await nextTick()
    expect(bar.value).toBe(1)
  })

  it('sorts cards without changing the selected node and opens the original more-settings controls', async () => {
    await mount()
    host.querySelector<HTMLElement>('.font-bold.text-18')!.click()
    await nextTick()
    expect([...host.querySelectorAll('article')].map((card) => card.dataset.title)).toEqual([
      'slow',
      'fast',
    ])
    host.querySelector<HTMLButtonElement>('[data-switch="home.controller.sortBy"]')!.click()
    await nextTick()
    expect([...host.querySelectorAll('article')].map((card) => card.dataset.title)).toEqual([
      'fast',
      'slow',
    ])
    expect(mocks.store.groups[0].selected).toBe('slow')
    host.querySelector<HTMLButtonElement>('button[aria-label="common.more"]')!.click()
    await nextTick()
    const modal = host.querySelector('[data-modal]')!
    expect(modal.textContent).toContain('home.controller.closeMode.name')
    expect(modal.textContent).toContain('home.controller.cardColumns')
  })
})
