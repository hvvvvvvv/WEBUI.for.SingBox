<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

type OptionValue = string | number | boolean

interface Option {
  label: string
  value: OptionValue
  disabled?: boolean
}

interface Props {
  options?: Option[]
  multiple?: boolean
  clearable?: boolean
  emptyValue?: OptionValue
  disabled?: boolean
  size?: 'default' | 'small'
  ariaLabel?: string
}

const props = withDefaults(defineProps<Props>(), {
  options: () => [],
  multiple: false,
  clearable: false,
  emptyValue: '',
  disabled: false,
  size: 'default',
  ariaLabel: '',
})

const model = defineModel<OptionValue | OptionValue[]>()

const emit = defineEmits<{
  change: [value: OptionValue | OptionValue[], oldValue: OptionValue | OptionValue[] | undefined]
}>()

const { t } = useI18n()

const selectedValues = computed<OptionValue[]>(() =>
  Array.isArray(model.value) ? model.value : [],
)

const hasValue = computed(
  () =>
    !props.multiple &&
    model.value !== undefined &&
    !Array.isArray(model.value) &&
    !Object.is(model.value, props.emptyValue),
)

const isSelected = (value: OptionValue) =>
  props.multiple ? selectedValues.value.includes(value) : Object.is(model.value, value)

const updateValue = (value: OptionValue | OptionValue[]) => {
  const oldValue = Array.isArray(model.value) ? [...model.value] : model.value
  model.value = value
  emit('change', value, oldValue)
}

const handleSelect = (option: Option) => {
  if (props.disabled || option.disabled) return

  if (!props.multiple) {
    if (!isSelected(option.value)) updateValue(option.value)
    return
  }

  updateValue(
    isSelected(option.value)
      ? selectedValues.value.filter((value) => !Object.is(value, option.value))
      : [...selectedValues.value, option.value],
  )
}

const handleClear = () => {
  if (!props.disabled && hasValue.value) updateValue(props.emptyValue)
}
</script>

<template>
  <div
    :class="[size, multiple ? 'multiple' : 'single', { disabled }]"
    class="option-group"
  >
    <div
      :role="multiple ? 'group' : 'radiogroup'"
      :aria-label="ariaLabel || undefined"
      class="option-group-list"
    >
      <button
        v-for="option in options"
        :key="`${typeof option.value}:${String(option.value)}`"
        v-tips.slow="option.label"
        type="button"
        :role="multiple ? 'checkbox' : 'radio'"
        :aria-checked="isSelected(option.value)"
        :disabled="disabled || option.disabled"
        :class="{ selected: isSelected(option.value) }"
        class="option-group-item"
        @click="handleSelect(option)"
      >
        <Icon
          v-if="multiple && isSelected(option.value)"
          icon="selected"
          :size="size === 'small' ? 12 : 14"
          color="currentColor"
          class="shrink-0"
        />
        <span class="option-group-label">{{ t(option.label) }}</span>
      </button>
    </div>

    <button
      v-if="!multiple && clearable && hasValue"
      v-tips.slow="'common.clear'"
      type="button"
      :disabled="disabled"
      :aria-label="t('common.clear')"
      class="option-group-clear"
      @click="handleClear"
    >
      <Icon icon="close" :size="size === 'small' ? 12 : 14" />
    </button>
  </div>
</template>

<style lang="less" scoped>
.option-group {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  min-width: 0;
  max-width: 100%;
}

.option-group-list {
  display: flex;
  flex: 1 1 auto;
  min-width: 0;
  max-width: 100%;
}

.option-group-item,
.option-group-clear {
  color: var(--radio-normal-color);
  font: inherit;
  appearance: none;
  cursor: pointer;
  transition:
    color 0.2s ease,
    background-color 0.2s ease,
    border-color 0.2s ease,
    transform 0.2s ease;

  &:focus-visible {
    position: relative;
    z-index: 1;
    outline: 2px solid color-mix(in srgb, var(--primary-color) 68%, transparent);
    outline-offset: 2px;
  }

  &:disabled {
    cursor: not-allowed;
    opacity: 0.5;
  }
}

.option-group-item {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 4px;
  min-width: 0;
  padding: 6px 12px;
  background: var(--radio-normal-bg);
}

.option-group-label {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.single {
  .option-group-list {
    overflow: hidden;
    border: 1px solid var(--primary-color);
    border-radius: 999px;
  }

  .option-group-item {
    flex: 1 1 auto;
    border: 0;
    border-left: 1px solid var(--primary-color);

    &:first-child {
      border-left: 0;
    }

    &:focus-visible {
      outline-offset: -2px;
    }

    &:hover:not(:disabled) {
      color: var(--radio-normal-hover-color);
    }

    &.selected {
      color: var(--radio-primary-color);
      background: var(--radio-primary-bg);

      &:hover:not(:disabled) {
        background: var(--radio-primary-hover-bg);
      }
    }
  }
}

.multiple {
  .option-group-list {
    flex-wrap: wrap;
    gap: 6px;
  }

  .option-group-item {
    border: 1px solid color-mix(in srgb, var(--card-color) 18%, transparent);
    border-radius: 999px;

    &:hover:not(:disabled) {
      color: var(--primary-color);
      border-color: color-mix(in srgb, var(--primary-color) 42%, transparent);
      transform: translateY(-1px);
    }

    &.selected {
      color: var(--radio-primary-color);
      background: var(--radio-primary-bg);
      border-color: var(--primary-color);
    }
  }
}

.option-group-clear {
  display: inline-flex;
  flex: 0 0 auto;
  align-items: center;
  justify-content: center;
  padding: 4px;
  background: transparent;
  border: 0;
  border-radius: 4px;

  &:hover:not(:disabled) {
    color: var(--primary-color);
    background: var(--btn-text-hover-bg);
  }
}

.small {
  .option-group-item {
    gap: 3px;
    padding: 4px 8px;
    font-size: 10px;
  }
}

.disabled {
  opacity: 0.72;
}
</style>
