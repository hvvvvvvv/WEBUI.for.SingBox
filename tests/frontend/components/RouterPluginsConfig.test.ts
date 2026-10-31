// @vitest-environment jsdom
import { createApp, defineComponent, h, nextTick, ref, type App } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import RouterPluginsConfig from '@/views/ProfilesView/components/RouterPluginsConfig.vue'

const mocks = vi.hoisted(() => ({
  settings: { sessionInfo: { cacheToken: 'session-token' } },
  recoverAuthToken: vi.fn(),
  error: vi.fn(),
  success: vi.fn(),
}))
vi.mock('@/stores', () => ({ useAppSettingsStore: () => mocks.settings }))
vi.mock('@/bridge/http', () => ({ recoverAuthToken: mocks.recoverAuthToken }))
vi.mock('@/utils', () => ({ message: { error: mocks.error, success: mocks.success } }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

const plugin = (version = '1.0.0') => ({
  apiVersion: 'webui.router/v1',
  id: 'community-dns',
  name: 'DNS for router',
  description: 'DNS interception',
  version,
  kind: 'dns-hijack',
  config: { inbound: '' },
})

describe('router plugin editor', () => {
  let app: App | undefined
  let host: HTMLDivElement
  const installed = ref<IRouterPlugin[]>([])
  const preview = vi.fn()
  const fetchMock = vi.fn()
  let catalog = [plugin()]
  const profile = {
    inbounds: [
      { id: 'lan', tag: 'lan-tun', type: 'tun', enable: true },
      { id: 'disabled', tag: 'disabled-tun', type: 'tun', enable: false },
      { id: 'mixed', tag: 'mixed-in', type: 'mixed', enable: true },
    ],
    outbounds: [{ id: 'direct', tag: 'direct', type: 'direct' }],
  } as IProfile

  beforeEach(() => {
    vi.clearAllMocks()
    installed.value = []
    catalog = [plugin()]
    mocks.settings.sessionInfo.cacheToken = 'session-token'
    fetchMock.mockReset().mockImplementation(async (path: string) => ({
      ok: true,
      status: 200,
      json: async () =>
        path.includes('/plugins')
          ? { plugins: catalog, source: path.includes('refresh=1') ? 'remote' : 'bundled' }
          : {
              os: 'linux',
              arch: 'arm64',
              checks: [{ id: 'tun', label: 'TUN device', status: 'ok', message: 'Available' }],
            },
    }))
    vi.stubGlobal('fetch', fetchMock)
    host = document.createElement('div')
    document.body.appendChild(host)
  })

  afterEach(() => {
    app?.unmount()
    host.remove()
    vi.useRealTimers()
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
  })

  const mount = async () => {
    app = createApp({
      render: () =>
        h(RouterPluginsConfig, {
          profile,
          modelValue: installed.value,
          'onUpdate:modelValue': (value: IRouterPlugin[]) => {
            installed.value = value
          },
          onPreview: preview,
        }),
    })
    app.component(
      'Button',
      defineComponent({
        props: ['disabled', 'loading'],
        setup:
          (props, { slots }) =>
          () =>
            h('button', { disabled: props.disabled || props.loading }, slots.default?.()),
      }),
    )
    app.component(
      'Switch',
      defineComponent({
        props: ['modelValue'],
        emits: ['update:modelValue'],
        setup:
          (props, { emit }) =>
          () =>
            h('button', {
              role: 'switch',
              'aria-checked': props.modelValue,
              onClick: () => emit('update:modelValue', !props.modelValue),
            }),
      }),
    )
    app.component(
      'Select',
      defineComponent({
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
              [
                h('option', { value: '' }, 'Choose'),
                ...(props.options || []).map((option: { value: string; label: string }) =>
                  h('option', { value: option.value }, option.label),
                ),
              ],
            ),
      }),
    )
    app.component('InputList', defineComponent({ render: () => h('div') }))
    app.mount(host)
    await vi.waitFor(() => expect(host.textContent).toContain('DNS for router'))
  }

  const click = async (key: string, root: Element = host) => {
    const button = [...root.querySelectorAll<HTMLButtonElement>('button')].find(
      (item) => item.textContent === `profile.routerPlugins.${key}`,
    )
    expect(button).toBeDefined()
    button!.click()
    await nextTick()
  }
  const selectInbound = async () => {
    const select = host.querySelector('select')!
    expect([...select.options].map((option) => option.value)).toEqual(['', 'lan-tun'])
    select.value = 'lan-tun'
    select.dispatchEvent(new Event('change', { bubbles: true }))
    await nextTick()
  }

  it('installs independent disabled snapshots, selects enabled TUN tags, enables and previews', async () => {
    await mount()
    expect(fetchMock.mock.calls.map(([path]) => path)).toEqual([
      '/api/router/plugins',
      '/api/router/diagnostics',
    ])
    expect(fetchMock.mock.calls[0]![1].headers.Authorization).toBe('Bearer session-token')
    expect(host.textContent).toContain('linux / arm64')
    await click('install')
    expect(installed.value[0]!.enabled).toBe(false)
    host.querySelector<HTMLButtonElement>('[role=switch]')!.click()
    await nextTick()
    expect(installed.value[0]!.enabled).toBe(false)
    expect(mocks.error).toHaveBeenCalledWith('profile.routerPlugins.errors.missingInbound')
    await selectInbound()
    host.querySelector<HTMLButtonElement>('[role=switch]')!.click()
    await nextTick()
    expect(installed.value[0]!.enabled).toBe(true)
    expect(JSON.parse(installed.value[0]!.manifest_json).config.inbound).toBe('lan-tun')
    await click('preview')
    expect(preview).toHaveBeenCalledOnce()
    await click('install')
    expect(installed.value).toHaveLength(2)
    expect(installed.value[0]!.id).not.toBe(installed.value[1]!.id)
    expect(JSON.parse(installed.value[1]!.manifest_json).config.inbound).toBe('')
    const firstId = installed.value[0]!.id
    await click('moveDown')
    expect(installed.value[1]!.id).toBe(firstId)
    await click('remove')
    expect(installed.value).toHaveLength(1)
    expect(installed.value[0]!.id).toBe(firstId)
  })

  it('never updates installed snapshots when refreshing the catalog and retains parameters on explicit update', async () => {
    await mount()
    await click('install')
    await selectInbound()
    host.querySelector<HTMLButtonElement>('[role=switch]')!.click()
    await nextTick()
    catalog = [plugin('1.2.0')]
    await click('refreshCatalog')
    await vi.waitFor(() => expect(host.textContent).toContain('profile.routerPlugins.update'))
    expect(JSON.parse(installed.value[0]!.manifest_json).version).toBe('1.0.0')
    await click('update')
    expect(installed.value[0]!.enabled).toBe(true)
    expect(JSON.parse(installed.value[0]!.manifest_json)).toMatchObject({
      version: '1.2.0',
      config: { inbound: 'lan-tun' },
    })
    expect(catalog[0]!.config.inbound).toBe('')
    expect(fetchMock).toHaveBeenCalledWith('/api/router/plugins?refresh=1', expect.anything())
  })

  it('recovers authorization for GET requests without discarding a saved draft', async () => {
    const normal = fetchMock.getMockImplementation()!
    let denied = false
    fetchMock.mockImplementation(async (...args) => {
      if (args[0] === '/api/router/plugins' && !denied) {
        denied = true
        return { ok: false, status: 401 }
      }
      return normal(...args)
    })
    mocks.recoverAuthToken.mockImplementation(async () => {
      mocks.settings.sessionInfo.cacheToken = 'recovered-token'
      return true
    })
    await mount()
    expect(mocks.recoverAuthToken).toHaveBeenCalledOnce()
    const requests = fetchMock.mock.calls.filter(([path]) => path === '/api/router/plugins')
    expect(requests).toHaveLength(2)
    expect(requests[1]![1].headers.Authorization).toBe('Bearer recovered-token')
  })

  it('imports JSON as a disabled independent snapshot, rejects JavaScript, and exports a JSON download', async () => {
    await mount()
    const input = host.querySelector<HTMLInputElement>('input[type=file]')!
    const chooseFile = (content: string) => {
      Object.defineProperty(input, 'files', {
        configurable: true,
        value: [{ size: content.length, text: async () => content }],
      })
      input.dispatchEvent(new Event('change', { bubbles: true }))
    }
    const community = { ...plugin(), id: 'my-community-package', config: { inbound: 'lan-tun' } }
    chooseFile(JSON.stringify(community))
    await vi.waitFor(() => expect(installed.value).toHaveLength(1))
    expect(installed.value[0]!.enabled).toBe(false)
    expect(JSON.parse(installed.value[0]!.manifest_json)).toEqual(community)
    chooseFile('globalThis.compromised = true')
    await vi.waitFor(() =>
      expect(mocks.error).toHaveBeenCalledWith('profile.routerPlugins.errors.invalidJson'),
    )
    expect(installed.value).toHaveLength(1)
    expect((globalThis as Record<string, unknown>).compromised).toBeUndefined()

    const createObjectURL = vi.fn(() => 'blob:router-plugin-test')
    const revokeObjectURL = vi.fn()
    const NativeURL = URL
    vi.stubGlobal(
      'URL',
      class extends NativeURL {
        static createObjectURL = createObjectURL
        static revokeObjectURL = revokeObjectURL
      },
    )
    const clickAnchor = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {})
    vi.useFakeTimers()
    await click('export')
    expect(createObjectURL).toHaveBeenCalledOnce()
    expect(clickAnchor.mock.instances[0]!.download).toBe('my-community-package.json')
    expect(clickAnchor.mock.instances[0]!.href).toBe('blob:router-plugin-test')
    vi.advanceTimersByTime(1000)
    expect(revokeObjectURL).toHaveBeenCalledWith('blob:router-plugin-test')
  })
})
