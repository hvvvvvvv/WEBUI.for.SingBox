<script setup lang="ts">
defineProps<{
  matchTitle: string
  actionTitle: string
}>()
</script>

<template>
  <div class="shared-rule-builder">
    <section class="shared-rule-pane shared-rule-action rounded-8">
      <div class="shared-rule-heading flex items-center gap-8">
        <span class="shared-rule-keyword rounded-full">THEN</span>
        <span class="font-bold text-16">{{ $t(actionTitle) }}</span>
      </div>
      <slot name="action"></slot>
    </section>

    <div class="shared-rule-arrow flex items-center justify-center" aria-hidden="true">
      <Icon icon="arrowRight" :size="20" color="var(--primary-color)" />
    </div>

    <section class="shared-rule-pane shared-rule-match rounded-8">
      <div class="shared-rule-heading flex items-center gap-8">
        <span class="shared-rule-keyword rounded-full">IF</span>
        <span class="font-bold text-16">{{ $t(matchTitle) }}</span>
      </div>
      <slot name="match"></slot>
    </section>
  </div>
</template>

<style lang="less" scoped>
.shared-rule-builder {
  display: grid;
  grid-template-columns: minmax(0, 3fr) 32px minmax(360px, 2fr);
  grid-template-areas: 'match arrow action';
  align-items: stretch;
  gap: 8px;
  height: 100%;
  min-height: 0;
  overflow: hidden;
}

.shared-rule-pane {
  min-width: 0;
  min-height: 0;
  padding: 12px;
  overflow-y: auto;
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

.shared-rule-keyword {
  padding: 2px 8px;
  color: var(--primary-color);
  background: color-mix(in srgb, var(--primary-color) 11%, transparent);
  font-size: 10px;
  font-weight: 700;
  letter-spacing: 0.08em;
}

.shared-rule-match {
  grid-area: match;
  background: color-mix(in srgb, var(--card-bg) 76%, transparent);
}

.shared-rule-action {
  --action-pane-bg: color-mix(in srgb, var(--primary-color) 5%, var(--card-bg));

  grid-area: action;
  background: var(--action-pane-bg);
  border-color: color-mix(in srgb, var(--primary-color) 28%, transparent);
  border-top: 3px solid var(--primary-color);
  container-type: inline-size;
}

.shared-rule-arrow {
  grid-area: arrow;
  align-self: start;
  width: 28px;
  height: 28px;
  margin-top: 14px;
  border-radius: 50%;
  background: color-mix(in srgb, var(--primary-color) 10%, transparent);
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

@media (max-width: 900px) {
  .shared-rule-builder {
    display: flex;
    flex-direction: column;
    height: 100%;
    overflow-y: auto;
  }

  .shared-rule-action {
    order: 1;
  }

  .shared-rule-match {
    order: 2;
  }

  .shared-rule-pane {
    min-height: auto;
    overflow: visible;
    flex: 0 0 auto;
  }

  .shared-rule-arrow {
    display: none;
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
