<script setup lang="ts">
import { Strategy } from '@/enums/kernel'

import ActionToggleField from './ActionToggleField.vue'

defineProps<{ serverOptions: { label: string; value: string }[] }>()
const model = defineModel<IActionOptions>({ required: true })

const strategyOptions = [
  { label: 'kernel.strategy.prefer_ipv4', value: Strategy.PreferIPv4 },
  { label: 'kernel.strategy.prefer_ipv6', value: Strategy.PreferIPv6 },
  { label: 'kernel.strategy.ipv4_only', value: Strategy.IPv4Only },
  { label: 'kernel.strategy.ipv6_only', value: Strategy.IPv6Only },
]

const addRewriteTTL = () => {
  model.value.rewrite_ttl = 0
}

const removeRewriteTTL = () => {
  delete model.value.rewrite_ttl
}
</script>

<template>
  <div class="form-item action-field">
    {{ $t('kernel.route.rules.server') }}
    <Select v-model="model.server" :options="serverOptions" clearable />
  </div>
  <div class="form-item action-field">
    {{ $t('kernel.strategy.name') }}
    <OptionGroup
      v-model="model.strategy"
      :options="strategyOptions"
      :aria-label="$t('kernel.strategy.name')"
      clearable
    />
  </div>
  <div class="action-toggle-group">
    <ActionToggleField
      v-model="model.disable_cache"
      :label="$t('kernel.route.rules.disable_cache')"
    />
    <ActionToggleField
      v-model="model.disable_optimistic_cache"
      :label="$t('kernel.route.rules.fields.disable_optimistic_cache')"
    />
  </div>
  <div class="form-item action-field action-field-wide">
    {{ $t('kernel.route.rules.fields.rewrite_ttl') }}
    <div
      v-if="model.rewrite_ttl !== undefined"
      class="action-input-row flex items-center gap-4"
    >
      <Input v-model="model.rewrite_ttl" type="number" :min="0" />
      <Button icon="close" type="text" @click="removeRewriteTTL" />
    </div>
    <Button v-else icon="add" type="text" @click="addRewriteTTL">
      {{ $t('common.add') }}
    </Button>
  </div>
  <div class="form-item action-field">
    {{ $t('kernel.route.rules.fields.timeout') }}
    <Input v-model="model.timeout" />
  </div>
  <div class="form-item action-field action-field-wide">
    {{ $t('kernel.route.rules.client_subnet') }}
    <Input v-model="model.client_subnet" />
  </div>
</template>
