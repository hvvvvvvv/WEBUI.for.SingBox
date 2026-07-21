<script setup lang="ts">
import ServerQueryActionEditor from './ServerQueryActionEditor.vue'

defineProps<{ serverOptions: { label: string; value: string }[] }>()
const model = defineModel<IDNSActionOptions>({ required: true })
</script>

<template>
  <ServerQueryActionEditor v-model="model" :server-options="serverOptions" show-server />
</template>
