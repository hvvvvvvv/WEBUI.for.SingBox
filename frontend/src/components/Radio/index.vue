<script setup lang="ts">
import { useI18n } from 'vue-i18n'

interface Props {
  options?: { label: string; value: string | number | boolean }[]
  size?: 'default' | 'small'
  disabled?: boolean
}

const model = defineModel<string | number | boolean>()

const props = withDefaults(defineProps<Props>(), {
  options: () => [],
  size: 'default',
  disabled: false,
})

const emits = defineEmits(['change'])

const { t } = useI18n()

const handleSelect = (val: string | number | boolean) => {
  if (props.disabled) return
  const oldValue = model.value
  if (oldValue === val) {
    return
  }
  model.value = val
  emits('change', val, oldValue)
}

const handleKeydown = (event: KeyboardEvent, index: number) => {
  if (props.disabled || event.isComposing) return
  if (!['ArrowLeft', 'ArrowRight', 'ArrowUp', 'ArrowDown', 'Home', 'End'].includes(event.key))
    return
  event.preventDefault()
  const length = props.options.length
  const next =
    event.key === 'Home'
      ? 0
      : event.key === 'End'
        ? length - 1
        : (index + (['ArrowRight', 'ArrowDown'].includes(event.key) ? 1 : -1) + length) % length
  handleSelect(props.options[next]!.value)
  const buttons = (event.currentTarget as HTMLElement).parentElement?.querySelectorAll<HTMLElement>(
    '[role="radio"]',
  )
  buttons?.[next]?.focus()
}
</script>

<template>
  <div
    :class="[size]"
    class="gui-radio inline-flex rounded-full text-12 overflow-hidden"
    role="radiogroup"
    :aria-disabled="disabled || undefined"
  >
    <button
      v-for="(o, index) in options"
      :key="o.value.toString()"
      type="button"
      role="radio"
      :aria-checked="o.value === model"
      :disabled="disabled"
      :tabindex="
        o.value === model || (index === 0 && !options.some((option) => option.value === model))
          ? 0
          : -1
      "
      v-tips.slow="o.label"
      :class="{ active: o.value === model }"
      class="gui-radio-button cursor-pointer px-12 py-6 duration-200 line-clamp-1 break-all"
      @click="handleSelect(o.value)"
      @keydown="handleKeydown($event, index)"
    >
      {{ t(o.label) }}
    </button>
  </div>
</template>

<style lang="less" scoped>
.gui-radio {
  border: 1px solid var(--primary-color);
  &-button {
    appearance: none;
    font: inherit;
    border: 0;
    color: var(--radio-normal-color);
    background-color: var(--radio-normal-bg);
    border-left: 1px solid var(--primary-color);
    &:nth-child(1) {
      border-left: none;
    }
    &:hover {
      color: var(--radio-normal-hover-color);
    }
    &:focus-visible {
      outline: 2px solid var(--primary-color);
      outline-offset: -2px;
    }
    &:disabled {
      cursor: not-allowed;
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

.small {
  .gui-radio-button {
    font-size: 10px;
    padding: 4px 8px;
  }
}
</style>
