<script setup lang="ts">
import { RuleSnifferOptions } from '@/constant/kernel'

const model = defineModel<IActionOptions>({ required: true })
</script>

<template>
  <div class="form-item action-field action-field-wide">
    {{ $t('kernel.route.rules.sniffer.name') }}
    <Select
      v-model="model.sniffer"
      multiple
      clearable
      :options="RuleSnifferOptions"
      placeholder="All"
    />
  </div>
  <div class="form-item action-field">
    {{ $t('kernel.route.rules.fields.timeout') }}
    <Input v-model="model.timeout" placeholder="300ms" />
  </div>
</template>
