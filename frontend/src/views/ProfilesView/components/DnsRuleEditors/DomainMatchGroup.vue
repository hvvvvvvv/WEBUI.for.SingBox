<script setup lang="ts">
const model = defineModel<IDNSRule>({ required: true })
</script>

<template>
  <div class="form-item">
    {{ $t('kernel.route.rules.fields.domain') }}
    <InputList v-model="model.domain" />
  </div>
  <div class="form-item">
    {{ $t('kernel.route.rules.fields.domain_suffix') }}
    <InputList v-model="model.domain_suffix" />
  </div>
  <div class="form-item">
    {{ $t('kernel.route.rules.fields.domain_keyword') }}
    <InputList v-model="model.domain_keyword" />
  </div>
  <div class="form-item">
    {{ $t('kernel.route.rules.fields.domain_regex') }}
    <InputList v-model="model.domain_regex" />
  </div>
</template>
