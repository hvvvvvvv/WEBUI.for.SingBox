<script setup lang="ts">
import RuleSetCardPicker from '../RuleSetCardPicker.vue'

defineProps<{ ruleSets: IRuleSet[] }>()
const model = defineModel<IDNSRule>({ required: true })
</script>

<template>
  <RuleSetCardPicker v-model="model.rule_set" :rule-sets="ruleSets" />
  <div class="form-item mt-8">
    {{ $t('kernel.dns.rules.fields.rule_set_ip_cidr_match_source') }}
    <Switch v-model="model.rule_set_ip_cidr_match_source" />
  </div>
</template>
