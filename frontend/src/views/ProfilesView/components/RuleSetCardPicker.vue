<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

import { RulesetFormat, RulesetType } from '@/enums/kernel'

const props = defineProps<{ ruleSets: IRuleSet[] }>()
const model = defineModel<string[]>({ required: true })

const { t } = useI18n()

const missingRuleSetIds = computed(() => {
  const availableIds = new Set(props.ruleSets.map((ruleSet) => ruleSet.id))
  return [...new Set(model.value.filter((id) => id && !availableIds.has(id)))]
})

const isSelected = (id: string) => model.value.includes(id)

const toggle = (id: string) => {
  model.value = isSelected(id)
    ? model.value.filter((selectedId) => selectedId !== id)
    : [...model.value, id]
}

const ruleSetFormat = (ruleSet: IRuleSet) =>
  ruleSet.type === RulesetType.Inline ? RulesetFormat.Source : ruleSet.format
</script>

<template>
  <div class="rule-set-card-picker">
    <div v-if="missingRuleSetIds.length" class="rule-set-card-grid mb-8">
      <Card
        v-for="id in missingRuleSetIds"
        :key="id"
        :title="id"
        selected
        role="checkbox"
        tabindex="0"
        aria-checked="true"
        :aria-label="`${id}, ${t('kernel.route.rule_set.notFound')}`"
        class="rule-set-card rule-set-card-missing text-12"
        @click="toggle(id)"
        @keydown.enter.prevent="toggle(id)"
        @keydown.space.prevent="toggle(id)"
      >
        {{ t('kernel.route.rule_set.notFound') }}
      </Card>
    </div>

    <Empty
      v-if="ruleSets.length === 0"
      :description="t('kernel.route.rule_set.empty')"
    />
    <div v-else class="rule-set-card-grid">
      <Card
        v-for="ruleSet in ruleSets"
        :key="ruleSet.id"
        v-tips="ruleSet.type"
        :title="ruleSet.tag"
        :selected="isSelected(ruleSet.id)"
        role="checkbox"
        tabindex="0"
        :aria-checked="isSelected(ruleSet.id)"
        :aria-label="`${ruleSet.tag}, ${ruleSet.type}, ${ruleSetFormat(ruleSet)}`"
        class="rule-set-card text-12"
        @click="toggle(ruleSet.id)"
        @keydown.enter.prevent="toggle(ruleSet.id)"
        @keydown.space.prevent="toggle(ruleSet.id)"
      >
        {{ ruleSet.type }} {{ ruleSetFormat(ruleSet) }}
      </Card>
    </div>
  </div>
</template>

<style lang="less" scoped>
.rule-set-card-picker {
  min-width: 0;
  container-type: inline-size;
}

.rule-set-card-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 8px;
}

.rule-set-card {
  min-width: 0;
  cursor: pointer;
  outline: none;

  &:focus-visible {
    outline: 2px solid color-mix(in srgb, var(--primary-color) 68%, transparent);
    outline-offset: 2px;
  }
}

.rule-set-card-missing {
  color: rgb(200, 193, 11);
  background: color-mix(in srgb, rgb(200, 193, 11) 8%, var(--card-bg));
  border: 1px solid color-mix(in srgb, rgb(200, 193, 11) 48%, transparent);
}

@container (max-width: 520px) {
  .rule-set-card-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

@container (max-width: 340px) {
  .rule-set-card-grid {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
