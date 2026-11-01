<script setup lang="ts">
import { computed, ref } from 'vue'
import {
  ModeOptions,
  RouteRuleIPVersionOptions,
  RouteRuleNetworkOptions,
  RouteRulePreferredByOptions,
  RouteRuleProtocolOptions,
} from '@/constant/kernel'
import { routeMatchLabel, type RouteMatchField } from './matchFields'
import InlineMatchGroup from './InlineMatchGroup.vue'

const props = defineProps<{
  field: Exclude<RouteMatchField, 'rule_set'>
  inboundOptions: { label: string; value: string; disabled?: boolean }[]
}>()
const model = defineModel<IRule>({ required: true })
const inlineEditor = ref<InstanceType<typeof InlineMatchGroup>>()
const list = computed({
  get: () => model.value[props.field] as string[] | number[],
  set: (value: (string | number)[]) => Object.assign(model.value, { [props.field]: value }),
})
const boolean = computed({
  get: () => model.value[props.field] as boolean,
  set: (value: boolean) => Object.assign(model.value, { [props.field]: value }),
})
const choices = computed(() => {
  if (props.field === 'inbound') return props.inboundOptions
  if (props.field === 'network') return RouteRuleNetworkOptions
  if (props.field === 'protocol') return RouteRuleProtocolOptions
  if (props.field === 'preferred_by') return RouteRulePreferredByOptions
  return null
})
const selected = computed({
  get: () => list.value as string[],
  set: (value: string | string[]) => {
    list.value = Array.isArray(value) ? value : [value]
  },
})
const numeric = computed(() => props.field === 'port' || props.field === 'source_port')
defineExpose({ getContent: () => inlineEditor.value?.getContent() ?? model.value.raw })
</script>

<template>
  <InlineMatchGroup v-if="field === 'raw'" ref="inlineEditor" v-model="model" />
  <div v-else class="form-item">
    <span>{{ $t(routeMatchLabel(field)) }}</span>
    <Radio v-if="field === 'clash_mode'" v-model="model.clash_mode" :options="ModeOptions" />
    <Radio
      v-else-if="field === 'ip_version'"
      v-model="model.ip_version"
      :options="[{ label: 'kernel.strategy.default', value: 0 }, ...RouteRuleIPVersionOptions]"
    />
    <Switch
      v-else-if="field === 'ip_is_private' || field === 'source_ip_is_private'"
      v-model="boolean"
    />
    <Select v-else-if="choices" v-model="selected" :options="choices" multiple clearable />
    <InputList
      v-else
      v-model="list"
      :type="numeric ? 'number' : 'text'"
      :min="numeric ? 0 : undefined"
      :max="numeric ? 65535 : undefined"
    />
  </div>
</template>
