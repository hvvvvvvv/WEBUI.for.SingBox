<script lang="ts" setup>
import { h, inject, ref } from 'vue'
import { useI18n } from 'vue-i18n'

import { DefaultCoreConfig } from '@/constant/kernel'
import { Branch } from '@/enums/app'
import { useAppConfigStore, useKernelApiStore } from '@/stores'
import { deepClone, message } from '@/utils'

import Button from '@/components/Button/index.vue'

interface Props {
  isAlpha: boolean
}

const props = defineProps<Props>()

const tabs = [
  { tab: 'settings.kernel.config.env', key: 'env' },
  { tab: 'settings.kernel.config.args', key: 'args' },
]

const activeKey = ref('env')
const handleCancel = inject('cancel') as any
const handleSubmit = inject('submit') as any

const { t } = useI18n()
const appConfig = useAppConfigStore()
const kernelApiStore = useKernelApiStore()

const source = props.isAlpha ? appConfig.config.alpha : appConfig.config.main

const model = ref(deepClone(source))

const handleSave = async () => {
  Object.assign(source, model.value)
  try {
    await appConfig.saveNow()
    const editingCurrentBranch = props.isAlpha
      ? appConfig.config.branch === Branch.Alpha
      : appConfig.config.branch === Branch.Main
    if (editingCurrentBranch && kernelApiStore.running) {
      await kernelApiStore.restartCore()
    }
    handleSubmit()
  } catch (error: any) {
    message.error(error.message || error)
  }
}

const modalSlots = {
  action: () =>
    h(
      Button,
      {
        type: 'link',
        class: 'mr-auto',
        onClick: () => {
          const { env, args } = DefaultCoreConfig()
          model.value.env = env
          model.value.args = args
          message.success('common.success')
        },
      },
      () => t('common.reset'),
    ),
  cancel: () =>
    h(
      Button,
      {
        onClick: handleCancel,
      },
      () => t('common.cancel'),
    ),
  submit: () =>
    h(
      Button,
      {
        type: 'primary',
        onClick: handleSave,
      },
      () => t('common.save'),
    ),
}

defineExpose({ modalSlots })
</script>

<template>
  <div>
    <Tabs v-model:active-key="activeKey" :items="tabs">
      <template #env>
        <KeyValueEditor v-model="model.env" />
      </template>

      <template #args>
        <InputList v-model="model.args" />
      </template>
    </Tabs>
  </div>
</template>
