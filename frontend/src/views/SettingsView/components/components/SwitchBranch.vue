<script setup lang="ts">
import { useI18n } from 'vue-i18n'

import { Branch } from '@/enums/app'
import { useAppConfigStore } from '@/stores'
import { message } from '@/utils'

const { t } = useI18n()
const appConfig = useAppConfigStore()

const handleUseBranch = async (branch: Branch) => {
  if (appConfig.config.branch === branch) return
  appConfig.config.branch = branch
  try {
    await appConfig.saveNow()
  } catch (error: any) {
    message.error(error.message || error)
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
