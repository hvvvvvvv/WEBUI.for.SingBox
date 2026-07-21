<script setup lang="ts">
import { computed } from 'vue'

import { type IconName } from '@/components/Icon/icons'
import { RuleAction } from '@/enums/kernel'

interface ActionItem {
  value: RuleAction
  label: string
  description: string
  icon: IconName
}

const model = defineModel<IRule['action']>({ required: true })
const emit = defineEmits<{
  change: [value: RuleAction, oldValue: IRule['action']]
}>()

const actionItems: ActionItem[] = [
  {
    value: RuleAction.Route,
    label: 'kernel.route.rules.action.route',
    description: 'kernel.route.rules.actionDescription.route',
    icon: 'forward',
  },
  {
    value: RuleAction.Bypass,
    label: 'kernel.route.rules.action.bypass',
    description: 'kernel.route.rules.actionDescription.bypass',
    icon: 'arrowRight',
  },
  {
    value: RuleAction.RouteOptions,
    label: 'kernel.route.rules.action.route-options',
    description: 'kernel.route.rules.actionDescription.route-options',
    icon: 'settings3',
  },
  {
    value: RuleAction.Reject,
    label: 'kernel.route.rules.action.reject',
    description: 'kernel.route.rules.actionDescription.reject',
    icon: 'forbidden',
  },
  {
    value: RuleAction.HijackDNS,
    label: 'kernel.route.rules.action.hijack-dns',
    description: 'kernel.route.rules.actionDescription.hijack-dns',
    icon: 'inbound',
  },
  {
    value: RuleAction.Sniff,
    label: 'kernel.route.rules.action.sniff',
    description: 'kernel.route.rules.actionDescription.sniff',
    icon: 'preview',
  },
  {
    value: RuleAction.Resolve,
    label: 'kernel.route.rules.action.resolve',
    description: 'kernel.route.rules.actionDescription.resolve',
    icon: 'link',
  },
  {
    value: RuleAction.Inline,
    label: 'kernel.route.rules.action.inline',
    description: 'kernel.route.rules.actionDescription.inline',
    icon: 'code',
  },
]

const currentAction = computed(
  () => actionItems.find((item) => item.value === model.value) || actionItems[0]!,
)

const handleSelect = (value: RuleAction) => {
  if (value === model.value) return
  const oldValue = model.value
  model.value = value as IRule['action']
  emit('change', value, oldValue)
}
</script>

<template>
  <div class="route-action-hero rounded-8">
    <div class="route-action-main">
      <div class="route-action-icon rounded-8 flex items-center justify-center">
        <Icon :icon="currentAction.icon" :size="24" color="var(--primary-color)" />
      </div>

      <div class="min-w-0">
        <div class="route-action-title line-clamp-1 font-bold text-18">
          {{ $t(currentAction.label) }}
        </div>
        <div class="route-action-description text-12 mt-2">
          {{ $t(currentAction.description) }}
        </div>
      </div>

      <div class="route-action-controls flex items-center gap-2">
        <Dropdown :trigger="['click']" coordinate="viewport">
          <Button type="text" size="small">
            {{ $t('kernel.route.rules.changeAction') }}
            <Icon icon="arrowDown" :size="14" class="ml-4" />
          </Button>
          <template #overlay="{ close }">
            <div class="route-action-grid">
              <button
                v-for="item in actionItems"
                :key="item.value"
                type="button"
                :class="{ selected: item.value === model }"
                class="route-action-option rounded-8 flex items-start gap-8 p-8 cursor-pointer"
                @click="
                  () => {
                    handleSelect(item.value)
                    close()
                  }
                "
              >
                <div class="route-action-option-icon rounded-6 flex items-center justify-center">
                  <Icon :icon="item.icon" :size="20" color="var(--primary-color)" />
                </div>
                <div class="min-w-0 text-left">
                  <div class="font-bold text-14 line-clamp-1">{{ $t(item.label) }}</div>
                  <div class="route-action-option-description text-12 mt-2">
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
.route-action-hero {
  padding: 12px;
  color: var(--card-color);
  background: color-mix(in srgb, var(--primary-color) 8%, var(--card-bg));
  border: 1px solid color-mix(in srgb, var(--primary-color) 24%, transparent);
}

.route-action-main {
  display: grid;
  grid-template-columns: 44px minmax(0, 1fr) auto;
  align-items: start;
  gap: 10px;
}

.route-action-icon {
  width: 44px;
  height: 44px;
  background: color-mix(in srgb, var(--primary-color) 14%, transparent);
}

.route-action-description,
.route-action-option-description {
  color: var(--card-color);
  opacity: 0.72;
}

.route-action-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 6px;
  width: min(520px, calc(100vw - 32px));
  padding: 8px;
}

.route-action-option {
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

.route-action-option-icon {
  width: 34px;
  height: 34px;
  flex: 0 0 34px;
  background: color-mix(in srgb, var(--primary-color) 10%, transparent);
}

@container (max-width: 360px) {
  .route-action-main {
    grid-template-columns: 44px minmax(0, 1fr);
  }

  .route-action-controls {
    grid-column: 1 / -1;
    justify-content: flex-end;
  }
}

@media (max-width: 560px) {
  .route-action-grid {
    grid-template-columns: 1fr;
    width: min(340px, calc(100vw - 24px));
  }
}
</style>
