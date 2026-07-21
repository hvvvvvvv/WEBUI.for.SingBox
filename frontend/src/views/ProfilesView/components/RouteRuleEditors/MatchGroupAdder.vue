<script setup lang="ts">
type MatchGroupKey = 'source' | 'destination' | 'process' | 'rule_set' | 'inline'

interface MatchGroupOption {
  key: MatchGroupKey
  label: string
}

defineProps<{ options: MatchGroupOption[] }>()

const emit = defineEmits<{
  add: [key: MatchGroupKey]
}>()
</script>

<template>
  <div class="match-group-grid">
    <button
      v-for="option in options"
      :key="option.key"
      type="button"
      class="match-group-option rounded-8 flex items-center justify-center gap-6 px-12 py-10 text-14 cursor-pointer"
      @click="emit('add', option.key)"
    >
      <Icon icon="add" :size="16" color="var(--primary-color)" />
      <span class="line-clamp-1">{{ option.label }}</span>
    </button>
  </div>
</template>

<style lang="less" scoped>
.match-group-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(148px, 1fr));
  gap: 8px;
  margin-bottom: 8px;
}

.match-group-option {
  min-height: 44px;
  color: var(--card-color);
  background: color-mix(in srgb, var(--card-bg) 72%, transparent);
  border: 1px dashed color-mix(in srgb, var(--primary-color) 38%, var(--card-color));
  font: inherit;
  transition:
    transform 0.2s ease,
    box-shadow 0.2s ease,
    border-color 0.2s ease,
    background-color 0.2s ease;

  &:hover {
    transform: translateY(-2px);
    border-color: var(--primary-color);
    background: var(--card-hover-bg);
    box-shadow: 0 6px 12px rgba(0, 0, 0, 0.08);
  }

  &:active {
    transform: translateY(0);
    background: var(--card-active-bg);
  }
}

@media (max-width: 480px) {
  .match-group-grid {
    grid-template-columns: 1fr;
  }
}
</style>
