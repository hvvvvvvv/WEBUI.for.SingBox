<script setup lang="ts">
import { computed } from 'vue'

import { DnsRcodeOptions } from '@/constant/kernel'

const model = defineModel<IDNSRule>({ required: true })

const hasDependencies = computed(
  () =>
    model.value.ip_accept_any ||
    model.value.ip_is_private ||
    model.value.ip_cidr.length > 0 ||
    !!model.value.response_rcode ||
    model.value.response_answer.length > 0 ||
    model.value.response_ns.length > 0 ||
    model.value.response_extra.length > 0,
)
</script>

<template>
  <div class="form-item">
    {{ $t('kernel.dns.rules.fields.match_response') }}
    <Switch v-model="model.match_response" :disabled="hasDependencies && model.match_response" />
  </div>
  <div class="form-item">
    {{ $t('kernel.dns.rules.fields.ip_accept_any') }}
    <Switch v-model="model.ip_accept_any" />
  </div>
  <div class="form-item">
    {{ $t('kernel.dns.rules.fields.ip_cidr') }}
    <InputList v-model="model.ip_cidr" />
  </div>
  <div class="form-item">
    {{ $t('kernel.dns.rules.fields.ip_is_private') }}
    <Switch v-model="model.ip_is_private" />
  </div>
  <div class="form-item">
    {{ $t('kernel.dns.rules.fields.response_rcode') }}
    <Select v-model="model.response_rcode" :options="DnsRcodeOptions" clearable />
  </div>
  <div class="form-item">
    {{ $t('kernel.dns.rules.fields.response_answer') }}
    <InputList v-model="model.response_answer" />
  </div>
  <div class="form-item">
    {{ $t('kernel.dns.rules.fields.response_ns') }}
    <InputList v-model="model.response_ns" />
  </div>
  <div class="form-item">
    {{ $t('kernel.dns.rules.fields.response_extra') }}
    <InputList v-model="model.response_extra" />
  </div>
</template>
