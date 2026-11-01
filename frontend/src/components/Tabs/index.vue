<script setup lang="ts">
import { computed, useSlots, type Component } from 'vue'
import { useI18n } from 'vue-i18n'

type TabItemType = {
  key: string
  tab: string
  component?: Component
}

interface Props {
  activeKey: string
  items: readonly TabItemType[]
  tabPosition?: 'left' | 'top'
  tabWidth?: string
  contentWidth?: string
}

const props = withDefaults(defineProps<Props>(), {
  tabPosition: 'left',
  tabWidth: '20%',
  contentWidth: '80%',
})

const emits = defineEmits(['update:activeKey'])

const { t } = useI18n()
const slots = useSlots()

const isTop = computed(() => props.tabPosition === 'top')

const handleChange = (key: string) => emits('update:activeKey', key)

const isActive = ({ key }: TabItemType) => key === props.activeKey

// NOTE:
// - component tabs are cached via KeepAlive
// - slot tabs are rendered as functional components and NOT cached
const currentComponent = computed(() => {
  const comp = props.items.find((i) => i.key === props.activeKey)?.component
  return comp ?? slots[props.activeKey]
})
</script>

<template>
  <div :class="{ 'flex-col': isTop }" class="gui-tabs flex">
    <div
      :class="{ 'justify-center mb-8': isTop, 'flex-col': !isTop }"
      :style="{ width: isTop ? 'auto' : tabWidth }"
      class="gui-tabs-tab flex"
    >
      <Button
        v-for="tab in items"
        :key="tab.key"
        :type="isActive(tab) ? 'link' : 'text'"
        @click="handleChange(tab.key)"
      >
        {{ t(tab.tab) }}
      </Button>
      <slot name="extra"></slot>
    </div>

    <div
      class="gui-tabs-content flex flex-col overflow-y-auto"
      :style="{ width: isTop ? 'auto' : contentWidth }"
    >
      <KeepAlive>
        <component :is="currentComponent" />
      </KeepAlive>
    </div>
  </div>
</template>

<style scoped>
@media (max-width: 700px) {
  .gui-tabs {
    min-width: 0;
    min-height: 0;
    flex-direction: column;
  }
  .gui-tabs-tab {
    width: 100% !important;
    flex-direction: row;
    flex-wrap: nowrap;
    flex-shrink: 0;
    justify-content: flex-start;
    gap: 4px;
    margin-bottom: 8px;
    overflow-x: auto;
    overscroll-behavior-x: contain;
    scrollbar-width: thin;
    scrollbar-color: var(--scrollbar-thumb-bg) var(--scrollbar-track-bg);
  }
  .gui-tabs-tab :deep(.gui-button) {
    flex: 0 0 auto;
    min-height: 44px;
    box-sizing: border-box;
  }
  .gui-tabs-content {
    width: 100% !important;
    min-width: 0;
    min-height: 0;
    flex: 1;
  }
}
</style>
