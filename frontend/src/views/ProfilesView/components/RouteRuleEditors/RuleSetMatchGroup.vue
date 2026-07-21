<script setup lang="ts">
import RuleSetCardPicker from '../RuleSetCardPicker.vue'

defineProps<{ ruleSets: IRuleSet[] }>()
const model = defineModel<IRule>({ required: true })
</script>

<template>
  <RuleSetCardPicker v-model="model.rule_set" :rule-sets="ruleSets" />
</template>
