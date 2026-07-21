<script setup lang="ts">
const model = defineModel<IDNSActionOptions>({ required: true })

const addRewriteTTL = () => {
  model.value.rewrite_ttl = 0
}

const removeRewriteTTL = () => {
  delete model.value.rewrite_ttl
}
</script>

<template>
  <div class="action-toggle-group">
    <ActionToggleField
      v-model="model.disable_cache"
      :label="$t('kernel.route.rules.disable_cache')"
    />
    <ActionToggleField
      v-model="model.disable_optimistic_cache"
      :label="$t('kernel.route.rules.fields.disable_optimistic_cache')"
    />
  </div>
  <div class="form-item action-field action-field-wide">
    {{ $t('kernel.route.rules.fields.rewrite_ttl') }}
    <div v-if="model.rewrite_ttl !== undefined" class="action-input-row flex items-center gap-4">
      <Input v-model="model.rewrite_ttl" type="number" :min="0" />
      <Button icon="close" type="text" @click="removeRewriteTTL" />
    </div>
    <Button v-else icon="add" type="text" @click="addRewriteTTL">
      {{ $t('common.add') }}
    </Button>
  </div>
  <div class="form-item action-field">
    {{ $t('kernel.route.rules.fields.timeout') }}
    <Input v-model="model.timeout" />
  </div>
  <div class="form-item action-field action-field-wide">
    {{ $t('kernel.route.rules.client_subnet') }}
    <Input v-model="model.client_subnet" />
  </div>
</template>
