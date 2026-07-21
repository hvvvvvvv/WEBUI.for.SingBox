<script lang="ts" setup>
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'

import { DraggableOptions } from '@/constant/app'
import {
  ModeOptions,
  RouteRuleIPVersionOptions,
  RouteRuleNetworkOptions,
  RouteRulePreferredByOptions,
  RouteRuleProtocolOptions,
} from '@/constant/kernel'
import { DefaultActionOptions, DefaultRouteRule } from '@/constant/profile'
import { RuleAction, RuleType } from '@/enums/kernel'
import { useBool } from '@/hooks'
import { deepClone, message } from '@/utils'

import BypassActionEditor from './RouteRuleEditors/BypassActionEditor.vue'
import DestinationMatchGroup from './RouteRuleEditors/DestinationMatchGroup.vue'
import InlineMatchGroup from './RouteRuleEditors/InlineMatchGroup.vue'
import MatchGroupAdder from './RouteRuleEditors/MatchGroupAdder.vue'
import ProcessMatchGroup from './RouteRuleEditors/ProcessMatchGroup.vue'
import RejectActionEditor from './RouteRuleEditors/RejectActionEditor.vue'
import ResolveActionEditor from './RouteRuleEditors/ResolveActionEditor.vue'
import RouteActionEditor from './RouteRuleEditors/RouteActionEditor.vue'
import RouteActionPicker from './RouteRuleEditors/RouteActionPicker.vue'
import RouteOptionsEditor from './RouteRuleEditors/RouteOptionsEditor.vue'
import RuleSetMatchGroup from './RouteRuleEditors/RuleSetMatchGroup.vue'
import SniffActionEditor from './RouteRuleEditors/SniffActionEditor.vue'
import SourceMatchGroup from './RouteRuleEditors/SourceMatchGroup.vue'

interface Props {
  inboundOptions: { label: string; value: string; disabled?: boolean }[]
  outboundOptions: { label: string; value: string }[]
  serverOptions: { label: string; value: string }[]
  ruleSet: IRuleSet[]
}

type GroupKey = 'source' | 'destination' | 'process' | 'rule_set' | 'inline'

const DefaultInlineRaw = '{\n  \n}'

const props = defineProps<Props>()
const model = defineModel<IRule[]>({ required: true })
const fields = ref<IRule>(DefaultRouteRule())
const activeGroups = ref<Set<GroupKey>>(new Set())
let ruleIndex = -1

const { t } = useI18n()
const [showEditModal] = useBool(false)

const groupOptions: { key: GroupKey; label: string }[] = [
  { key: 'source', label: 'kernel.route.rules.groups.source' },
  { key: 'destination', label: 'kernel.route.rules.groups.destination' },
  { key: 'process', label: 'kernel.route.rules.groups.process' },
  { key: 'rule_set', label: 'kernel.route.rules.groups.rule_set' },
  { key: 'inline', label: 'kernel.route.rules.groups.inline' },
]

const actionsWithOptions = new Set<RuleAction>([
  RuleAction.Route,
  RuleAction.Bypass,
  RuleAction.RouteOptions,
  RuleAction.Reject,
  RuleAction.Sniff,
  RuleAction.Resolve,
])

const availableGroupOptions = computed(() =>
  groupOptions
    .filter((group) => !activeGroups.value.has(group.key))
    .map((group) => ({ ...group, label: t(group.label) })),
)

const hasActionOptions = computed(() =>
  actionsWithOptions.has(fields.value.action as RuleAction),
)

const ruleSetOptions = computed(() =>
  props.ruleSet.map((ruleSet) => ({ label: ruleSet.tag, value: ruleSet.id })),
)

const inferActiveGroups = (rule: IRule) => {
  const groups = new Set<GroupKey>()
  if (
    rule.source_ip_cidr.length ||
    rule.source_ip_is_private ||
    rule.source_port.length ||
    rule.source_port_range.length
  ) {
    groups.add('source')
  }
  if (
    rule.domain.length ||
    rule.domain_suffix.length ||
    rule.domain_keyword.length ||
    rule.domain_regex.length ||
    rule.ip_cidr.length ||
    rule.ip_is_private ||
    rule.port.length ||
    rule.port_range.length
  ) {
    groups.add('destination')
  }
  if (rule.process_name.length || rule.process_path.length || rule.process_path_regex.length) {
    groups.add('process')
  }
  if (rule.rule_set.length) groups.add('rule_set')
  if (rule.raw.trim()) groups.add('inline')
  activeGroups.value = groups
}

const handleAdd = () => {
  ruleIndex = -1
  fields.value = DefaultRouteRule()
  activeGroups.value = new Set()
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
  const hasInlineGroup = activeGroups.value.has('inline')
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
  const validationError = validateRule()
  if (validationError) {
    message.error(validationError)
    return false
  }
  if (!activeGroups.value.has('inline')) fields.value.raw = ''
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
  inferActiveGroups(fields.value)
  showEditModal.value = true
}

const handleDelete = (index: number) => model.value.splice(index, 1)

const addGroup = (key: GroupKey) => {
  if (key === 'inline' && !fields.value.raw.trim()) fields.value.raw = DefaultInlineRaw
  activeGroups.value = new Set([...activeGroups.value, key])
}

const removeGroup = (key: GroupKey) => {
  const next = new Set(activeGroups.value)
  next.delete(key)
  activeGroups.value = next
  if (key === 'source') {
    fields.value.source_ip_cidr = []
    fields.value.source_ip_is_private = false
    fields.value.source_port = []
    fields.value.source_port_range = []
  } else if (key === 'destination') {
    fields.value.domain = []
    fields.value.domain_suffix = []
    fields.value.domain_keyword = []
    fields.value.domain_regex = []
    fields.value.ip_cidr = []
    fields.value.ip_is_private = false
    fields.value.port = []
    fields.value.port_range = []
  } else if (key === 'process') {
    fields.value.process_name = []
    fields.value.process_path = []
    fields.value.process_path_regex = []
  } else if (key === 'rule_set') {
    fields.value.rule_set = []
  } else {
    fields.value.raw = ''
  }
}

const handleActionChange = () => {
  fields.value.action_options = DefaultActionOptions()
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

const referenceLabels = (ids: string[], options: { label: string; value: string }[]) =>
  ids.map((id) => options.find((entry) => entry.value === id)?.label || id).join('|')

const renderRule = (rule: IRule) => {
  const matches: string[] = []
  if (rule.inbound.length)
    matches.push(`inbound=${referenceLabels(rule.inbound, props.inboundOptions)}`)
  if (rule.ip_version) matches.push(`ip_version=${rule.ip_version}`)
  if (rule.network.length) matches.push(`network=${rule.network.join('|')}`)
  if (rule.preferred_by.length) matches.push(`preferred_by=${rule.preferred_by.join('|')}`)
  if (rule.protocol.length) matches.push(`protocol=${rule.protocol.join('|')}`)
  if (rule.clash_mode) matches.push(`clash_mode=${rule.clash_mode}`)
  if (rule.source_ip_cidr.length || rule.source_port.length || rule.source_port_range.length) {
    matches.push(t('kernel.route.rules.groups.source'))
  }
  if (
    rule.domain.length ||
    rule.domain_suffix.length ||
    rule.domain_keyword.length ||
    rule.domain_regex.length ||
    rule.ip_cidr.length ||
    rule.port.length ||
    rule.port_range.length
  ) {
    matches.push(t('kernel.route.rules.groups.destination'))
  }
  if (rule.process_name.length || rule.process_path.length || rule.process_path_regex.length) {
    matches.push(t('kernel.route.rules.groups.process'))
  }
  if (rule.rule_set.length)
    matches.push(`rule_set=${referenceLabels(rule.rule_set, ruleSetOptions.value)}`)
  if (rule.raw.trim()) matches.push('raw')

  const action: string[] = [rule.action]
  if (rule.action_options.outbound) {
    action.push(referenceLabels([rule.action_options.outbound], props.outboundOptions))
  }
  if (rule.action_options.server) {
    action.push(referenceLabels([rule.action_options.server], props.serverOptions))
  }
  return `${matches.join(', ') || '*'}${rule.invert ? ' (invert)' : ''} → ${action.join(': ')}`
}
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
      <div v-else class="flex items-start py-2 gap-8">
        <Switch v-model="rule.enable" border="square" size="small" class="shrink-0" />
        <div class="font-bold flex-1 rule-content">
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
        <div class="ml-auto shrink-0">
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
    max-width="90"
    max-height="90"
  >
    <div class="rule-builder">
      <section class="rule-builder-pane rule-action-pane rounded-8">
        <div class="rule-action-pane-top">
          <div class="rule-builder-heading flex items-center gap-8">
            <span class="rule-builder-keyword rounded-full">THEN</span>
            <span class="font-bold text-16">{{ t('kernel.route.rules.executeAction') }}</span>
          </div>
          <RouteActionPicker
            v-model="fields.action"
            @change="handleActionChange"
          />
        </div>

        <div v-if="hasActionOptions" class="action-editor-grid">
          <RouteActionEditor
            v-if="fields.action === RuleAction.Route"
            v-model="fields.action_options"
            :outbound-options="outboundOptions"
          />
          <BypassActionEditor
            v-else-if="fields.action === RuleAction.Bypass"
            v-model="fields.action_options"
            :outbound-options="outboundOptions"
          />
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
        </div>
      </section>

      <div class="rule-flow-arrow flex items-center justify-center" aria-hidden="true">
        <Icon icon="arrowRight" :size="20" color="var(--primary-color)" />
      </div>

      <section class="rule-builder-pane rule-match-pane rounded-8">
        <div class="rule-builder-heading flex items-center gap-8">
          <span class="rule-builder-keyword rounded-full">IF</span>
          <span class="font-bold text-16">{{ t('kernel.route.rules.matchConditions') }}</span>
        </div>

        <div class="form-item">
          {{ t('kernel.rules.type.inbound') }}
          <Select v-model="fields.inbound" :options="inboundOptions" multiple clearable />
        </div>
        <div class="form-item">
          {{ t('kernel.route.rules.fields.ip_version') }}
          <OptionGroup
            v-model="fields.ip_version"
            :options="RouteRuleIPVersionOptions"
            :empty-value="0"
            :aria-label="t('kernel.route.rules.fields.ip_version')"
            clearable
          />
        </div>
        <div class="form-item">
          {{ t('kernel.rules.type.network') }}
          <OptionGroup
            v-model="fields.network"
            :options="RouteRuleNetworkOptions"
            :aria-label="t('kernel.rules.type.network')"
            multiple
          />
        </div>
        <div class="form-item">
          {{ t('kernel.route.rules.fields.preferred_by') }}
          <OptionGroup
            v-model="fields.preferred_by"
            :options="RouteRulePreferredByOptions"
            :aria-label="t('kernel.route.rules.fields.preferred_by')"
            multiple
          />
        </div>
        <div class="form-item">
          {{ t('kernel.rules.type.protocol') }}
          <Select v-model="fields.protocol" :options="RouteRuleProtocolOptions" multiple clearable />
        </div>
        <div class="form-item">
          {{ t('kernel.rules.type.clash_mode') }}
          <OptionGroup
            v-model="fields.clash_mode"
            :options="ModeOptions"
            :aria-label="t('kernel.rules.type.clash_mode')"
            clearable
          />
        </div>
        <div class="form-item">
          {{ t('kernel.route.rules.invert') }}
          <Switch v-model="fields.invert" />
        </div>

        <MatchGroupAdder
          v-if="availableGroupOptions.length"
          :options="availableGroupOptions"
          @add="addGroup"
        />

        <Card
          v-if="activeGroups.has('source')"
          :title="t('kernel.route.rules.groups.source')"
          class="mb-8"
        >
          <template #extra>
            <Button
              icon="close"
              :icon-size="16"
              type="text"
              size="small"
              @click="removeGroup('source')"
            />
          </template>
          <SourceMatchGroup v-model="fields" />
        </Card>
        <Card
          v-if="activeGroups.has('destination')"
          :title="t('kernel.route.rules.groups.destination')"
          class="mb-8"
        >
          <template #extra>
            <Button
              icon="close"
              :icon-size="16"
              type="text"
              size="small"
              @click="removeGroup('destination')"
            />
          </template>
          <DestinationMatchGroup v-model="fields" />
        </Card>
        <Card
          v-if="activeGroups.has('process')"
          :title="t('kernel.route.rules.groups.process')"
          class="mb-8"
        >
          <template #extra>
            <Button
              icon="close"
              :icon-size="16"
              type="text"
              size="small"
              @click="removeGroup('process')"
            />
          </template>
          <ProcessMatchGroup v-model="fields" />
        </Card>
        <Card
          v-if="activeGroups.has('rule_set')"
          :title="t('kernel.route.rules.groups.rule_set')"
          class="mb-8"
        >
          <template #extra>
            <Button
              icon="close"
              :icon-size="16"
              type="text"
              size="small"
              @click="removeGroup('rule_set')"
            />
          </template>
          <RuleSetMatchGroup v-model="fields" :rule-sets="ruleSet" />
        </Card>
        <Card
          v-if="activeGroups.has('inline')"
          :title="t('kernel.route.rules.groups.inline')"
          class="mb-8"
        >
          <template #title-suffix>
            <Icon
              v-tips.fast="'kernel.route.rules.rawOverrideHint'"
              icon="messageWarn"
              :size="14"
              class="ml-4 shrink-0"
            />
          </template>
          <template #extra>
            <Button
              icon="close"
              :icon-size="16"
              type="text"
              size="small"
              @click="removeGroup('inline')"
            />
          </template>
          <InlineMatchGroup v-model="fields" />
        </Card>
      </section>
    </div>
  </Modal>
</template>

<style lang="less" scoped>
.rule-content {
  min-width: 0;
  word-break: break-all;
}

.rule-builder {
  display: grid;
  grid-template-columns: minmax(0, 3fr) 32px minmax(360px, 2fr);
  grid-template-areas: 'match arrow action';
  align-items: start;
  gap: 8px;
}

.rule-builder-pane {
  min-width: 0;
  padding: 12px;
  border: 1px solid color-mix(in srgb, var(--card-color) 12%, transparent);
  box-sizing: border-box;
}

.rule-builder-heading {
  min-height: 28px;
  margin-bottom: 10px;
}

.rule-builder-keyword {
  padding: 2px 8px;
  color: var(--primary-color);
  background: color-mix(in srgb, var(--primary-color) 11%, transparent);
  font-size: 10px;
  font-weight: 700;
  letter-spacing: 0.08em;
}

.rule-match-pane {
  grid-area: match;
  background: color-mix(in srgb, var(--card-bg) 76%, transparent);
}

.rule-action-pane {
  --action-pane-bg: color-mix(in srgb, var(--primary-color) 5%, var(--card-bg));

  grid-area: action;
  position: sticky;
  top: 0;
  align-self: start;
  max-height: calc(90vh - 120px);
  overflow-y: auto;
  background: var(--action-pane-bg);
  border-color: color-mix(in srgb, var(--primary-color) 28%, transparent);
  border-top: 3px solid var(--primary-color);
  container-type: inline-size;
}

.rule-action-pane-top {
  position: sticky;
  top: 0;
  z-index: 2;
  padding-bottom: 2px;
  background: var(--action-pane-bg);
}

.rule-flow-arrow {
  grid-area: arrow;
  width: 28px;
  height: 28px;
  margin-top: 14px;
  border-radius: 50%;
  background: color-mix(in srgb, var(--primary-color) 10%, transparent);
}

.action-editor-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  column-gap: 10px;
  row-gap: 12px;
  padding-top: 12px;
  margin-top: 12px;
  border-top: 1px solid color-mix(in srgb, var(--primary-color) 20%, transparent);
}

.action-editor-grid :deep(.action-field) {
  min-width: 0;
  padding: 0;
  flex-direction: column;
  align-items: stretch;
  justify-content: flex-start;
  gap: 6px;
}

.action-editor-grid :deep(.action-field-wide) {
  grid-column: 1 / -1;
}

.action-editor-grid :deep(.action-toggle-group) {
  display: grid;
  grid-column: 1 / -1;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 8px;
  min-width: 0;
}

.action-editor-grid :deep(.action-toggle-group > :only-child) {
  grid-column: 1 / -1;
}

.action-editor-grid :deep(.action-field > .gui-dropdown),
.action-editor-grid :deep(.action-field > .gui-input),
.action-editor-grid :deep(.action-field > .gui-input-list),
.action-editor-grid :deep(.action-field > .action-input-row),
.action-editor-grid :deep(.action-field .gui-select) {
  width: 100%;
  min-width: 0;
  max-width: 100%;
}

@container (max-width: 440px) {
  .action-editor-grid {
    grid-template-columns: 1fr;
  }

  .action-editor-grid :deep(.action-field-wide) {
    grid-column: auto;
  }
}

@container (max-width: 360px) {
  .action-editor-grid :deep(.action-toggle-group) {
    grid-template-columns: 1fr;
  }
}

@media (max-width: 900px) {
  .rule-builder {
    grid-template-columns: minmax(0, 1fr);
    grid-template-areas:
      'action'
      'match';
  }

  .rule-flow-arrow {
    display: none;
  }

  .rule-action-pane {
    position: static;
    max-height: none;
    overflow: visible;
  }

  .rule-action-pane-top {
    position: static;
  }
}
</style>
