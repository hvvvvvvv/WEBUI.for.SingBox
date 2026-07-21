<script setup lang="ts">
import {
  NetworkStrategyOptions,
  NetworkTypeOptions,
  TLSSpoofMethodOptions,
} from '@/constant/kernel'

const model = defineModel<IActionOptions>({ required: true })

const handleTlsFragment = (enabled: boolean) => {
  if (enabled) model.value.tls_record_fragment = false
}

const handleTlsRecordFragment = (enabled: boolean) => {
  if (enabled) model.value.tls_fragment = false
}
</script>

<template>
  <div class="form-item action-field action-field-wide">
    {{ $t('kernel.route.rules.fields.override_address') }}
    <Input v-model="model.override_address" />
  </div>
  <div class="form-item action-field">
    {{ $t('kernel.route.rules.fields.override_port') }}
    <Input v-model="model.override_port" type="number" :min="0" :max="65535" />
  </div>
  <div class="form-item action-field">
    {{ $t('kernel.route.rules.fields.network_strategy') }}
    <OptionGroup
      v-model="model.network_strategy"
      :options="NetworkStrategyOptions"
      :aria-label="$t('kernel.route.rules.fields.network_strategy')"
      clearable
    />
  </div>
  <div class="form-item action-field action-field-wide">
    {{ $t('kernel.route.rules.fields.network_type') }}
    <OptionGroup
      v-model="model.network_type"
      :options="NetworkTypeOptions"
      :aria-label="$t('kernel.route.rules.fields.network_type')"
      multiple
    />
  </div>
  <div class="form-item action-field action-field-wide">
    {{ $t('kernel.route.rules.fields.fallback_network_type') }}
    <OptionGroup
      v-model="model.fallback_network_type"
      :options="NetworkTypeOptions"
      :aria-label="$t('kernel.route.rules.fields.fallback_network_type')"
      multiple
    />
  </div>
  <div class="form-item action-field">
    {{ $t('kernel.route.rules.fields.fallback_delay') }}
    <Input v-model="model.fallback_delay" placeholder="300ms" />
  </div>
  <div class="action-toggle-group">
    <ActionToggleField
      v-model="model.udp_disable_domain_unmapping"
      :label="$t('kernel.route.rules.fields.udp_disable_domain_unmapping')"
    />
    <ActionToggleField
      v-model="model.udp_connect"
      :label="$t('kernel.route.rules.fields.udp_connect')"
    />
  </div>
  <div class="form-item action-field">
    {{ $t('kernel.route.rules.fields.udp_timeout') }}
    <Input v-model="model.udp_timeout" placeholder="30s" />
  </div>
  <div class="action-toggle-group">
    <ActionToggleField
      v-model="model.tls_fragment"
      :label="$t('kernel.route.rules.fields.tls_fragment')"
      @change="handleTlsFragment"
    />
    <ActionToggleField
      v-model="model.tls_record_fragment"
      :label="$t('kernel.route.rules.fields.tls_record_fragment')"
      @change="handleTlsRecordFragment"
    />
  </div>
  <div v-if="model.tls_fragment" class="form-item action-field">
    {{ $t('kernel.route.rules.fields.tls_fragment_fallback_delay') }}
    <Input v-model="model.tls_fragment_fallback_delay" placeholder="500ms" />
  </div>
  <div class="form-item action-field action-field-wide">
    {{ $t('kernel.route.rules.fields.tls_spoof') }}
    <Input v-model="model.tls_spoof" placeholder="example.com" />
  </div>
  <div class="form-item action-field">
    {{ $t('kernel.route.rules.fields.tls_spoof_method') }}
    <Select v-model="model.tls_spoof_method" :options="TLSSpoofMethodOptions" clearable />
  </div>
</template>
