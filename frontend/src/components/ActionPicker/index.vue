<script setup lang="ts">
import { computed } from 'vue'

import type { IconName } from '@/components/Icon/icons'

export interface ActionPickerItem {
  value: string
  label: string
  description: string
  icon: IconName
}

const props = defineProps<{
  items: ActionPickerItem[]
  changeLabel: string
}>()
const model = defineModel<string>({ required: true })
const emit = defineEmits<{
  change: [value: string, oldValue: string]
}>()

const currentAction = computed(
  () => props.items.find((item) => item.value === model.value) || props.items[0],
)

const handleSelect = (value: string) => {
  if (value === model.value) return
  const oldValue = model.value
  model.value = value
  emit('change', value, oldValue)
}
</script>

<template>
  <div v-if="currentAction" class="action-picker-hero rounded-8">
    <div class="action-picker-main">
      <div class="action-picker-icon rounded-8 flex items-center justify-center">
        <Icon :icon="currentAction.icon" :size="24" color="var(--primary-color)" />
      </div>

      <div class="min-w-0">
        <div class="line-clamp-1 font-bold text-18">{{ $t(currentAction.label) }}</div>
        <div class="action-picker-description text-12 mt-2">
          {{ $t(currentAction.description) }}
        </div>
      </div>

      <div class="action-picker-controls flex items-center gap-2">
        <Dropdown :trigger="['click']" coordinate="viewport">
          <Button type="text" size="small">
            {{ $t(changeLabel) }}
            <Icon icon="arrowDown" :size="14" class="ml-4" />
          </Button>
          <template #overlay="{ close }">
            <div class="action-picker-grid">
              <button
                v-for="item in items"
                :key="item.value"
                type="button"
                :aria-pressed="item.value === model"
                :class="{ selected: item.value === model }"
                class="action-picker-option rounded-8 flex items-start gap-8 p-8 cursor-pointer"
                @click="
                  () => {
                    handleSelect(item.value)
                    close()
                  }
                "
              >
                <div class="action-picker-option-icon rounded-6 flex items-center justify-center">
                  <Icon :icon="item.icon" :size="20" color="var(--primary-color)" />
                </div>
                <div class="min-w-0 text-left">
                  <div class="font-bold text-14 line-clamp-1">{{ $t(item.label) }}</div>
                  <div class="action-picker-description text-12 mt-2">
                    {{ $t(item.description) }}
                  </div>
                </div>
                <Icon
                  v-if="item.value === model"
                  icon="selected"
                  :size="16"
                  color="var(--primary-color)"
                  class="ml-auto shrink-0"
                />
              </button>
            </div>
          </template>
        </Dropdown>
      </div>
    </div>
  </div>
</template>

<style lang="less" scoped>
.action-picker-hero {
  padding: 12px;
  color: var(--card-color);
  background: color-mix(in srgb, var(--primary-color) 8%, var(--card-bg));
  border: 1px solid color-mix(in srgb, var(--primary-color) 24%, transparent);
}

.action-picker-main {
  display: grid;
  grid-template-columns: 44px minmax(0, 1fr) auto;
  align-items: start;
  gap: 10px;
}

.action-picker-icon {
  width: 44px;
  height: 44px;
  background: color-mix(in srgb, var(--primary-color) 14%, transparent);
}

.action-picker-description {
  color: var(--card-color);
  opacity: 0.72;
}

.action-picker-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 6px;
  width: min(520px, calc(100vw - 32px));
  padding: 8px;
}

.action-picker-option {
  min-width: 0;
  color: var(--card-color);
  background: transparent;
  border: 1px solid transparent;
  font: inherit;
  transition:
    border-color 0.2s ease,
    background-color 0.2s ease,
    transform 0.2s ease;

  &:hover {
    transform: translateY(-1px);
    background: var(--card-hover-bg);
    border-color: color-mix(in srgb, var(--primary-color) 38%, transparent);
  }

  &.selected {
    background: color-mix(in srgb, var(--primary-color) 10%, transparent);
    border-color: var(--primary-color);
  }
}

.action-picker-option-icon {
  width: 34px;
  height: 34px;
  flex: 0 0 34px;
  background: color-mix(in srgb, var(--primary-color) 10%, transparent);
}

@container (max-width: 360px) {
  .action-picker-main {
    grid-template-columns: 44px minmax(0, 1fr);
  }

  .action-picker-controls {
    grid-column: 1 / -1;
    justify-content: flex-end;
  }
}

@media (max-width: 560px) {
  .action-picker-grid {
    grid-template-columns: 1fr;
    width: min(340px, calc(100vw - 24px));
  }
}
</style>
