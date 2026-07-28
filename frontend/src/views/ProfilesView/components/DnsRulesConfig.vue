<script lang="ts" setup>
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'

import type { ActionPickerItem } from '@/components/ActionPicker/index.vue'
import { DraggableOptions } from '@/constant/app'
import {
  DnsQueryTypeOptions,
  DnsRuleNetworkOptions,
  DnsRulePreferredByOptions,
  ModeOptions,
  RouteRuleIPVersionOptions,
  RouteRuleProtocolOptions,
} from '@/constant/kernel'
import { DefaultDnsActionOptions, DefaultDnsRule } from '@/constant/profile'
import { RuleAction, RuleActionReject, RuleType } from '@/enums/kernel'
import { useBool } from '@/hooks'
import { deepClone, message, renderDnsRulePreview, sampleID } from '@/utils'

import DomainMatchGroup from './DnsRuleEditors/DomainMatchGroup.vue'
import EvaluateActionEditor from './DnsRuleEditors/EvaluateActionEditor.vue'
import InlineMatchGroup from './DnsRuleEditors/InlineMatchGroup.vue'
import PredefinedActionEditor from './DnsRuleEditors/PredefinedActionEditor.vue'
import ProcessMatchGroup from './DnsRuleEditors/ProcessMatchGroup.vue'
import RejectActionEditor from './DnsRuleEditors/RejectActionEditor.vue'
import ResponseMatchGroup from './DnsRuleEditors/ResponseMatchGroup.vue'
import RouteActionEditor from './DnsRuleEditors/RouteActionEditor.vue'
import RouteOptionsActionEditor from './DnsRuleEditors/RouteOptionsActionEditor.vue'
import RuleSetMatchGroup from './DnsRuleEditors/RuleSetMatchGroup.vue'
import SourceMatchGroup from './DnsRuleEditors/SourceMatchGroup.vue'

interface Props {
  inboundOptions: { label: string; value: string; disabled?: boolean }[]
  serversOptions: { label: string; value: string }[]
  ruleSet: IRuleSet[]
}

type GroupKey = 'source' | 'domain' | 'rule_set' | 'response' | 'process' | 'inline'

const DefaultInlineRaw = '{\n  \n}'
const props = defineProps<Props>()
const model = defineModel<IDNSRule[]>({ required: true })
const fields = ref<IDNSRule>(DefaultDnsRule())
const activeGroups = ref<Set<GroupKey>>(new Set())
let ruleIndex = -1

const { t } = useI18n()
const [showEditModal] = useBool(false)

const groupOptions: { key: GroupKey; label: string }[] = [
  { key: 'rule_set', label: 'kernel.dns.rules.groups.rule_set' },
  { key: 'domain', label: 'kernel.dns.rules.groups.domain' },
  { key: 'source', label: 'kernel.dns.rules.groups.source' },
  { key: 'response', label: 'kernel.dns.rules.groups.response' },
  { key: 'process', label: 'kernel.dns.rules.groups.process' },
  { key: 'inline', label: 'kernel.dns.rules.groups.inline' },
]

const actionItems: ActionPickerItem[] = [
  {
    value: RuleAction.Route,
    label: 'kernel.route.rules.action.route',
    description: 'kernel.dns.rules.actionDescription.route',
    icon: 'forward',
  },
  {
    value: RuleAction.Evaluate,
    label: 'kernel.dns.rules.actionName.evaluate',
    description: 'kernel.dns.rules.actionDescription.evaluate',
    icon: 'preview',
  },
  {
    value: RuleAction.Respond,
    label: 'kernel.dns.rules.actionName.respond',
    description: 'kernel.dns.rules.actionDescription.respond',
    icon: 'backward',
  },
  {
    value: RuleAction.RouteOptions,
    label: 'kernel.route.rules.action.route-options',
    description: 'kernel.dns.rules.actionDescription.route-options',
    icon: 'settings3',
  },
  {
    value: RuleAction.Reject,
    label: 'kernel.route.rules.action.reject',
    description: 'kernel.dns.rules.actionDescription.reject',
    icon: 'forbidden',
  },
  {
    value: RuleAction.Predefined,
    label: 'kernel.route.rules.action.predefined',
    description: 'kernel.dns.rules.actionDescription.predefined',
    icon: 'file',
  },
  {
    value: RuleAction.Inline,
    label: 'kernel.route.rules.action.inline',
    description: 'kernel.dns.rules.actionDescription.inline',
    icon: 'code',
  },
]

const actionsWithOptions = new Set<string>([
  RuleAction.Route,
  RuleAction.Evaluate,
  RuleAction.RouteOptions,
  RuleAction.Reject,
  RuleAction.Predefined,
])

const availableGroupOptions = computed(() =>
  groupOptions
    .filter((group) => !activeGroups.value.has(group.key))
    .map((group) => ({ ...group, label: t(group.label) })),
)
const hasActionOptions = computed(() => actionsWithOptions.has(fields.value.action))

const hasResponseDependencies = (rule: IDNSRule) =>
  rule.ip_accept_any ||
  rule.ip_is_private ||
  rule.ip_cidr.length > 0 ||
  !!rule.response_rcode ||
  rule.response_answer.length > 0 ||
  rule.response_ns.length > 0 ||
  rule.response_extra.length > 0

const inferActiveGroups = (rule: IDNSRule) => {
  const groups = new Set<GroupKey>()
  if (
    rule.source_ip_cidr.length ||
    rule.source_ip_is_private ||
    rule.source_port.length ||
    rule.source_port_range.length
  )
    groups.add('source')
  if (
    rule.domain.length ||
    rule.domain_suffix.length ||
    rule.domain_keyword.length ||
    rule.domain_regex.length
  )
    groups.add('domain')
  if (rule.rule_set.length || rule.rule_set_ip_cidr_match_source) groups.add('rule_set')
  if (rule.match_response || hasResponseDependencies(rule)) groups.add('response')
  if (rule.process_name.length || rule.process_path.length || rule.process_path_regex.length)
    groups.add('process')
  if (rule.raw.trim()) groups.add('inline')
  activeGroups.value = groups
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
  if (hasInlineGroup && raw === null) return 'kernel.dns.rules.inlineInvalidPayload'
  if (fields.value.action === RuleAction.Inline) {
    if (!hasInlineGroup || !fields.value.raw.trim()) return 'kernel.dns.rules.inlineGroupRequired'
    if (typeof raw?.action !== 'string' || !raw.action.trim())
      return 'kernel.dns.rules.inlineActionRequired'
  }

  const effectiveAction = typeof raw?.action === 'string' ? raw.action : fields.value.action
  const effectiveServer =
    typeof raw?.server === 'string' ? raw.server : fields.value.action_options.server
  if (
    [RuleAction.Route, RuleAction.Evaluate].includes(effectiveAction as RuleAction) &&
    !effectiveServer
  ) {
    return 'kernel.dns.rules.serverRequired'
  }
  if (hasResponseDependencies(fields.value) && !fields.value.match_response) {
    return 'kernel.dns.rules.matchResponseRequired'
  }
  if (
    fields.value.action === RuleAction.Reject &&
    fields.value.action_options.method === RuleActionReject.Drop &&
    fields.value.action_options.no_drop
  ) {
    return 'kernel.route.rules.noDropConflict'
  }
  if (
    fields.value.query_type.some((value) => /^\d+$/.test(value.trim()) && Number(value) > 65535)
  ) {
    return 'kernel.dns.rules.queryTypeInvalid'
  }
  if (
    fields.value.source_port.some((port) => !Number.isInteger(port) || port < 0 || port > 65535)
  ) {
    return 'kernel.dns.rules.portInvalid'
  }
  return ''
}

const handleAdd = () => {
  ruleIndex = -1
  fields.value = DefaultDnsRule()
  activeGroups.value = new Set()
  showEditModal.value = true
}

defineExpose({ handleAdd })

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

const handleCopy = (index: number) => {
  ruleIndex = -1
  fields.value = deepClone(model.value[index]!)
  fields.value.id = sampleID()
  inferActiveGroups(fields.value)
  showEditModal.value = true
}

const handleAddInsertionPoint = () => {
  const rule = DefaultDnsRule()
  rule.id = RuleType.InsertionPoint
  model.value.unshift(rule)
}

const handleDelete = (index: number) => model.value.splice(index, 1)

const addGroup = (key: GroupKey) => {
  if (key === 'inline' && !fields.value.raw.trim()) fields.value.raw = DefaultInlineRaw
  if (key === 'response') fields.value.match_response = true
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
  } else if (key === 'domain') {
    fields.value.domain = []
    fields.value.domain_suffix = []
    fields.value.domain_keyword = []
    fields.value.domain_regex = []
  } else if (key === 'rule_set') {
    fields.value.rule_set = []
    fields.value.rule_set_ip_cidr_match_source = false
  } else if (key === 'response') {
    fields.value.match_response = false
    fields.value.ip_accept_any = false
    fields.value.ip_cidr = []
    fields.value.ip_is_private = false
    fields.value.response_rcode = ''
    fields.value.response_answer = []
    fields.value.response_ns = []
    fields.value.response_extra = []
  } else if (key === 'process') {
    fields.value.process_name = []
    fields.value.process_path = []
    fields.value.process_path_regex = []
  } else {
    fields.value.raw = ''
  }
}

const handleActionSelect = (action: string) => {
  fields.value.action = action as IDNSRule['action']
  fields.value.action_options = DefaultDnsActionOptions()
}

const isInsertionPointMissing = computed(() =>
  model.value.every((rule) => rule.id !== RuleType.InsertionPoint),
)

const isMissing = (options: { value: string; disabled?: boolean }[], id: string) => {
  const option = options.find((entry) => entry.value === id)
  return !option || option.disabled
}

const hasLost = (rule: IDNSRule) => {
  if (!rule.enable) return false
  if (rule.inbound.some((id) => isMissing(props.inboundOptions, id))) return true
  if (rule.rule_set.some((id) => props.ruleSet.every((item) => item.id !== id))) return true
  if (
    [RuleAction.Route, RuleAction.Evaluate].includes(rule.action as RuleAction) &&
    isMissing(props.serversOptions, rule.action_options.server)
  )
    return true
  if (hasResponseDependencies(rule) && !rule.match_response) return true
  const raw = parseRawObject(rule.raw)
  if (rule.raw.trim() && raw === null) return true
  if (
    rule.action === RuleAction.Inline &&
    (!rule.raw.trim() || typeof raw?.action !== 'string' || !raw.action.trim())
  )
    return true
  return false
}

const renderRule = (rule: IDNSRule) =>
  renderDnsRulePreview(rule, {
    inboundOptions: props.inboundOptions,
    serverOptions: props.serversOptions,
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
      <div v-else class="flex items-start py-2 gap-8">
        <Switch v-model="rule.enable" border="square" size="small" class="shrink-0" />
        <div v-tips.fast.overflow="renderRule(rule)" class="font-bold flex-1 rule-content">
          <span
            v-if="hasLost(rule)"
            class="warn cursor-pointer"
            @click="message.warn('kernel.dns.rules.invalid')"
          >
            [ ! ]
          </span>
          {{ renderRule(rule) }}
        </div>
        <div class="ml-auto shrink-0">
          <Button
            icon="copy"
            type="text"
            size="small"
            @click="handleCopy(index)"
          />
          <Button icon="edit" type="text" size="small" @click="handleEdit(index)" />
          <Button icon="delete" type="text" size="small" @click="handleDelete(index)" />
        </div>
      </div>
    </Card>
  </div>

  <Modal
    v-model:open="showEditModal"
    :on-ok="handleAddEnd"
    title="kernel.dns.tab.rules"
    max-width="90"
    max-height="90"
    height="90"
    :body-scrollable="false"
  >
    <RuleBuilder
      match-title="kernel.dns.rules.matchConditions"
      action-title="kernel.dns.rules.executeAction"
    >
      <template #action>
        <ActionPicker
          :model-value="fields.action"
          :items="actionItems"
          change-label="kernel.dns.rules.changeAction"
          @update:model-value="handleActionSelect"
        />

        <div v-if="hasActionOptions" class="action-editor-grid">
          <RouteActionEditor
            v-if="fields.action === RuleAction.Route"
            v-model="fields.action_options"
            :server-options="serversOptions"
          />
          <EvaluateActionEditor
            v-else-if="fields.action === RuleAction.Evaluate"
            v-model="fields.action_options"
            :server-options="serversOptions"
          />
          <RouteOptionsActionEditor
            v-else-if="fields.action === RuleAction.RouteOptions"
            v-model="fields.action_options"
          />
          <RejectActionEditor
            v-else-if="fields.action === RuleAction.Reject"
            v-model="fields.action_options"
          />
          <PredefinedActionEditor
            v-else-if="fields.action === RuleAction.Predefined"
            v-model="fields.action_options"
          />
        </div>
      </template>

      <template #match>
        <div class="form-item">
          {{ t('kernel.rules.type.clash_mode') }}
          <OptionGroup v-model="fields.clash_mode" :options="ModeOptions" clearable />
        </div>
        <div class="form-item">
          {{ t('kernel.rules.type.inbound') }}
          <Select v-model="fields.inbound" :options="inboundOptions" multiple clearable />
        </div>
        <div class="form-item">
          {{ t('kernel.dns.rules.fields.query_type') }}
          <Select
            v-model="fields.query_type"
            :options="DnsQueryTypeOptions"
            multiple
            clearable
            searchable
            allow-create
          />
        </div>
        <div class="form-item">
          {{ t('kernel.route.rules.fields.ip_version') }}
          <OptionGroup
            v-model="fields.ip_version"
            :options="RouteRuleIPVersionOptions"
            :empty-value="0"
            clearable
          />
        </div>
        <div class="form-item">
          {{ t('kernel.rules.type.network') }}
          <OptionGroup v-model="fields.network" :options="DnsRuleNetworkOptions" multiple />
        </div>
        <div class="form-item">
          {{ t('kernel.rules.type.protocol') }}
          <Select
            v-model="fields.protocol"
            :options="RouteRuleProtocolOptions"
            multiple
            clearable
          />
        </div>
        <div class="form-item">
          {{ t('kernel.dns.rules.fields.preferred_by') }}
          <Select
            v-model="fields.preferred_by"
            :options="DnsRulePreferredByOptions"
            multiple
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
          v-if="activeGroups.has('rule_set')"
          :title="t('kernel.dns.rules.groups.rule_set')"
          class="mb-8"
        >
          <template #extra
            ><Button
              icon="close"
              :icon-size="16"
              type="text"
              size="small"
              @click="removeGroup('rule_set')"
          /></template>
          <RuleSetMatchGroup v-model="fields" :rule-sets="ruleSet" />
        </Card>
        <Card
          v-if="activeGroups.has('domain')"
          :title="t('kernel.dns.rules.groups.domain')"
          class="mb-8"
        >
          <template #extra
            ><Button
              icon="close"
              :icon-size="16"
              type="text"
              size="small"
              @click="removeGroup('domain')"
          /></template>
          <DomainMatchGroup v-model="fields" />
        </Card>
        <Card
          v-if="activeGroups.has('source')"
          :title="t('kernel.dns.rules.groups.source')"
          class="mb-8"
        >
          <template #extra
            ><Button
              icon="close"
              :icon-size="16"
              type="text"
              size="small"
              @click="removeGroup('source')"
          /></template>
          <SourceMatchGroup v-model="fields" />
        </Card>
        <Card
          v-if="activeGroups.has('response')"
          :title="t('kernel.dns.rules.groups.response')"
          class="mb-8"
        >
          <template #extra
            ><Button
              icon="close"
              :icon-size="16"
              type="text"
              size="small"
              @click="removeGroup('response')"
          /></template>
          <ResponseMatchGroup v-model="fields" />
        </Card>
        <Card
          v-if="activeGroups.has('process')"
          :title="t('kernel.dns.rules.groups.process')"
          class="mb-8"
        >
          <template #extra
            ><Button
              icon="close"
              :icon-size="16"
              type="text"
              size="small"
              @click="removeGroup('process')"
          /></template>
          <ProcessMatchGroup v-model="fields" />
        </Card>
        <Card
          v-if="activeGroups.has('inline')"
          :title="t('kernel.dns.rules.groups.inline')"
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
          <template #extra
            ><Button
              icon="close"
              :icon-size="16"
              type="text"
              size="small"
              @click="removeGroup('inline')"
          /></template>
          <InlineMatchGroup v-model="fields" />
        </Card>
      </template>
    </RuleBuilder>
  </Modal>
</template>

<style lang="less" scoped>
.warn {
  color: rgb(200, 193, 11);
}

.rule-content {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
</style>
