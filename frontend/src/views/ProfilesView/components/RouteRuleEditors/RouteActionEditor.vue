<script setup lang="ts">
import RouteOptionsEditor from './RouteOptionsEditor.vue'

defineProps<{ outboundOptions: { label: string; value: string }[] }>()
const model = defineModel<IActionOptions>({ required: true })
</script>

<template>
  <div class="form-item action-field action-field-wide">
    {{ $t('kernel.route.rules.outbound') }}
    <Select v-model="model.outbound" :options="outboundOptions" clearable />
  </div>
  <RouteOptionsEditor v-model="model" />
</template>
