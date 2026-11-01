// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from 'vitest'
import { createApp, defineComponent, h, nextTick, ref, type App } from 'vue'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (value: string) => value }) }))
vi.mock('@/lang', () => ({ default: { global: { t: (value: string) => value } } }))
vi.mock('@/utils', async () => ({ deepClone: (await import('@/utils/others')).deepClone }))
vi.mock('@/views/ProfilesView/components/RouteRulesConfig.vue', () => ({ default: { render: () => null } }))
vi.mock('@/views/ProfilesView/components/RouteRulesetConfig.vue', () => ({ default: { render: () => null } }))

import RouteConfig from '@/views/ProfilesView/components/RouteConfig.vue'
import Select from '@/components/Select/index.vue'
import { DefaultRoute } from '@/constant/profile'

describe('route default HTTP client selection', () => {
  let app: App
  let host: HTMLDivElement
  afterEach(() => { app.unmount(); host.remove() })

  it('edits and clears the HTTP client independently from the final route in official field order', async () => {
    const route = ref(DefaultRoute())
    const originalFinal = route.value.final
    host = document.createElement('div')
    document.body.append(host)
    app = createApp({ render: () => h(RouteConfig, {
      modelValue: route.value,
      'onUpdate:modelValue': (value: IRoute) => { route.value = value },
      inboundOptions: [], serverOptions: [],
      outboundOptions: [{ value: 'outbound-select', label: 'Proxy' }, { value: 'direct-id', label: 'Direct' }],
    }) })
    app.component('Tabs', defineComponent((_, { slots }) => () => h('div', slots.common?.())))
    app.component('Select', Select)
    app.component('Dropdown', defineComponent((_, { slots }) => () => h('div', [
      slots.default?.({ open() {}, close() {}, toggle() {}, opened: true }),
      slots.overlay?.({ close() {} }),
    ])))
    app.component('Button', defineComponent({ props: ['icon'], setup: (props, { attrs, slots }) => () => h('button', { ...attrs, 'data-icon': props.icon }, slots.default?.()) }))
    for (const name of ['Switch', 'InterfaceSelect', 'Empty']) app.component(name, defineComponent(() => () => null))
    app.mount(host)
    await nextTick()
    const rows = [...host.querySelectorAll('.form-item')]
    const http = rows.find((row) => row.textContent?.includes('kernel.route.default_http_client'))!
    expect(rows.indexOf(http)).toBe(rows.findIndex((row) => row.textContent?.includes('kernel.route.default_domain_resolver.server')) + 1)
    expect(rows[rows.indexOf(http) + 1]?.textContent).toContain('kernel.route.final')
    // Dispatch in a later task, as a real click follows Vue's listener setup.
    await new Promise((resolve) => setTimeout(resolve, 10))
    const direct = [...http.querySelectorAll<HTMLButtonElement>('[role=option]')].find((item) => item.textContent?.trim() === 'Direct')!
    direct.click()
    await nextTick()
    expect(route.value.default_http_client).toBe('direct-id')
    expect(route.value.final).toBe(originalFinal)
    http.querySelector<HTMLButtonElement>('[data-icon=close]')!.click()
    await nextTick()
    expect(route.value.default_http_client).toBe('')
    expect(route.value.final).toBe(originalFinal)
  })
})
