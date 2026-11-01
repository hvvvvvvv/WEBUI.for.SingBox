<script setup lang="ts">
import { RuleActionRejectOptions } from '@/constant/kernel'
import { RuleActionReject } from '@/enums/kernel'

const model = defineModel<IActionOptions>({ required: true })

const handleMethodChange = (method: string | number | boolean | (string | number | boolean)[]) => {
  if (!Array.isArray(method) && method === RuleActionReject.Drop) model.value.no_drop = false
}
</script>

<template>
  <div class="form-item action-field action-field-wide">
    {{ $t('kernel.route.rules.action.rejectMethod') }}
    <Radio
      v-model="model.method"
      :options="RuleActionRejectOptions"
      :aria-label="$t('kernel.route.rules.action.rejectMethod')"
      @change="handleMethodChange"
    />
  </div>
  <div class="form-item">
    {{ $t('kernel.route.rules.fields.no_drop') }}
    <Switch v-model="model.no_drop" :disabled="model.method === RuleActionReject.Drop" />
  </div>
</template>
