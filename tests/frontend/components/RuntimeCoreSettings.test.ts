// @vitest-environment jsdom
import { createApp, defineComponent, h, nextTick, reactive, type App } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

const mocks = vi.hoisted(() => ({ kernel: null as any, interfaces: vi.fn(), version: vi.fn() }))
vi.mock('@/stores', () => ({
  useKernelApiStore: () => mocks.kernel,
  useAppStore: () => ({ platformOS: 'linux' }),
}))
vi.mock('@/bridge', () => ({ GetInterfaces: mocks.interfaces }))
vi.mock('@/api/kernel', () => ({ getNativeVersion: mocks.version }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/lang', () => ({ default: { global: { t: (value: string) => value } } }))
vi.mock('@/utils', () => ({ debounce: (fn: unknown) => fn }))
import { DefaultInboundMixed } from '@/constant/profile'
import { protoProfileToIProfile } from '@/utils/profileRpc'
import RuntimeCoreSettings from '@/views/HomeView/components/RuntimeCoreSettings.vue'

describe('temporary core settings panel', () => {
  let app: App
  let host: HTMLDivElement
  let profile: IProfile
  const mount = async (waitForLoaded = true) => {
    app = createApp({ render: () => h(RuntimeCoreSettings) })
    app.component(
      'Card',
      defineComponent({
        props: ['title', 'subtitle'],
        setup:
          (props, { slots }) =>
          () =>
            h('section', [h('h3', props.title), props.subtitle, slots.default?.()]),
      }),
    )
    app.component('Divider', {
      setup:
        (_, { slots }) =>
        () =>
          h('div', slots.default?.()),
    })
    app.component('Button', {
      props: ['disabled'],
      setup:
        (props, { slots }) =>
        () =>
          h('button', { disabled: props.disabled }, slots.default?.()),
    })
    app.component('Input', {
      props: ['modelValue', 'type', 'disabled'],
      emits: ['update:modelValue'],
      setup:
        (props, { emit }) =>
        () =>
          h('input', {
            disabled: props.disabled,
            type: props.type || 'text',
            value: props.modelValue,
            onInput: (event: Event) =>
              emit(
                'update:modelValue',
                props.type === 'number'
                  ? Number((event.target as HTMLInputElement).value)
                  : (event.target as HTMLInputElement).value,
              ),
          }),
    })
    app.component('Switch', {
      props: ['modelValue', 'disabled'],
      emits: ['update:modelValue'],
      setup:
        (props, { emit }) =>
        () =>
          h('input', {
            type: 'checkbox',
            disabled: props.disabled,
            checked: props.modelValue,
            onChange: (event: Event) =>
              emit('update:modelValue', (event.target as HTMLInputElement).checked),
          }),
    })
    app.component('Select', {
      props: ['modelValue', 'options'],
      emits: ['update:modelValue'],
      setup:
        (props, { emit }) =>
        () =>
          h(
            'select',
            {
              value: props.modelValue,
              onChange: (event: Event) =>
                emit('update:modelValue', (event.target as HTMLSelectElement).value),
            },
            props.options.map((option: any) => h('option', { value: option.value }, option.label)),
          ),
    })
    app.mount(host)
    if (waitForLoaded)
      await vi.waitFor(() =>
        expect(host.querySelector('[data-proxy="mixed-in"] input')).toBeTruthy(),
      )
  }
  const port = (id: string) =>
    host.querySelector<HTMLInputElement>(`[data-proxy="${id}"] input[type=number]`)!
  const setPort = async (id: string, value: number) => {
    port(id).value = String(value)
    port(id).dispatchEvent(new Event('input', { bubbles: true }))
    await nextTick()
  }
  const clickApply = () => host.querySelector<HTMLButtonElement>('[data-apply]')!.click()
  beforeEach(() => {
    vi.resetAllMocks()
    profile = protoProfileToIProfile(undefined)
    profile.inbounds[0]!.enable = true
    profile.inbounds.push({
      id: 'second',
      tag: 'second proxy',
      type: 'mixed',
      enable: true,
      mixed: DefaultInboundMixed(),
    })
    profile.inbounds[2]!.mixed!.listen.listen_port = 20882
    mocks.kernel = reactive({
      pid: 1,
      running: true,
      restarting: false,
      loadRuntimeSettings: vi
        .fn()
        .mockImplementation(async () => ({ profile: structuredClone(profile), generation: 1 })),
      updateRuntimeSettings: vi.fn().mockResolvedValue(undefined),
    })
    mocks.interfaces.mockResolvedValue(['eth0', 'br-lan'])
    mocks.version.mockResolvedValue({ version: '1.14.4' })
    host = document.createElement('div')
    document.body.append(host)
  })
  afterEach(() => {
    app?.unmount()
    host.remove()
  })

  it('renders all proxy listeners, reads server interfaces and applies only on explicit confirmation', async () => {
    await mount()
    expect(port('mixed-in').value).toBe('20122')
    expect(port('second').value).toBe('20882')
    expect(host.textContent).toContain('br-lan')
    await setPort('second', 20883)
    expect(mocks.kernel.updateRuntimeSettings).not.toHaveBeenCalled()
    clickApply()
    await vi.waitFor(() => expect(mocks.kernel.updateRuntimeSettings).toHaveBeenCalledOnce())
    expect(
      mocks.kernel.updateRuntimeSettings.mock.calls[0][0].proxies.find(
        (proxy: any) => proxy.id === 'second',
      ).port,
    ).toBe(20883)
    expect(profile.inbounds[2]!.mixed!.listen.listen_port).toBe(20882)
  })

  it('distinguishes system routing, interface detection and a server interface without changing the untouched default', async () => {
    profile.route.auto_detect_interface = false
    profile.route.default_interface = ''
    await mount()
    const choice = Array.from(host.querySelectorAll('select')).find((select) =>
      Array.from(select.options).some((option) => option.value === 'auto'),
    )!
    expect(choice.value).toBe('system')
    await setPort('second', 20883)
    clickApply()
    await vi.waitFor(() => expect(mocks.kernel.updateRuntimeSettings).toHaveBeenCalledOnce())
    expect(mocks.kernel.updateRuntimeSettings.mock.calls[0][0].outboundMode).toBe('system')
    await vi.waitFor(() =>
      expect(host.querySelector<HTMLButtonElement>('[data-apply]')!.disabled).toBe(false),
    )
    choice.value = 'interface:eth0'
    choice.dispatchEvent(new Event('change', { bubbles: true }))
    await nextTick()
    clickApply()
    await vi.waitFor(() => expect(mocks.kernel.updateRuntimeSettings).toHaveBeenCalledTimes(2))
    expect(mocks.kernel.updateRuntimeSettings.mock.calls[1][0]).toMatchObject({
      outboundMode: 'interface',
      outboundInterface: 'eth0',
    })
    await vi.waitFor(() =>
      expect(host.querySelector<HTMLButtonElement>('[data-apply]')!.disabled).toBe(false),
    )
    choice.value = 'auto'
    choice.dispatchEvent(new Event('change', { bubbles: true }))
    await nextTick()
    clickApply()
    await vi.waitFor(() => expect(mocks.kernel.updateRuntimeSettings).toHaveBeenCalledTimes(3))
    expect(mocks.kernel.updateRuntimeSettings.mock.calls[2][0].outboundMode).toBe('auto')
  })

  it('restores actual running values after failed restart and permits editing and retry', async () => {
    await mount()
    mocks.kernel.updateRuntimeSettings.mockRejectedValueOnce(new Error('preflight failed'))
    await setPort('mixed-in', 20900)
    clickApply()
    await vi.waitFor(() =>
      expect(host.querySelector('[role=alert]')?.textContent).toBe('preflight failed'),
    )
    await vi.waitFor(() => expect(port('mixed-in').value).toBe('20122'))
    await setPort('mixed-in', 20901)
    clickApply()
    await vi.waitFor(() => expect(mocks.kernel.updateRuntimeSettings).toHaveBeenCalledTimes(2))
    expect(mocks.kernel.updateRuntimeSettings.mock.calls[1][0].proxies[0].port).toBe(20901)
  })

  it('keeps invalid input visible, rejects it before mutation, and removes stale form when stopped', async () => {
    await mount()
    await setPort('mixed-in', 70000)
    clickApply()
    await nextTick()
    expect(host.textContent).toContain('home.overview.runtimeSettings.invalidPort')
    expect(port('mixed-in').value).toBe('70000')
    expect(mocks.kernel.updateRuntimeSettings).not.toHaveBeenCalled()
    mocks.kernel.running = false
    await nextTick()
    expect(host.querySelector('[data-proxy]')).toBeNull()
    expect(host.textContent).toContain('home.overview.runtimeSettings.notRunning')
  })

  it.each(['1.15.0-alpha.1', ''])(
    'does not offer an ineffective stack control for version %s',
    async (version) => {
      mocks.version.mockResolvedValue({ version })
      await mount()
      expect(host.querySelector('[data-stack] select')).toBeNull()
      expect(host.textContent).toContain(
        `home.overview.runtimeSettings.${version ? 'stackManaged' : 'stackUnavailable'}`,
      )
    },
  )

  it('discards delayed loading when the panel is closed', async () => {
    let resolve!: (value: unknown) => void
    mocks.kernel.loadRuntimeSettings.mockImplementationOnce(
      () =>
        new Promise((accept) => {
          resolve = accept
        }),
    )
    await mount(false)
    app.unmount()
    resolve({ profile, generation: 1 })
    await nextTick()
    expect(host.querySelector('[data-proxy]')).toBeNull()
    expect(mocks.kernel.updateRuntimeSettings).not.toHaveBeenCalled()
  })
})
