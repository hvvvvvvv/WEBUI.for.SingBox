<script lang="ts" setup>
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'

import { DraggableOptions } from '@/constant/app'
import { DefaultActionOptions, DefaultRouteRule } from '@/constant/profile'
import { RuleAction, RuleType } from '@/enums/kernel'
import { useBool } from '@/hooks'
import { deepClone, message, renderRouteRulePreview, sampleID } from '@/utils'
import MatchFieldEditor from './RouteRuleEditors/MatchFieldEditor.vue'
import {
  clearRouteMatch,
  hasRouteMatch,
  routeMatchFields,
  routeMatchLabel,
  type RouteMatchField,
} from './RouteRuleEditors/matchFields'
import RejectActionEditor from './RouteRuleEditors/RejectActionEditor.vue'
import ResolveActionEditor from './RouteRuleEditors/ResolveActionEditor.vue'
import RouteOptionsEditor from './RouteRuleEditors/RouteOptionsEditor.vue'
import RuleSetCardPicker from './RuleSetCardPicker.vue'
import SniffActionEditor from './RouteRuleEditors/SniffActionEditor.vue'

interface Props {
  inboundOptions: { label: string; value: string; disabled?: boolean }[]
  outboundOptions: { label: string; value: string }[]
  serverOptions: { label: string; value: string }[]
  ruleSet: IRuleSet[]
}

const props = defineProps<Props>()
const model = defineModel<IRule[]>({ required: true })
const fields = ref<IRule>(DefaultRouteRule())
const selectedType = ref<RouteMatchField | 'combined'>('rule_set')
const matchFields = ref<RouteMatchField[]>([])
const matchEditors = new Map<RouteMatchField, { getContent(): string }>()
let ruleIndex = -1
const { t } = useI18n()
const [showEditModal] = useBool(false)

const typeOptions = [
  ...routeMatchFields.map((field) => ({ label: routeMatchLabel(field), value: field })),
  { label: 'kernel.route.rules.combinedMatch', value: 'combined' },
]
const actionOptions = [
  RuleAction.Route,
  RuleAction.Bypass,
  RuleAction.RouteOptions,
  RuleAction.Reject,
  RuleAction.HijackDNS,
  RuleAction.Sniff,
  RuleAction.Resolve,
  RuleAction.Inline,
].map((value) => ({ label: `kernel.route.rules.action.${value}`, value }))
const additionalFields = computed(() =>
  matchFields.value.filter(
    (field): field is Exclude<RouteMatchField, 'rule_set'> =>
      field !== selectedType.value && field !== 'rule_set',
  ),
)
const showRuleSets = computed(
  () => selectedType.value === 'rule_set' || matchFields.value.includes('rule_set'),
)
const showInlineHint = computed(
  () => selectedType.value === 'raw' || matchFields.value.includes('raw'),
)
const hasAdvancedOptions = computed(() => {
  const defaults = DefaultActionOptions()
  const keys = [
    'override_address',
    'override_port',
    'network_strategy',
    'network_type',
    'fallback_network_type',
    'fallback_delay',
    'udp_disable_domain_unmapping',
    'udp_connect',
    'udp_timeout',
    'tls_fragment',
    'tls_fragment_fallback_delay',
    'tls_record_fragment',
    'tls_spoof',
    'tls_spoof_method',
  ] as const
  return keys.some(
    (key) => JSON.stringify(fields.value.action_options[key]) !== JSON.stringify(defaults[key]),
  )
})
const setMatchEditor = (field: RouteMatchField, editor: unknown) => {
  if (editor) matchEditors.set(field, editor as { getContent(): string })
  else matchEditors.delete(field)
}
const matchEditorRef = (field: RouteMatchField) => (editor: unknown) =>
  setMatchEditor(field, editor)
const readInline = () => {
  const editor = matchEditors.get('raw')
  if (editor) fields.value.raw = editor.getContent()
}
const configureEditor = () => {
  matchEditors.clear()
  matchFields.value = routeMatchFields.filter((field) => hasRouteMatch(fields.value, field))
  selectedType.value = matchFields.value.length === 1 ? matchFields.value[0]! : 'combined'
}
const changeType = (type: string | string[]) => {
  if (Array.isArray(type)) return
  readInline()
  // Changing the editing entry never silently removes another effective match.
  matchFields.value = routeMatchFields.filter((field) => hasRouteMatch(fields.value, field))
  selectedType.value = type as RouteMatchField | 'combined'
  if (type !== 'combined' && !matchFields.value.includes(type as RouteMatchField))
    matchFields.value.push(type as RouteMatchField)
}
const removeMatch = (field: RouteMatchField) => {
  matchEditors.delete(field)
  clearRouteMatch(fields.value, field)
  matchFields.value = matchFields.value.filter((entry) => entry !== field)
}
const handleAdd = () => {
  ruleIndex = -1
  fields.value = DefaultRouteRule()
  configureEditor()
  selectedType.value = 'rule_set'
  showEditModal.value = true
}
defineExpose({ handleAdd })

const handleAddInsertionPoint = () => {
  const rule = DefaultRouteRule()
  rule.id = RuleType.InsertionPoint
  model.value.unshift(rule)
}

const parseRawObject = (raw: string): Record<string, unknown> | null => {
  if (!raw.trim()) return {}
  try {
    const parsed = JSON.parse(raw)
    return parsed && typeof parsed === 'object' && !Array.isArray(parsed) ? parsed : null
  } catch {
    return null
  }
}

const validateRule = () => {
  const hasInlineGroup = !!fields.value.raw.trim() || showInlineHint.value
  const raw = hasInlineGroup ? parseRawObject(fields.value.raw) : {}
  if (hasInlineGroup && raw === null) return 'kernel.route.rules.inlineInvalidPayload'
  if (fields.value.action === RuleAction.Inline) {
    if (!hasInlineGroup || !fields.value.raw.trim()) return 'kernel.route.rules.inlineGroupRequired'
    if (typeof raw?.action !== 'string' || !raw.action.trim()) {
      return 'kernel.route.rules.inlineActionRequired'
    }
  }

  const effectiveAction = typeof raw?.action === 'string' ? raw.action : fields.value.action
  const effectiveOutbound =
    typeof raw?.outbound === 'string' ? raw.outbound : fields.value.action_options.outbound
  if (effectiveAction === RuleAction.Route && !effectiveOutbound) {
    return 'kernel.route.rules.routeOutboundRequired'
  }
  if (
    effectiveAction === RuleAction.Route &&
    selectedType.value === 'rule_set' &&
    !routeMatchFields.some((field) => hasRouteMatch(fields.value, field))
  ) {
    return 'kernel.route.rules.ruleSetRequired'
  }
  if (fields.value.action_options.tls_fragment && fields.value.action_options.tls_record_fragment) {
    return 'kernel.route.rules.tlsFragmentConflict'
  }
  if (
    fields.value.action === RuleAction.Reject &&
    fields.value.action_options.method === 'drop' &&
    fields.value.action_options.no_drop
  ) {
    return 'kernel.route.rules.noDropConflict'
  }
  const ports = [...fields.value.source_port, ...fields.value.port]
  if (ports.some((port) => !Number.isInteger(port) || port < 0 || port > 65535)) {
    return 'kernel.route.rules.portInvalid'
  }
  return ''
}

const handleAddEnd = () => {
  readInline()
  const validationError = validateRule()
  if (validationError) {
    message.error(validationError)
    return false
  }

  if (ruleIndex !== -1) {
    model.value[ruleIndex] = fields.value
  } else {
    const insertionIndex = model.value.findIndex((rule) => rule.id === RuleType.InsertionPoint)
    if (insertionIndex !== -1) model.value.splice(insertionIndex + 1, 0, fields.value)
    else model.value.unshift(fields.value)
  }
  return true
}

const handleEdit = (index: number) => {
  ruleIndex = index
  fields.value = deepClone(model.value[index]!)
  configureEditor()
  showEditModal.value = true
}

const handleCopy = (index: number) => {
  ruleIndex = -1
  fields.value = deepClone(model.value[index]!)
  fields.value.id = sampleID()
  configureEditor()
  showEditModal.value = true
}

const handleDelete = (index: number) => model.value.splice(index, 1)

const missingRuleSets = (rule: IRule) =>
  rule.rule_set.some((id) => props.ruleSet.every((item) => item.id !== id))
const clearMissingRuleSets = (rule: IRule) => {
  rule.rule_set = rule.rule_set.filter((id) => props.ruleSet.some((item) => item.id === id))
}

const isInsertionPointMissing = computed(() =>
  model.value.every((rule) => rule.id !== RuleType.InsertionPoint),
)

const isMissing = (options: { value: string; disabled?: boolean }[], id: string) => {
  const option = options.find((entry) => entry.value === id)
  return !option || option.disabled
}

const hasLost = (rule: IRule) => {
  if (!rule.enable) return false
  if (rule.inbound.some((id) => isMissing(props.inboundOptions, id))) return true
  if (rule.rule_set.some((id) => props.ruleSet.every((item) => item.id !== id))) return true
  if (
    [RuleAction.Route, RuleAction.Bypass].includes(rule.action as any) &&
    rule.action_options.outbound &&
    isMissing(props.outboundOptions, rule.action_options.outbound)
  ) {
    return true
  }
  const raw = parseRawObject(rule.raw)
  const effectiveAction = typeof raw?.action === 'string' ? raw.action : rule.action
  const effectiveOutbound =
    typeof raw?.outbound === 'string' ? raw.outbound : rule.action_options.outbound
  if (effectiveAction === RuleAction.Route && !effectiveOutbound) return true
  if (
    rule.action === RuleAction.Resolve &&
    rule.action_options.server &&
    isMissing(props.serverOptions, rule.action_options.server)
  ) {
    return true
  }
  if (rule.raw.trim() && raw === null) return true
  if (
    rule.action === RuleAction.Inline &&
    (!rule.raw.trim() || typeof raw?.action !== 'string' || !raw.action.trim())
  ) {
    return true
  }
  return false
}

const renderRule = (rule: IRule) =>
  renderRouteRulePreview(rule, {
    inboundOptions: props.inboundOptions,
    outboundOptions: props.outboundOptions,
    serverOptions: props.serverOptions,
    ruleSetOptions: props.ruleSet.map((ruleSet) => ({
      label: ruleSet.tag,
      value: ruleSet.id,
    })),
  })
</script>

<template>
  <Empty v-if="model.length === 0 || (model.length === 1 && !isInsertionPointMissing)">
    <template #description>
      <Button icon="add" type="primary" size="small" @click="handleAdd">
        {{ t('common.add') }}
      </Button>
    </template>
  </Empty>

  <Divider v-if="isInsertionPointMissing">
    <Button type="text" size="small" @click="handleAddInsertionPoint">
      {{ t('kernel.addInsertionPoint') }}
    </Button>
  </Divider>

  <div v-draggable="[model, DraggableOptions]">
    <Card v-for="(rule, index) in model" :key="rule.id" class="mb-2">
      <div v-if="rule.id === RuleType.InsertionPoint" class="text-center font-bold">
        <Divider class="cursor-move">
          <Button icon="add" type="text" size="small" @click="handleAdd">
            {{ t('kernel.insertionPoint') }}
          </Button>
        </Divider>
      </div>
      <div v-else class="rule-row flex items-start py-2 gap-8">
        <Switch v-model="rule.enable" border="square" size="small" class="shrink-0" />
        <div v-tips.fast.overflow="renderRule(rule)" class="font-bold flex-1 rule-content">
          <span
            v-if="hasLost(rule)"
            class="cursor-pointer"
            :style="{ color: 'rgb(200, 193, 11)' }"
            @click="message.warn('kernel.route.rules.invalid')"
          >
            [ ! ]
          </span>
          {{ renderRule(rule) }}
        </div>
        <div class="rule-actions ml-auto shrink-0">
          <Button
            v-if="missingRuleSets(rule)"
            type="text"
            size="small"
            @click="clearMissingRuleSets(rule)"
          >
            {{ t('common.clear') }}
          </Button>
          <Button icon="copy" type="text" size="small" @click="handleCopy(index)" />
          <Button icon="edit" type="text" size="small" @click="handleEdit(index)" />
          <Button icon="delete" type="text" size="small" @click="handleDelete(index)" />
        </div>
      </div>
    </Card>
  </div>

  <Modal
    v-model:open="showEditModal"
    :on-ok="handleAddEnd"
    title="kernel.route.tab.rules"
    max-width="80"
    max-height="80"
  >
    <div class="route-rule-editor">
      <div class="form-item" data-rule-type>
        {{ t('kernel.route.rules.type') }}
        <Select
          :model-value="selectedType"
          :options="typeOptions"
          @update:model-value="changeType"
        />
      </div>
      <div class="form-item" data-rule-action>
        {{ t('kernel.route.rules.action.name') }}
        <Radio v-model="fields.action" :options="actionOptions" class="rule-action-options ml-8" />
      </div>
      <MatchFieldEditor
        v-if="selectedType !== 'rule_set' && selectedType !== 'combined'"
        :ref="matchEditorRef(selectedType)"
        :key="selectedType"
        v-model="fields"
        :field="selectedType"
        :inbound-options="inboundOptions"
      />
      <p v-if="selectedType === 'combined' && !matchFields.length" class="text-12 opacity-60 my-8">
        {{ t('kernel.route.rules.matchAll') }}
      </p>
      <div class="form-item" data-rule-invert>
        {{ t('kernel.route.rules.invert') }}
        <Switch v-model="fields.invert" />
      </div>
      <Card
        v-if="fields.action !== RuleAction.Inline && fields.action !== RuleAction.HijackDNS"
        class="mt-4 mb-16 rule-action-parameters"
      >
        <template v-if="fields.action === RuleAction.Route || fields.action === RuleAction.Bypass">
          <div class="form-item" data-rule-outbound>
            {{ t('kernel.route.rules.outbound') }}
            <Select v-model="fields.action_options.outbound" :options="outboundOptions" clearable />
          </div>
          <details :open="hasAdvancedOptions" class="rule-advanced-options">
            <summary class="cursor-pointer py-8 text-12">
              {{ t('kernel.route.rules.advancedOptions') }}
            </summary>
            <RouteOptionsEditor v-model="fields.action_options" />
          </details>
        </template>
        <RouteOptionsEditor
          v-else-if="fields.action === RuleAction.RouteOptions"
          v-model="fields.action_options"
        />
        <RejectActionEditor
          v-else-if="fields.action === RuleAction.Reject"
          v-model="fields.action_options"
        />
        <SniffActionEditor
          v-else-if="fields.action === RuleAction.Sniff"
          v-model="fields.action_options"
        />
        <ResolveActionEditor
          v-else-if="fields.action === RuleAction.Resolve"
          v-model="fields.action_options"
          :server-options="serverOptions"
        />
      </Card>
      <template v-if="additionalFields.length">
        <Divider>{{ t('kernel.route.rules.additionalMatches') }}</Divider>
        <div v-for="field in additionalFields" :key="field" class="additional-rule-match">
          <MatchFieldEditor
            :ref="matchEditorRef(field)"
            v-model="fields"
            :field="field"
            :inbound-options="inboundOptions"
          />
          <Button
            type="text"
            size="small"
            icon="delete"
            :aria-label="`${t('common.delete')} ${t(routeMatchLabel(field))}`"
            @click="removeMatch(field)"
          />
        </div>
      </template>
      <p v-if="showInlineHint" class="text-12 opacity-60 my-8">
        {{ t('kernel.route.rules.rawOverrideHint') }}
      </p>
      <template v-if="showRuleSets">
        <Divider>
          {{ t('kernel.route.tab.rule_set') }}
          <Button
            v-if="selectedType !== 'rule_set'"
            type="text"
            size="small"
            icon="delete"
            :aria-label="t('common.delete')"
            @click="removeMatch('rule_set')"
          />
        </Divider>
        <RuleSetCardPicker v-model="fields.rule_set" :rule-sets="ruleSet" />
      </template>
    </div>
  </Modal>
</template>

<style lang="less" scoped>
.rule-content {
  min-width: 0;
  overflow-wrap: anywhere;
  white-space: normal;
}

.route-rule-editor {
  min-width: 0;
}
.route-rule-editor :deep(.form-item) {
  min-width: 0;
  gap: 8px 16px;
  overflow-wrap: anywhere;
}
.rule-action-options {
  max-width: 100%;
  overflow-x: auto;
}
.route-rule-editor [data-rule-action] {
  flex-wrap: wrap;
}
.rule-action-options :deep(.gui-radio-button) {
  flex-shrink: 0;
}
.additional-rule-match {
  display: flex;
  align-items: flex-start;
  gap: 8px;
}
.additional-rule-match > :first-child {
  flex: 1;
  min-width: 0;
}
.additional-rule-match > :last-child {
  flex-shrink: 0;
  margin-top: 8px;
}

@media (max-width: 700px) {
  .route-rule-editor :deep(.form-item) {
    flex-wrap: wrap;
  }
  .route-rule-editor :deep(.gui-radio) {
    max-width: 100%;
    overflow-x: auto;
  }
  .rule-action-options {
    margin-left: 0;
  }
  .route-rule-editor :deep(.form-item > .gui-dropdown),
  .route-rule-editor :deep(.form-item > .gui-input),
  .route-rule-editor :deep(.form-item > .gui-input-list) {
    min-width: 0;
    width: 100%;
  }
  .rule-row {
    display: grid;
    grid-template-columns: auto minmax(0, 1fr);
  }
  .rule-actions {
    grid-column: 1 / -1;
    justify-self: end;
    display: flex;
    flex-wrap: wrap;
    justify-content: flex-end;
    gap: 4px;
    margin-left: 0;
  }
}
</style>
