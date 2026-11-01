<script setup lang="ts">
import { useI18n } from 'vue-i18n'

interface Props {
  options?: { label: string; value: string }[]
  size?: 'default' | 'small'
}

withDefaults(defineProps<Props>(), {
  options: () => [],
  size: 'default',
})
const model = defineModel<string[]>({ default: () => [] })
const emit = defineEmits<{ change: [value: string[]] }>()
const { t } = useI18n()

const handleSelect = (value: string) => {
  const selected = model.value.includes(value)
    ? model.value.filter((item) => item !== value)
    : [...model.value, value]
  model.value = selected
  emit('change', selected)
}
</script>

<template>
  <div
    :class="[size]"
    class="gui-checkbox inline-flex rounded-8 overflow-hidden text-12"
    role="group"
  >
    <button
      v-for="option in options"
      :key="option.value"
      type="button"
      role="checkbox"
      :aria-checked="model.includes(option.value)"
      :class="{ active: model.includes(option.value) }"
      class="gui-checkbox-button cursor-pointer px-12 py-6 transition duration-200"
      @click="handleSelect(option.value)"
    >
      {{ t(option.label) }}
    </button>
  </div>
</template>

<style lang="less" scoped>
.gui-checkbox {
  max-width: 100%;
  flex-wrap: wrap;
  border: 1px solid var(--primary-color);
  &-button {
    appearance: none;
    font: inherit;
    flex: 1 0 auto;
    border: 0;
    color: var(--radio-normal-color);
    background-color: var(--radio-normal-bg);
    border-left: 1px solid var(--primary-color);
    &:first-child {
      border-left: none;
    }
    &:hover {
      color: var(--radio-normal-hover-color);
    }
    &:focus-visible {
      outline: 2px solid var(--primary-color);
      outline-offset: -2px;
    }
  }
  .active {
    color: var(--radio-primary-color);
    background-color: var(--radio-primary-bg);
    &:hover {
      background-color: var(--radio-primary-hover-bg);
    }
    &:active {
      background-color: var(--radio-primary-active-bg);
    }
  }
}
.small .gui-checkbox-button {
  font-size: 10px;
  padding: 4px 8px;
}
</style>
