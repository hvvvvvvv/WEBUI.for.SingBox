import { computed, type Ref } from 'vue'

import { RuleAction, RuleType } from '@/enums/kernel'

interface InlineRuleFields {
  type: string
  action: string
  payload: string
}

interface ActionOption {
  label: string
  value: string
}

export const useInlineRuleActionOptions = <T extends InlineRuleFields>(
  fields: Ref<T>,
  options: ActionOption[],
) => {
  const actionOptions = computed(() =>
    options.filter(
      (option) => fields.value.type === RuleType.Inline || option.value !== RuleAction.Inline,
    ),
  )

  const handleRuleTypeChange = () => {
    fields.value.payload = ''
    if (fields.value.type !== RuleType.Inline && fields.value.action === RuleAction.Inline) {
      fields.value.action = RuleAction.Route
    }
  }

  return { actionOptions, handleRuleTypeChange }
}
