<script setup lang="ts">
import { useI18n } from 'vue-i18n'

import { Branch } from '@/enums/app'
import { useAppConfigStore, useKernelApiStore } from '@/stores'
import { message } from '@/utils'

const { t } = useI18n()
const appConfig = useAppConfigStore()
const kernelApiStore = useKernelApiStore()

const handleUseBranch = async (branch: Branch) => {
  appConfig.config.branch = branch

  if (!kernelApiStore.running) return

  try {
    await kernelApiStore.restartCore()
  } catch (error: any) {
    message.error(error)
  }
}
</script>

<template>
  <div class="font-bold text-16 mx-4 my-12">{{ t('settings.kernel.version') }}</div>
  <div class="flex gap-8">
    <Card
      :selected="appConfig.config.branch === Branch.Main"
      title="Stable"
      class="w-[36%]"
      @click="handleUseBranch(Branch.Main)"
    >
      <div class="py-4 text-12">
        {{ t('settings.kernel.stable') }}
      </div>
    </Card>
    <Card
      :selected="appConfig.config.branch === Branch.Alpha"
      title="Alpha"
      class="w-[36%]"
      @click="handleUseBranch(Branch.Alpha)"
    >
      <div class="py-4 text-12">
        {{ t('settings.kernel.alpha') }}
      </div>
    </Card>
  </div>
</template>
