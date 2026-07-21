<script setup lang="ts">
import { DnsRuleActionRejectOptions } from '@/constant/kernel'
import { RuleActionReject } from '@/enums/kernel'

const model = defineModel<IDNSActionOptions>({ required: true })

const handleMethodChange = (method: string | number | boolean | (string | number | boolean)[]) => {
  if (!Array.isArray(method) && method === RuleActionReject.Drop) model.value.no_drop = false
}
</script>

<template>
  <div class="form-item action-field action-field-wide">
    {{ $t('kernel.route.rules.action.rejectMethod') }}
    <OptionGroup
      v-model="model.method"
      :options="DnsRuleActionRejectOptions"
      :aria-label="$t('kernel.route.rules.action.rejectMethod')"
      @change="handleMethodChange"
    />
  </div>
  <div class="action-toggle-group">
    <ActionToggleField
      v-model="model.no_drop"
      :label="$t('kernel.route.rules.fields.no_drop')"
      :disabled="model.method === RuleActionReject.Drop"
    />
  </div>
</template>
