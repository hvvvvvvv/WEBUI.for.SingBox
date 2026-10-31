// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from 'vitest'
import { createApp, defineComponent, h, nextTick, ref, type App } from 'vue'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (value: string) => value }) }))
vi.mock('@/lang', () => ({ default: { global: { t: (value: string) => value } } }))
vi.mock('@/views/ProfilesView/components/DnsRulesConfig.vue', () => ({
  default: { render: () => null },
}))
vi.mock('@/views/ProfilesView/components/DnsServersConfig.vue', () => ({
  default: { render: () => null },
}))

import DnsConfig from '@/views/ProfilesView/components/DnsConfig.vue'
import Switch from '@/components/Switch/index.vue'
import { DefaultDns } from '@/constant/profile'

describe('DNS cache configuration controls', () => {
  let app: App
  let host: HTMLDivElement

  afterEach(() => {
    app.unmount()
    host.remove()
  })

  it('keeps optimistic caching mutually exclusive with disabled caching or expiry', async () => {
    const dns = ref(DefaultDns())
    dns.value.disable_cache = true
    dns.value.disable_expire = true
    host = document.createElement('div')
    document.body.append(host)
    app = createApp({
      render: () =>
        h(DnsConfig, {
          modelValue: dns.value,
          'onUpdate:modelValue': (value: IDNS) => {
            dns.value = value
          },
          inboundOptions: [],
          outboundOptions: [],
          ruleSet: [],
        }),
    })
    app.component(
      'Tabs',
      defineComponent(
        (_, { slots }) =>
          () =>
            h('div', slots.common?.()),
      ),
    )
    app.component('Switch', Switch)
    for (const name of ['Input', 'Select'])
      app.component(
        name,
        defineComponent(() => () => null),
      )
    app.directive('tips', {})
    app.mount(host)
    const toggle = async (index: number) => {
      host.querySelectorAll<HTMLElement>('.gui-switch')[index]!.click()
      await nextTick()
      await nextTick()
    }

    await toggle(2)
    expect(dns.value.optimistic.enabled).toBe(true)
    expect(dns.value.disable_cache).toBe(false)
    expect(dns.value.disable_expire).toBe(false)
    await toggle(0)
    expect(dns.value.disable_cache).toBe(true)
    expect(dns.value.optimistic.enabled).toBe(false)
    await toggle(2)
    await toggle(1)
    expect(dns.value.disable_expire).toBe(true)
    expect(dns.value.optimistic.enabled).toBe(false)
    expect(dns.value.optimistic.timeout).toBe('3d')
  })
})
