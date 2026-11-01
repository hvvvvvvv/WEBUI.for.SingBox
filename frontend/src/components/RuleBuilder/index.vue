<script setup lang="ts">
defineProps<{
  matchTitle: string
  actionTitle: string
}>()
</script>

<template>
  <div class="shared-rule-builder">
    <section class="shared-rule-pane shared-rule-match rounded-8">
      <div class="shared-rule-heading flex items-center gap-8">
        <span class="font-bold text-16">{{ $t(matchTitle) }}</span>
      </div>
      <slot name="match"></slot>
    </section>

    <section class="shared-rule-pane shared-rule-action rounded-8">
      <div class="shared-rule-heading flex items-center gap-8">
        <span class="font-bold text-16">{{ $t(actionTitle) }}</span>
      </div>
      <slot name="action"></slot>
    </section>
  </div>
</template>

<style lang="less" scoped>
.shared-rule-builder {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  align-items: stretch;
  gap: 8px;
  min-height: 0;
}

.shared-rule-pane {
  min-width: 0;
  min-height: 0;
  padding: 12px;
  border: 1px solid color-mix(in srgb, var(--card-color) 12%, transparent);
  box-sizing: border-box;
}

.shared-rule-pane :deep(.form-item) {
  min-width: 0;
  gap: 8px 12px;
  overflow-wrap: anywhere;
}

.shared-rule-pane :deep(.form-item > :last-child) {
  flex-shrink: 0;
}

.shared-rule-heading {
  min-height: 28px;
  margin-bottom: 10px;
}

.shared-rule-match {
  background: color-mix(in srgb, var(--card-bg) 76%, transparent);
}

.shared-rule-action {
  --action-pane-bg: color-mix(in srgb, var(--primary-color) 5%, var(--card-bg));

  background: var(--action-pane-bg);
  border-color: color-mix(in srgb, var(--primary-color) 28%, transparent);
  border-top: 3px solid var(--primary-color);
  container-type: inline-size;
}

:deep(.action-editor-grid) {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  column-gap: 10px;
  row-gap: 12px;
  padding-top: 12px;
  margin-top: 12px;
  border-top: 1px solid color-mix(in srgb, var(--primary-color) 20%, transparent);
}

:deep(.action-editor-grid .action-field) {
  min-width: 0;
  padding: 0;
  flex-direction: column;
  align-items: stretch;
  justify-content: flex-start;
  gap: 6px;
}

:deep(.action-editor-grid .action-field-wide) {
  grid-column: 1 / -1;
}

:deep(.action-editor-grid .action-toggle-group) {
  display: grid;
  grid-column: 1 / -1;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 8px;
  min-width: 0;
}

:deep(.action-editor-grid .action-toggle-group > :only-child) {
  grid-column: 1 / -1;
}

:deep(.action-editor-grid .action-field > .gui-dropdown),
:deep(.action-editor-grid .action-field > .gui-input),
:deep(.action-editor-grid .action-field > .gui-input-list),
:deep(.action-editor-grid .action-field > .action-input-row),
:deep(.action-editor-grid .action-field .gui-select) {
  width: 100%;
  min-width: 0;
  max-width: 100%;
}

@container (max-width: 440px) {
  :deep(.action-editor-grid) {
    grid-template-columns: 1fr;
  }

  :deep(.action-editor-grid .action-field-wide) {
    grid-column: auto;
  }
}

@container (max-width: 360px) {
  :deep(.action-editor-grid .action-toggle-group) {
    grid-template-columns: 1fr;
  }
}

@media (max-width: 560px) {
  .shared-rule-match :deep(.form-item) {
    flex-direction: column;
    align-items: stretch;
  }

  .shared-rule-match :deep(.form-item > :last-child) {
    width: 100%;
    max-width: 100%;
    min-width: 0;
  }
}
</style>
