<script setup lang="ts">
import { type IconName } from '@/components/Icon/icons'

interface Props {
  type?: 'primary' | 'normal' | 'link' | 'text'
  size?: 'default' | 'small' | 'large'
  iconSize?: number
  iconColor?: string
  icon?: IconName
  loading?: boolean
  disabled?: boolean
  role?: string
}

const props = withDefaults(defineProps<Props>(), {
  type: 'normal',
  size: 'default',
  iconSize: undefined,
  iconColor: undefined,
  icon: undefined,
  loading: false,
  disabled: false,
  role: 'button',
})

const handleKeydown = (event: KeyboardEvent) => {
  if (event.target !== event.currentTarget || event.isComposing) return
  if (event.key !== 'Enter' && event.key !== ' ') return
  event.preventDefault()
  if (event.key === 'Enter' && !event.repeat && !props.disabled && !props.loading) {
    ;(event.currentTarget as HTMLElement).click()
  }
}

const handleKeyup = (event: KeyboardEvent) => {
  if (event.target !== event.currentTarget || event.key !== ' ' || event.isComposing) return
  event.preventDefault()
  if (!props.disabled && !props.loading) (event.currentTarget as HTMLElement).click()
}

const guardDisabledClick = (event: MouseEvent) => {
  if (props.disabled || props.loading) {
    event.preventDefault()
    event.stopImmediatePropagation()
  }
}
</script>

<template>
  <div
    :role="role"
    :tabindex="disabled || loading ? -1 : 0"
    :aria-disabled="disabled || loading || undefined"
    :class="[type, size, { 'pointer-events-none': disabled || loading }]"
    class="gui-button inline-flex items-center justify-center text-center align-middle rounded-6 text-14 text-nowrap cursor-pointer px-12 py-6 duration-200"
    @keydown="handleKeydown"
    @keyup="handleKeyup"
    @click.capture="guardDisabledClick"
  >
    <Icon
      v-if="loading"
      :color="`var(--btn-${type}-color)`"
      :size="size === 'small' ? 14 : 16"
      icon="loading"
      class="rotation"
    />
    <template v-else>
      <Icon
        v-if="disabled"
        :color="`var(--btn-${type}-color)`"
        icon="forbidden"
        class="pointer-events-none shrink-0 mr-4"
      />
      <Icon
        v-if="icon"
        :icon="icon"
        :size="iconSize"
        :color="iconColor || `var(--btn-${type}-color)`"
        :class="$slots.default ? 'mr-4' : ''"
      />
    </template>
    <slot></slot>
  </div>
</template>

<style lang="less" scoped>
.gui-button:focus-visible {
  outline: 2px solid var(--primary-color);
  outline-offset: 2px;
}

.normal {
  color: var(--btn-normal-color);
  background-color: var(--btn-normal-bg);
  &:hover {
    color: var(--btn-normal-hover-color);
    border-color: var(--btn-normal-hover-border-color);
  }
  &:active {
    color: var(--btn-normal-active-color);
    border-color: var(--btn-normal-active-border-color);
  }
}

.primary {
  color: var(--btn-primary-color);
  background-color: var(--btn-primary-bg);
  border: none;
  &:hover {
    background-color: var(--btn-primary-hover-bg);
  }
  &:active {
    background-color: var(--btn-primary-active-bg);
  }
}

.link {
  color: var(--btn-link-color);
  background-color: var(--btn-link-bg);
  border: none;
  &:hover {
    color: var(--btn-link-hover-color);
  }
  &:active {
    color: var(--btn-link-active-color);
  }
}

.text {
  color: var(--btn-text-color);
  background-color: var(--btn-text-bg);
  border: none;
  &:hover {
    color: var(--btn-text-hover-color);
    background-color: var(--btn-text-hover-bg);
  }
  &:active {
    color: var(--btn-text-active-color);
    background-color: var(--btn-text-active-bg);
  }
}

.small {
  padding: 4px 8px;
  font-size: 12px;
}
.large {
  padding: 8px 12px;
  font-size: 16px;
}
</style>
