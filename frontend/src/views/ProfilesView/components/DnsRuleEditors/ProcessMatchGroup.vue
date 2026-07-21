<script setup lang="ts">
const model = defineModel<IDNSRule>({ required: true })
</script>

<template>
  <div class="form-item">
    {{ $t('kernel.route.rules.fields.process_name') }}
    <InputList v-model="model.process_name" />
  </div>
  <div class="form-item">
    {{ $t('kernel.route.rules.fields.process_path') }}
    <InputList v-model="model.process_path" />
  </div>
  <div class="form-item">
    {{ $t('kernel.route.rules.fields.process_path_regex') }}
    <InputList v-model="model.process_path_regex" />
  </div>
</template>
