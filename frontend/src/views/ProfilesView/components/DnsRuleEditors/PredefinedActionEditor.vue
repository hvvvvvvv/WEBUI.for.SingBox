<script setup lang="ts">
import { DnsRcodeOptions } from '@/constant/kernel'

const model = defineModel<IDNSActionOptions>({ required: true })
</script>

<template>
  <div class="form-item action-field action-field-wide">
    {{ $t('kernel.dns.rules.fields.rcode') }}
    <Select v-model="model.rcode" :options="DnsRcodeOptions" />
  </div>
  <div class="form-item action-field action-field-wide">
    {{ $t('kernel.dns.rules.fields.answer') }}
    <InputList v-model="model.answer" />
  </div>
  <div class="form-item action-field action-field-wide">
    {{ $t('kernel.dns.rules.fields.ns') }}
    <InputList v-model="model.ns" />
  </div>
  <div class="form-item action-field action-field-wide">
    {{ $t('kernel.dns.rules.fields.extra') }}
    <InputList v-model="model.extra" />
  </div>
</template>
