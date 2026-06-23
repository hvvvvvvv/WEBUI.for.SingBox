<script lang="ts" setup>
import { useI18n } from 'vue-i18n'

import { LogLevelOptions } from '@/constant/kernel'
import { useBool } from '@/hooks'

const model = defineModel<{ log: IProfile['log']; experimental: IProfile['experimental'] }>({
  required: true,
})

const { t } = useI18n()
const [showMore, toggleMore] = useBool(false)
</script>

<template>
  <div>
    <div class="form-item">
      {{ t('kernel.log.disabled') }}
      <Switch v-model="model.log.disabled" />
    </div>
    <template v-if="!model.log.disabled">
      <div class="form-item">
        {{ t('kernel.log.level') }}
        <Radio v-model="model.log.level" :options="LogLevelOptions" />
      </div>
      <div class="form-item">
        {{ t('kernel.log.output') }}
        <Input v-model="model.log.output" editable />
      </div>
      <div class="form-item">
        {{ t('kernel.log.timestamp') }}
        <Switch v-model="model.log.timestamp" />
      </div>
    </template>
    <Divider>
      <Button type="text" size="small" @click="toggleMore">{{ t('common.more') }}</Button>
    </Divider>
    <div v-show="showMore">
      <div class="form-item">
        {{ t('kernel.cache_file.enabled') }}
        <Switch v-model="model.experimental.cache_file.enabled" />
      </div>
      <template v-if="model.experimental.cache_file.enabled">
        <div class="form-item">
          {{ t('kernel.cache_file.path') }}
          <Input v-model="model.experimental.cache_file.path" editable />
        </div>
        <div class="form-item">
          {{ t('kernel.cache_file.cache_id') }}
          <Input v-model="model.experimental.cache_file.cache_id" editable />
        </div>
        <div class="form-item">
          {{ t('kernel.cache_file.store_fakeip') }}
          <Switch v-model="model.experimental.cache_file.store_fakeip" />
        </div>
        <div class="form-item">
          {{ t('kernel.cache_file.store_rdrc') }}
          <Switch v-model="model.experimental.cache_file.store_rdrc" />
        </div>
      </template>
    </div>
  </div>
</template>
