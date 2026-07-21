<script setup lang="ts">
const model = defineModel<IRule>({ required: true })
</script>

<template>
  <div class="form-item">
    {{ $t('kernel.route.rules.fields.domain') }}
    <InputList v-model="model.domain" placeholder="example.com" />
  </div>
  <div class="form-item">
    {{ $t('kernel.route.rules.fields.domain_suffix') }}
    <InputList v-model="model.domain_suffix" placeholder=".example.com" />
  </div>
  <div class="form-item">
    {{ $t('kernel.route.rules.fields.domain_keyword') }}
    <InputList v-model="model.domain_keyword" />
  </div>
  <div class="form-item">
    {{ $t('kernel.route.rules.fields.domain_regex') }}
    <InputList v-model="model.domain_regex" />
  </div>
  <div class="form-item">
    {{ $t('kernel.route.rules.fields.ip_cidr') }}
    <InputList v-model="model.ip_cidr" placeholder="192.0.2.0/24" />
  </div>
  <div class="form-item">
    {{ $t('kernel.route.rules.fields.ip_is_private') }}
    <Switch v-model="model.ip_is_private" />
  </div>
  <div class="form-item">
    {{ $t('kernel.route.rules.fields.port') }}
    <InputList v-model="model.port" type="number" :min="0" :max="65535" />
  </div>
  <div class="form-item">
    {{ $t('kernel.route.rules.fields.port_range') }}
    <InputList v-model="model.port_range" placeholder="1000:2000" />
  </div>
</template>
