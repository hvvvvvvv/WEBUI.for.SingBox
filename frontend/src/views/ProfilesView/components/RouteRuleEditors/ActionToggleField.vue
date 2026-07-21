<script setup lang="ts">
interface Props {
  label: string
  disabled?: boolean
}

const props = withDefaults(defineProps<Props>(), {
  disabled: false,
})

const model = defineModel<boolean>({ default: false })

const emit = defineEmits<{
  (event: 'change', value: boolean): void
}>()

const toggle = () => {
  if (props.disabled) return

  const value = !model.value
  model.value = value
  emit('change', value)
}
</script>

<template>
  <button
    type="button"
    role="switch"
    :aria-checked="model"
    :aria-disabled="disabled"
    :disabled="disabled"
    :class="{ 'is-active': model }"
    class="action-toggle-field rounded-8"
    @click="toggle"
  >
    <span class="action-toggle-label">{{ label }}</span>
    <span class="action-toggle-track" aria-hidden="true">
      <span class="action-toggle-thumb"></span>
    </span>
  </button>
</template>

<style lang="less" scoped>
.action-toggle-field {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  width: 100%;
  min-width: 0;
  min-height: 44px;
  padding: 8px 10px;
  color: inherit;
  font: inherit;
  text-align: left;
  appearance: none;
  cursor: pointer;
  background: color-mix(in srgb, var(--card-bg) 74%, transparent);
  border: 1px solid color-mix(in srgb, var(--card-color) 14%, transparent);
  transition:
    transform 0.2s ease,
    background-color 0.2s ease,
    border-color 0.2s ease,
    box-shadow 0.2s ease;

  &:hover:not(:disabled) {
    transform: translateY(-1px);
    background: color-mix(in srgb, var(--primary-color) 5%, var(--card-bg));
    border-color: color-mix(in srgb, var(--primary-color) 32%, transparent);
  }

  &:focus-visible {
    outline: 2px solid color-mix(in srgb, var(--primary-color) 68%, transparent);
    outline-offset: 2px;
  }

  &:disabled {
    cursor: not-allowed;
    opacity: 0.5;
  }

  &.is-active {
    background: color-mix(in srgb, var(--primary-color) 10%, var(--card-bg));
    border-color: color-mix(in srgb, var(--primary-color) 48%, transparent);
    box-shadow: 0 2px 8px color-mix(in srgb, var(--primary-color) 9%, transparent);
  }
}

.action-toggle-label {
  min-width: 0;
  line-height: 1.35;
  overflow-wrap: anywhere;
}

.action-toggle-track {
  position: relative;
  flex: 0 0 auto;
  width: 34px;
  height: 20px;
  border-radius: 999px;
  background: var(--switch-off-bg);
  transition: background-color 0.2s ease;
}

.action-toggle-thumb {
  position: absolute;
  top: 3px;
  left: 3px;
  width: 14px;
  height: 14px;
  border-radius: 50%;
  background: var(--switch-off-dot-bg);
  transition:
    transform 0.2s ease,
    background-color 0.2s ease;
}

.is-active {
  .action-toggle-track {
    background: var(--switch-on-bg);
  }

  .action-toggle-thumb {
    background: var(--switch-on-dot-bg);
    transform: translateX(14px);
  }
}
</style>
