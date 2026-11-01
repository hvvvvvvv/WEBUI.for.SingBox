// @vitest-environment jsdom
import { createApp, defineComponent, h, nextTick, ref, type App } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

const errors = vi.hoisted(() => vi.fn())
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/lang', () => ({ default: { global: { t: (key: string) => key } } }))
vi.mock('@/directives/tips', () => ({ default: {} }))
vi.mock('@/utils', async () => ({
  ...(await import('@/utils/others')),
  renderRouteRulePreview: () => 'rule preview',
  message: { error: errors, warn: vi.fn() },
}))
import { DefaultRouteRule, DefaultRouteRuleset } from '@/constant/profile'
import { RuleAction, RuleType } from '@/enums/kernel'
import Radio from '@/components/Radio/index.vue'
import Switch from '@/components/Switch/index.vue'
import Card from '@/components/Card/index.vue'
import RouteRulesConfig from '@/views/ProfilesView/components/RouteRulesConfig.vue'

describe('official route rule controls and lossless compound editing', () => {
  let app: App
  let host: HTMLDivElement
  let exposed: { handleAdd(): void }
  beforeEach(() => {
    vi.useFakeTimers()
    errors.mockClear()
    host = document.createElement('div')
    document.body.append(host)
  })
  afterEach(() => {
    app?.unmount()
    host.remove()
    vi.clearAllTimers()
    vi.useRealTimers()
  })
  const mount = async (rules: IRule[]) => {
    const component = ref()
    app = createApp({
      render: () =>
        h(RouteRulesConfig, {
          ref: component,
          modelValue: rules,
          inboundOptions: [{ label: 'LAN', value: 'lan' }],
          outboundOptions: [{ label: 'Direct', value: 'direct' }],
          serverOptions: [{ label: 'DNS', value: 'dns' }],
          ruleSet: [
            { ...DefaultRouteRuleset(), id: 'set', tag: 'Set' },
            { ...DefaultRouteRuleset(), id: 'set-long', tag: 'Set Long' },
          ],
        }),
    })
    app.config.globalProperties.$t = (key: string) => key
    app.directive('tips', {})
    app.directive('draggable', {})
    app.component('Radio', Radio)
    app.component('Switch', Switch)
    app.component('Card', Card)
    app.component(
      'Button',
      defineComponent({
        props: ['icon'],
        setup:
          (props, { slots }) =>
          () =>
            h('button', { 'data-icon': props.icon }, slots.default?.()),
      }),
    )
    app.component(
      'Select',
      defineComponent({
        props: ['modelValue', 'options', 'multiple'],
        emits: ['update:modelValue'],
        setup:
          (props, { emit }) =>
          () =>
            h(
              'select',
              {
                value: props.modelValue,
                multiple: props.multiple,
                onChange: (event: Event) => {
                  const field = event.target as HTMLSelectElement
                  emit(
                    'update:modelValue',
                    props.multiple
                      ? [...field.selectedOptions].map((option) => option.value)
                      : field.value,
                  )
                },
              },
              props.options.map((option: { label: string; value: string }) =>
                h('option', { value: option.value }, option.label),
              ),
            ),
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
    app.component(
      'Modal',
      defineComponent({
        props: ['open', 'onOk'],
        setup:
          (props, { slots }) =>
          () =>
            props.open &&
            h('section', { 'data-modal': '' }, [
              slots.default?.(),
              h('button', { 'data-save': '', onClick: props.onOk }, 'Save'),
            ]),
      }),
    )
    app.component(
      'Divider',
      defineComponent({
        setup:
          (_, { slots }) =>
          () =>
            h('div', slots.default?.()),
      }),
    )
    for (const name of ['Input', 'InputList', 'Empty', 'Icon'])
      app.component(name, { render: () => null })
    app.mount(host)
    await nextTick()
    await vi.advanceTimersByTimeAsync(1)
    exposed = component.value
  }
  const edit = async () => {
    host.querySelector<HTMLButtonElement>('[data-icon=edit]')!.click()
    await nextTick()
    await vi.advanceTimersByTimeAsync(1)
  }
  const save = () => host.querySelector<HTMLButtonElement>('[data-save]')!.click()
  const selectType = async (type: string) => {
    const field = host.querySelector<HTMLSelectElement>('[data-rule-type] select')!
    field.value = type
    field.dispatchEvent(new Event('change', { bubbles: true }))
    await nextTick()
  }
  const action = async (type: RuleAction) => {
    const control = [
      ...host.querySelectorAll<HTMLElement>('[data-rule-action] .gui-radio-button'),
    ].find((entry) => entry.textContent?.trim() === `kernel.route.rules.action.${type}`)!
    control.click()
    await nextTick()
  }

  it('shows official type, segmented action, invert and outbound rows before rule-set cards', async () => {
    const rule = DefaultRouteRule()
    rule.rule_set = ['set']
    rule.action_options.outbound = 'direct'
    const rules = [rule]
    await mount(rules)
    await edit()
    expect(host.querySelector('.shared-rule-builder')).toBeNull()
    expect(host.querySelector('.action-picker')).toBeNull()
    expect(
      [
        ...host.querySelectorAll(
          '[data-rule-type], [data-rule-action], [data-rule-invert], [data-rule-outbound]',
        ),
      ].map(
        (node) =>
          [...node.attributes].find((attribute) => attribute.name.startsWith('data-rule-'))!.name,
      ),
    ).toEqual(['data-rule-type', 'data-rule-action', 'data-rule-invert', 'data-rule-outbound'])
    expect(host.querySelector<HTMLSelectElement>('[data-rule-type] select')!.value).toBe('rule_set')
    expect(host.querySelector<HTMLDetailsElement>('details')!.open).toBe(false)
    const cards = host.querySelectorAll<HTMLElement>('.rule-set-card')
    expect([...cards].map((card) => card.getAttribute('aria-checked'))).toEqual(['true', 'false'])
    cards[1]!.click()
    save()
    expect(rules[0]!.rule_set).toEqual(['set', 'set-long'])
  })

  it('preserves every compound match, disabled state, raw logical rule and advanced action option', async () => {
    const rule: IRule = {
      ...DefaultRouteRule(),
      enable: false,
      invert: true,
      domain: ['example.org'],
      source_ip_cidr: ['192.0.2.0/24'],
      port: [443],
      rule_set: ['set'],
      raw: '{"type":"logical","mode":"or","rules":[{"domain_suffix":["example.net"]}],"action":"route","outbound":"direct"}',
    }
    Object.assign(rule.action_options, {
      outbound: 'direct',
      override_address: '127.0.0.1',
      tls_fragment: true,
      timeout: '2s',
    })
    const expected = structuredClone(rule)
    const rules = [rule]
    await mount(rules)
    await edit()
    expect(host.querySelector<HTMLSelectElement>('[data-rule-type] select')!.value).toBe('combined')
    expect(host.querySelector<HTMLDetailsElement>('details')!.open).toBe(true)
    expect(host.textContent).toContain('kernel.route.rules.rawOverrideHint')
    await selectType('network')
    save()
    expect(rules[0]).toEqual(expected)
    expect(errors).not.toHaveBeenCalled()
  })

  it('keeps parameters when changing action and only removes explicitly deleted matches', async () => {
    const rule = {
      ...DefaultRouteRule(),
      domain: ['example.org'],
      source_ip_cidr: ['192.0.2.0/24'],
      port: [443],
    }
    Object.assign(rule.action_options, {
      outbound: 'direct',
      override_port: 8443,
      sniffer: ['tls'],
    })
    const rules = [rule]
    await mount(rules)
    await edit()
    await action(RuleAction.Reject)
    await action(RuleAction.Sniff)
    await action(RuleAction.Route)
    host
      .querySelector<HTMLButtonElement>(
        '[aria-label="common.delete kernel.rules.type.source_ip_cidr"]',
      )!
      .click()
    save()
    expect(rules[0]!.domain).toEqual(['example.org'])
    expect(rules[0]!.source_ip_cidr).toEqual([])
    expect(rules[0]!.port).toEqual([443])
    expect(rules[0]!.action_options).toEqual(rule.action_options)
  })

  it('keeps invalid inline drafts and reads the latest logical JSON when changing type', async () => {
    const rule = { ...DefaultRouteRule(), action: RuleAction.Inline, raw: '{"action":"reject"}' }
    const rules = [rule]
    await mount(rules)
    await edit()
    const area = host.querySelector<HTMLTextAreaElement>('textarea')!
    area.value = '[invalid'
    save()
    expect(errors).toHaveBeenCalledWith('kernel.route.rules.inlineInvalidPayload')
    expect(rules[0]!.raw).toBe(rule.raw)
    area.value =
      '{"type":"logical","mode":"and","rules":[{"domain":["example.org"]}],"action":"reject"}'
    await selectType('rule_set')
    save()
    expect(rules[0]!.raw).toBe(area.value)
    expect(rules[0]!.action).toBe(RuleAction.Inline)
  })

  it('requires a selected rule set for a new rule and keeps insertion-point ordering', async () => {
    const marker = { ...DefaultRouteRule(), id: RuleType.InsertionPoint }
    const rules = [marker]
    await mount(rules)
    exposed.handleAdd()
    await nextTick()
    await vi.advanceTimersByTimeAsync(1)
    const outbound = host.querySelector<HTMLSelectElement>('[data-rule-outbound] select')!
    outbound.value = 'direct'
    outbound.dispatchEvent(new Event('change', { bubbles: true }))
    save()
    expect(errors).toHaveBeenCalledWith('kernel.route.rules.ruleSetRequired')
    expect(rules).toHaveLength(1)
    host.querySelector<HTMLElement>('.rule-set-card')!.click()
    save()
    expect(rules).toHaveLength(2)
    expect(rules[0]).toBe(marker)
    expect(rules[1]!.rule_set).toEqual(['set'])
  })

  it('does not restore explicitly removed raw JSON from an editor waiting to unmount', async () => {
    const rules = [
      {
        ...DefaultRouteRule(),
        action: RuleAction.Reject,
        domain: ['example.org'],
        raw: '{"action":"reject"}',
      },
    ]
    await mount(rules)
    await edit()
    host
      .querySelector<HTMLButtonElement>('[aria-label="common.delete kernel.rules.type.inline"]')!
      .click()
    save()
    expect(rules[0]!.raw).toBe('')
    expect(rules[0]!.domain).toEqual(['example.org'])
  })
})
