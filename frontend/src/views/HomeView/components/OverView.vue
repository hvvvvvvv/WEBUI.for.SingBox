<script setup lang="ts">
import { ref, computed, watch, onUnmounted } from 'vue'
import { useI18n } from 'vue-i18n'

import { useAppStore, useKernelApiStore } from '@/stores'
import { formatBytes, handleChangeMode, message } from '@/utils'
import { nativeModeOptions, sameKernelMode } from '@/utils/nativeKernelUi'

import { useModal } from '@/components/Modal'

import ConnectionsController from './ConnectionsController.vue'
import LogsController from './LogsController.vue'

const trafficHistory = ref<[number[], number[]]>([[], []])
const statistics = ref({
  upload: 0,
  download: 0,
  downloadTotal: 0n,
  uploadTotal: 0n,
  connections: 0,
  inuse: 0n,
  goroutines: 0,
})

const { t } = useI18n()
const [Modal, modalApi] = useModal({})
const appStore = useAppStore()
const kernelApiStore = useKernelApiStore()
const modes = computed(() => nativeModeOptions(kernelApiStore.config.modeList))
const changeMode = (mode: string) =>
  handleChangeMode(mode).catch((error: any) => message.error(error.message || error))

const handleRestartKernel = async () => {
  try {
    await kernelApiStore.restartCore()
  } catch (error: any) {
    console.error(error)
    message.error(error)
  }
}

const handleStopKernel = async () => {
  try {
    await kernelApiStore.stopCore()
  } catch (error: any) {
    console.error(error)
    message.error(error)
  }
}

const handleShowApiLogs = () => {
  modalApi.setProps({
    title: 'Logs',
    cancelText: 'common.close',
    width: '90',
    height: '90',
    submit: false,
    maskClosable: true,
  })
  modalApi.setContent(LogsController).open()
}

const handleShowApiConnections = () => {
  modalApi.setProps({
    title: 'home.overview.connections',
    cancelText: 'common.close',
    width: '90',
    height: '90',
    submit: false,
    maskClosable: true,
  })
  modalApi.setContent(ConnectionsController).open()
}

const onRuntimeInboundSwitchChange = async (inbound: IInbound, enable: boolean) => {
  try {
    await kernelApiStore.updateRuntimeInboundEnable(inbound.id, enable)
  } catch (error: any) {
    inbound.enable = !enable
    console.error(error)
    message.error(error)
  }
}

const unregisterStatusHandler = kernelApiStore.onStatus((data) => {
  statistics.value.inuse = data.memory
  statistics.value.goroutines = data.goroutines
  statistics.value.connections = data.connectionsIn
  statistics.value.uploadTotal = data.uplinkTotal
  statistics.value.downloadTotal = data.downlinkTotal
  statistics.value.upload = data.trafficAvailable ? Number(data.uplink) : 0
  statistics.value.download = data.trafficAvailable ? Number(data.downlink) : 0
  trafficHistory.value[0].push(statistics.value.upload)
  trafficHistory.value[1].push(statistics.value.download)
  if (trafficHistory.value[0].length > 60) {
    trafficHistory.value[0].shift()
    trafficHistory.value[1].shift()
  }
})
watch(
  () => kernelApiStore.pid,
  () => {
    statistics.value = {
      upload: 0,
      download: 0,
      uploadTotal: 0n,
      downloadTotal: 0n,
      connections: 0,
      inuse: 0n,
      goroutines: 0,
    }
    trafficHistory.value = [[], []]
  },
  { flush: 'sync' },
)
onUnmounted(unregisterStatusHandler)
</script>

<template>
  <div>
    <div class="flex items-center rounded-8 px-8 py-4" style="background-color: var(--card-bg)">
      <div
        v-if="kernelApiStore.runtimeInbounds.length"
        class="h-20 px-6 rounded-4 text-12 font-bold text-nowrap inline-flex items-center gap-4 shrink-0"
        style="
          color: var(--primary-color);
          background-color: color-mix(in srgb, var(--primary-color) 12%, transparent);
        "
      >
        <Icon icon="inbound" :size="12" color="var(--primary-color)" />
        <span>{{ t('profiles.inbounds') }}</span>
      </div>
      <Switch
        v-for="inbound in kernelApiStore.runtimeInbounds"
        :key="inbound.id"
        v-model="inbound.enable"
        size="small"
        border="square"
        class="ml-8"
        style="
          --switch-on-bg: color-mix(in srgb, var(--primary-color) 78%, white);
          --switch-on-hover-bg: color-mix(in srgb, var(--primary-color) 88%, white);
        "
        @change="(enable) => onRuntimeInboundSwitchChange(inbound, enable)"
      >
        {{ inbound.tag }}
      </Switch>
      <CustomAction :actions="appStore.customActions.core_state" />
      <Button
        v-tips="'home.overview.viewlog'"
        type="text"
        size="small"
        icon="log"
        class="ml-auto"
        @click="handleShowApiLogs"
      />
      <Button
        v-tips="'home.overview.restart'"
        :loading="kernelApiStore.restarting"
        type="text"
        size="small"
        icon="restart"
        @click="handleRestartKernel"
      />
      <Button
        v-tips="'home.overview.stop'"
        :loading="kernelApiStore.stopping"
        type="text"
        size="small"
        icon="stop"
        @click="handleStopKernel"
      />
    </div>
    <div class="flex mt-20 gap-12">
      <Card :title="t('home.overview.realtimeTraffic')" class="flex-1">
        <div class="py-8 text-12">
          ↑ {{ formatBytes(statistics.upload) }}/s ↓ {{ formatBytes(statistics.download) }}/s
        </div>
      </Card>
      <Card :title="t('home.overview.totalTraffic')" class="flex-1">
        <div class="py-8 text-12">
          ↑ {{ formatBytes(statistics.uploadTotal) }} ↓ {{ formatBytes(statistics.downloadTotal) }}
        </div>
      </Card>
      <Card
        :title="t('home.overview.connections')"
        class="flex-1 cursor-pointer"
        @click="handleShowApiConnections"
      >
        <div class="py-8 text-12">
          {{ statistics.connections }}
        </div>
      </Card>
      <Card :title="t('home.overview.memory')" class="flex-1">
        <div class="py-8 text-12">
          {{ formatBytes(statistics.inuse) }}
        </div>
      </Card>
      <Card :title="t('home.overview.goroutines')" class="flex-1">
        <div class="py-8 text-12">
          {{ statistics.goroutines }}
        </div>
      </Card>
    </div>
    <div class="flex">
      <div class="w-[60%]">
        <div class="py-16 font-bold" style="color: var(--card-color)">
          {{ t('home.overview.traffic') }}
        </div>
        <TrafficChart
          :series="trafficHistory"
          :legend="[t('home.overview.transmit'), t('home.overview.receive')]"
        />
      </div>
      <div class="ml-12 flex-1">
        <div class="py-16 font-bold" style="color: var(--card-color)">
          {{ t('kernel.mode') }}
        </div>
        <div class="flex flex-col gap-12">
          <Card
            v-for="mode in modes"
            :key="mode.value"
            :selected="sameKernelMode(kernelApiStore.config.mode, mode.value)"
            :title="mode.desc ? t(mode.label) : mode.label"
            class="cursor-pointer"
            @click="changeMode(mode.value)"
          >
            <div v-if="mode.desc" class="text-12 py-2">{{ t(mode.desc) }}</div>
          </Card>
        </div>
      </div>
    </div>
  </div>

  <Modal />
</template>
