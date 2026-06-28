<script setup lang="ts">
import { h, inject, onMounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

import { TunStackOptions } from '@/constant/kernel'
import { useKernelApiStore } from '@/stores'
import { message } from '@/utils'

import Button from '@/components/Button/index.vue'

import type { RuntimeConfigChange } from '@/stores/kernelApi'

const { t } = useI18n()
const kernelApiStore = useKernelApiStore()
const handleSubmit = inject('submit') as any

type DraftConfig = {
  mixedPort: number
  httpPort: number
  socksPort: number
  allowLan: boolean
  tunStack: string
  tunDevice: string
  interfaceName: string
}

const createDraftConfig = (): DraftConfig => ({
  mixedPort: kernelApiStore.config['mixed-port'],
  httpPort: kernelApiStore.config.port,
  socksPort: kernelApiStore.config['socks-port'],
  allowLan: kernelApiStore.config['allow-lan'],
  tunStack: kernelApiStore.config.tun.stack,
  tunDevice: kernelApiStore.config.tun.device,
  interfaceName: kernelApiStore.config['interface-name'],
})

let initialConfig = createDraftConfig()
const draftConfig = reactive<DraftConfig>({ ...initialConfig })
const saving = ref(false)

const normalizePort = (port: number | string) => Number(port) || 0

const syncDraftConfig = () => {
  initialConfig = createDraftConfig()
  Object.assign(draftConfig, initialConfig)
}

const collectChanges = (): RuntimeConfigChange[] => {
  const changes: RuntimeConfigChange[] = []
  const mixedPort = normalizePort(draftConfig.mixedPort)
  const httpPort = normalizePort(draftConfig.httpPort)
  const socksPort = normalizePort(draftConfig.socksPort)

  if (mixedPort !== initialConfig.mixedPort) {
    changes.push({ field: 'mixed', value: mixedPort })
  }
  if (httpPort !== initialConfig.httpPort) {
    changes.push({ field: 'http', value: httpPort })
  }
  if (socksPort !== initialConfig.socksPort) {
    changes.push({ field: 'socks', value: socksPort })
  }
  if (draftConfig.allowLan !== initialConfig.allowLan) {
    changes.push({ field: 'allow-lan', value: draftConfig.allowLan })
  }
  if (draftConfig.tunStack !== initialConfig.tunStack) {
    changes.push({ field: 'tun-stack', value: { stack: draftConfig.tunStack } })
  }
  if (draftConfig.tunDevice !== initialConfig.tunDevice) {
    changes.push({ field: 'tun-device', value: { device: draftConfig.tunDevice } })
  }
  if (draftConfig.interfaceName !== initialConfig.interfaceName) {
    changes.push({
      field: 'interface-name',
      value: { interface_name: draftConfig.interfaceName },
    })
  }

  return changes
}

const handleSave = async () => {
  if (saving.value) return
  saving.value = true
  try {
    const changes = collectChanges()
    if (changes.length > 0) {
      await kernelApiStore.updateConfigs(changes)
    }
    syncDraftConfig()
    saving.value = false
    await handleSubmit?.()
  } catch (error: any) {
    console.error(error)
    message.error(error.message || error)
  } finally {
    saving.value = false
  }
}

onMounted(syncDraftConfig)

watch(
  () => [
    kernelApiStore.config['mixed-port'],
    kernelApiStore.config.port,
    kernelApiStore.config['socks-port'],
    kernelApiStore.config['allow-lan'],
    kernelApiStore.config.tun.stack,
    kernelApiStore.config.tun.device,
    kernelApiStore.config['interface-name'],
  ],
  () => {
    if (!saving.value && collectChanges().length === 0) {
      syncDraftConfig()
    }
  },
)

const modalSlots = {
  submit: () =>
    h(
      Button,
      {
        type: 'primary',
        loading: saving.value,
        onClick: handleSave,
      },
      () => t('common.save'),
    ),
}

defineExpose({ modalSlots })
</script>

<template>
  <div>
    <Divider class="w-full mb-8"> {{ t('home.overview.settingsTips') }} </Divider>
    <div class="grid grid-cols-4 gap-8 pb-16">
      <Card :title="t('kernel.inbounds.mixedPort')">
        <Input
          v-model="draftConfig.mixedPort"
          :min="0"
          :max="65535"
          type="number"
          :border="false"
          editable
          auto-size
          class="w-full"
        />
      </Card>
      <Card :title="t('kernel.inbounds.httpPort')">
        <Input
          v-model="draftConfig.httpPort"
          :min="0"
          :max="65535"
          type="number"
          :border="false"
          editable
          auto-size
          class="w-full"
        />
      </Card>
      <Card :title="t('kernel.inbounds.socksPort')">
        <Input
          v-model="draftConfig.socksPort"
          :min="0"
          :max="65535"
          type="number"
          editable
          :border="false"
          auto-size
          class="w-full"
        />
      </Card>
      <Card :title="t('kernel.allow-lan')">
        <Switch v-model="draftConfig.allowLan" />
      </Card>
      <Card :title="t('kernel.inbounds.tun.stack')">
        <Select
          v-model="draftConfig.tunStack"
          :options="TunStackOptions"
          :border="false"
          auto-size
        />
      </Card>
      <Card :title="t('kernel.inbounds.tun.interface_name')">
        <Input
          v-model="draftConfig.tunDevice"
          editable
          :border="false"
          auto-size
          class="w-full"
        />
      </Card>
      <Card :title="t('kernel.route.default_interface')">
        <InterfaceSelect
          v-model="draftConfig.interfaceName"
          :border="false"
          auto-size
        />
      </Card>
      <Card :title="t('common.none')"> </Card>
    </div>
  </div>
</template>
