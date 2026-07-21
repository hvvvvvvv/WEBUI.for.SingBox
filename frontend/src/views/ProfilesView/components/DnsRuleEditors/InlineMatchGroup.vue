<script setup lang="ts">
const model = defineModel<IDNSRule>({ required: true })
</script>

<template>
  <div class="form-item inline-json-field">
    <div>{{ $t('kernel.route.rules.fields.raw') }}</div>
    <CodeViewer v-model="model.raw" editable lang="json" class="inline-json-editor" />
  </div>
</template>

<style lang="less" scoped>
.inline-json-field {
  min-width: 0;
  flex-direction: column;
  align-items: stretch;
  justify-content: flex-start;
  gap: 8px;
}

.inline-json-editor {
  width: 100%;
  min-width: 0;
  max-width: 100%;
  min-height: 160px;
  overflow: hidden;
  border-radius: 4px;

  :deep(.cm-editor) {
    min-width: 0;
    min-height: 160px;
  }
}
</style>
