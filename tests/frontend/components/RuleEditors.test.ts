// @vitest-environment jsdom
import { createApp, defineComponent, h, nextTick, ref, type App, type Component } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

const mocks = vi.hoisted(() => ({ error: vi.fn(), load: vi.fn(), save: vi.fn(), submit: vi.fn() }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/lang', () => ({ default: { global: { t: (key: string) => key } } }))
vi.mock('@/utils', async () => ({
  ...(await import('@/utils/others')),
  ...(await import('@/utils/is')),
  renderRouteRulePreview: () => 'complete route preview',
  renderDnsRulePreview: () => 'complete DNS preview',
  message: { error: mocks.error, warn: vi.fn() },
}))
vi.mock('@/stores', () => ({
  isResourceConflict: () => false,
  isResourceNotFound: () => false,
  useRulesetsStore: () => ({
    rulesets: [],
    getRulesetById: () => ({ id: 'local', tag: 'local', type: 'Manual' }),
    getRulesetContentWithRevision: mocks.load,
    saveRulesetContent: mocks.save,
  }),
}))

import { DefaultDnsRule, DefaultRouteRule, DefaultRouteRuleset } from '@/constant/profile'
import { RuleAction, RulesetType } from '@/enums/kernel'
import RuleBuilder from '@/components/RuleBuilder/index.vue'
import RouteRulesConfig from '@/views/ProfilesView/components/RouteRulesConfig.vue'
import DnsRulesConfig from '@/views/ProfilesView/components/DnsRulesConfig.vue'
import RouteRulesetConfig from '@/views/ProfilesView/components/RouteRulesetConfig.vue'
import RulesetView from '@/views/RulesetsView/components/RulesetView.vue'

const editor = defineComponent({
  props: ['modelValue'],
  setup(props, { expose }) {
    const area = ref<HTMLTextAreaElement>()
    expose({ getContent: () => area.value?.value ?? props.modelValue })
    return () => h('textarea', { ref: area, value: props.modelValue })
  },
})
const button = defineComponent({
  props: ['icon'],
  setup:
    (props, { slots }) =>
    () =>
      h('button', { 'data-icon': props.icon }, slots.default?.()),
})
const modal = defineComponent({
  props: ['open', 'onOk'],
  setup:
    (props, { slots }) =>
    () =>
      props.open &&
      h('section', { 'data-modal': '' }, [
        slots.default?.(),
        h('button', { 'data-save': '', onClick: props.onOk }, 'save'),
      ]),
})

describe('rule editing preserves current drafts and official operation order', () => {
  let app: App
  let host: HTMLDivElement
  beforeEach(() => {
    vi.resetAllMocks()
    mocks.load.mockResolvedValue({
      content: '{"version":3,"rules":[]}',
      revision: { instanceId: 'one', revision: 4n },
    })
    mocks.save.mockResolvedValue(undefined)
    host = document.createElement('div')
    document.body.append(host)
  })
  afterEach(() => {
    app?.unmount()
    host.remove()
  })
  const mount = async (component: Component, props: Record<string, unknown>) => {
    const instance = ref<any>()
    app = createApp({
      render: () =>
        h('div', [
          h(component, { ...props, ref: instance }),
          h('footer', instance.value?.modalSlots?.submit?.()),
        ]),
    })
    app.config.globalProperties.$t = (key: string) => key
    app.provide('submit', mocks.submit)
    app.provide('cancel', vi.fn())
    app.directive('tips', {})
    app.directive('draggable', {})
    app.component('Button', button)
    app.component('Modal', modal)
    app.component('CodeViewer', editor)
    app.component('RuleBuilder', RuleBuilder)
    for (const name of ['Card', 'Divider'])
      app.component(name, {
        setup:
          (_, { slots }) =>
          () =>
            h('div', slots.default?.()),
      })
    for (const name of [
      'Empty',
      'Switch',
      'Input',
      'InputList',
      'OptionGroup',
      'Radio',
      'Select',
      'Tag',
      'Icon',
      'ActionPicker',
      'MatchGroupAdder',
    ])
      app.component(name, { render: () => null })
    app.mount(host)
    await nextTick()
    await new Promise((resolve) => setTimeout(resolve, 1))
    return instance
  }
  const edit = async () => {
    host.querySelector<HTMLButtonElement>('button[data-icon="edit"]')!.click()
    await nextTick()
    await new Promise((resolve) => setTimeout(resolve, 1))
  }

  for (const [name, component, createRule] of [
    ['route', RouteRulesConfig, DefaultRouteRule],
    ['DNS', DnsRulesConfig, DefaultDnsRule],
  ] as const) {
    it(`${name}: clears only missing ruleset references and reads the latest inline JSON on save`, async () => {
      const rule = {
        ...createRule(),
        action: RuleAction.Inline,
        raw: '{"action":"reject"}',
        rule_set: ['keep', 'missing'],
        domain: ['example.org'],
      }
      const rules = [rule]
      await mount(component, {
        modelValue: rules,
        inboundOptions: [],
        outboundOptions: [],
        serverOptions: [],
        serversOptions: [],
        ruleSet: [{ ...DefaultRouteRuleset(), id: 'keep', tag: 'keep' }],
      })
      const clear = [...host.querySelectorAll('button')].find(
        (entry) => entry.textContent === 'common.clear',
      )!
      clear.click()
      expect(rule.rule_set).toEqual(['keep'])
      expect(rule.domain).toEqual(['example.org'])
      await edit()
      if (name === 'route') {
        expect(host.querySelector('.shared-rule-pane')).toBeNull()
        expect(
          [
            ...host.querySelectorAll('[data-rule-type], [data-rule-action], [data-rule-invert]'),
          ].map((entry) => entry.textContent?.trim()),
        ).toEqual([
          'kernel.route.rules.type',
          'kernel.route.rules.action.name',
          'kernel.route.rules.invert',
        ])
      } else {
        expect(
          [...host.querySelectorAll('.shared-rule-pane')].map((entry) =>
            entry.classList.contains('shared-rule-match') ? 'match' : 'action',
          ),
        ).toEqual(['match', 'action'])
      }
      const area = host.querySelector('textarea')!
      area.value = '{bad JSON'
      host.querySelector<HTMLButtonElement>('[data-save]')!.click()
      expect(mocks.error).toHaveBeenCalledOnce()
      expect(rules[0]!.raw).toBe('{"action":"reject"}')
      area.value = '{"action":"reject","method":"drop"}'
      host.querySelector<HTMLButtonElement>('[data-save]')!.click()
      expect(rules[0]!.raw).toBe(area.value)
      expect(rules[0]!.domain).toEqual(['example.org'])
      expect(rules[0]!.id).toBe(rule.id)
    })
  }

  it('inline ruleset edits validate the current document before committing', async () => {
    const rules = [
      { ...DefaultRouteRuleset(), type: RulesetType.Inline, tag: 'inline', rules: '[]' },
    ]
    await mount(RouteRulesetConfig, { modelValue: rules, outboundOptions: [] })
    await edit()
    const area = host.querySelector('textarea')!
    area.value = '{}'
    host.querySelector<HTMLButtonElement>('[data-save]')!.click()
    expect(mocks.error).toHaveBeenCalledWith('ruleset.invalidInlineRules')
    expect(rules[0]!.rules).toBe('[]')
    area.value = '[{"domain":["example.org"]}]'
    host.querySelector<HTMLButtonElement>('[data-save]')!.click()
    expect(rules[0]!.rules).toBe(area.value)
  })

  it('cached ruleset source saves the latest document with its original revision', async () => {
    await mount(RulesetView, { id: 'local' })
    await vi.waitFor(() => expect(host.querySelector('textarea')!.value).toContain('"version":3'))
    const area = host.querySelector('textarea')!
    area.value = '{"version":3,"rules":[{"domain":["example.org"]}]}'
    host.querySelector<HTMLElement>('footer [role="button"]')!.click()
    await vi.waitFor(() => expect(mocks.save).toHaveBeenCalledOnce())
    expect(mocks.save).toHaveBeenCalledWith('local', area.value, {
      instanceId: 'one',
      revision: 4n,
    })
    expect(mocks.submit).toHaveBeenCalledOnce()
  })
})
