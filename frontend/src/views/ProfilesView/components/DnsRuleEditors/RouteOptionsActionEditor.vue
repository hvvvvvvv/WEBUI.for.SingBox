<script setup lang="ts">
import QueryOptionsEditor from './QueryOptionsEditor.vue'

const model = defineModel<IDNSActionOptions>({ required: true })
</script>

<template>
  <QueryOptionsEditor v-model="model" />
</template>
