<script setup lang="ts">
import QueryOptionsEditor from './QueryOptionsEditor.vue'

defineProps<{ serverOptions: { label: string; value: string }[] }>()
const model = defineModel<IDNSActionOptions>({ required: true })
</script>

<template>
  <div class="form-item action-field action-field-wide">
    {{ $t('kernel.route.rules.server') }}
    <Select v-model="model.server" :options="serverOptions" clearable />
  </div>
  <QueryOptionsEditor v-model="model" />
</template>
