<script setup lang="ts">
const model = defineModel<IRule>({ required: true })
</script>

<template>
  <div class="form-item">
    {{ $t('kernel.route.rules.fields.source_ip_cidr') }}
    <InputList v-model="model.source_ip_cidr" placeholder="10.0.0.0/24" />
  </div>
  <div class="form-item">
    {{ $t('kernel.route.rules.fields.source_ip_is_private') }}
    <Switch v-model="model.source_ip_is_private" />
  </div>
  <div class="form-item">
    {{ $t('kernel.route.rules.fields.source_port') }}
    <InputList v-model="model.source_port" type="number" :min="0" :max="65535" />
  </div>
  <div class="form-item">
    {{ $t('kernel.route.rules.fields.source_port_range') }}
    <InputList v-model="model.source_port_range" placeholder="1000:2000" />
  </div>
</template>
