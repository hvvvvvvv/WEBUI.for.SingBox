<script lang="ts" setup>
import { ref, computed, watch, onUnmounted } from 'vue'
import { useI18n } from 'vue-i18n'

import { closeConnection } from '@/api/kernel'
import { DraggableOptions } from '@/constant/app'
import { DefaultConnections } from '@/constant/kernel'
import { useBool } from '@/hooks'
import { useAppSettingsStore, useKernelApiStore } from '@/stores'
import { addToRuleSet, formatBytes, formatRelativeTime, message, picker } from '@/utils'
import {
  compareBigInt,
  connectionRuleOptions,
  stringifyNative,
  freezeConnectionSnapshot,
} from '@/utils/nativeKernelUi'
import type { Column } from '@/components/Table/index.vue'
import type { Menu } from '@/types/app'
import type { NativeConnectionRow, NativeConnectionSnapshot } from '@/types/kernel'

type DisplayRow = NativeConnectionRow & { id: string }
const appSettingsStore = useAppSettingsStore()
const kernelApiStore = useKernelApiStore()
const { t } = useI18n()
const details = ref('')
const isActive = ref(true)
const keywords = ref('')
const closing = ref(false)
const [showDetails, toggleDetails] = useBool(false)
const [showSettings, toggleSettings] = useBool(false)
const [isPause, togglePause] = useBool(false)
const latest = ref<NativeConnectionSnapshot>({ active: [], closed: [] })
const displayed = ref<NativeConnectionSnapshot>({ active: [], closed: [] })
watch(isPause, (paused) => {
  if (!paused) displayed.value = freezeConnectionSnapshot(latest.value)
})

const visible = (key: string) => !appSettingsStore.app.connections.visibility[key]
const columns = computed(() =>
  (
    [
      {
        title: 'home.connections.type',
        key: 'connection.inboundType',
        align: 'center',
        hidden: visible('connection.inboundType'),
        sort: (a, b) => a.connection.inboundType.localeCompare(b.connection.inboundType),
        customRender: ({ record }) =>
          `${record.connection.inboundType} (${record.connection.network})`,
      },
      {
        title: 'home.connections.processPath',
        key: 'connection.processInfo.processPath',
        hidden: visible('connection.processInfo.processPath'),
        sort: (a, b) =>
          (a.connection.processInfo?.processPath || '').localeCompare(
            b.connection.processInfo?.processPath || '',
          ),
      },
      {
        title: 'home.connections.host',
        key: 'connection.domain',
        hidden: visible('connection.domain'),
        sort: (a, b) => a.connection.domain.localeCompare(b.connection.domain),
        customRender: ({ value, record }) => value || record.connection.destination,
      },
      {
        title: 'home.connections.sourceIP',
        key: 'connection.source',
        align: 'center',
        hidden: visible('connection.source'),
        sort: (a, b) => a.connection.source.localeCompare(b.connection.source),
      },
      {
        title: 'home.connections.destinationIP',
        key: 'connection.destination',
        align: 'center',
        hidden: visible('connection.destination'),
        sort: (a, b) => a.connection.destination.localeCompare(b.connection.destination),
      },
      {
        title: 'home.connections.rule',
        key: 'connection.rule',
        align: 'center',
        hidden: visible('connection.rule'),
        sort: (a, b) => a.connection.rule.localeCompare(b.connection.rule),
      },
      {
        title: 'home.connections.chains',
        key: 'connection.chainList',
        hidden: visible('connection.chainList'),
        sort: (a, b) =>
          a.connection.chainList.join(' / ').localeCompare(b.connection.chainList.join(' / ')),
        customRender: ({ value }) => value.slice().reverse().join(' :: '),
      },
      {
        title: 'home.connections.uploadSpeed',
        key: 'uplinkBps',
        align: 'center',
        minWidth: '90px',
        hidden: visible('uplinkBps'),
        sort: (a, b) => a.uplinkBps - b.uplinkBps,
        customRender: ({ value }) => formatBytes(value) + '/s',
      },
      {
        title: 'home.connections.downSpeed',
        key: 'downlinkBps',
        align: 'center',
        minWidth: '90px',
        hidden: visible('downlinkBps'),
        sort: (a, b) => a.downlinkBps - b.downlinkBps,
        customRender: ({ value }) => formatBytes(value) + '/s',
      },
      {
        title: 'home.connections.upload',
        key: 'uplinkTotal',
        align: 'center',
        hidden: visible('uplinkTotal'),
        sort: (a, b) => compareBigInt(a.uplinkTotal, b.uplinkTotal),
        customRender: ({ value }) => formatBytes(value),
      },
      {
        title: 'home.connections.download',
        key: 'downlinkTotal',
        align: 'center',
        hidden: visible('downlinkTotal'),
        sort: (a, b) => compareBigInt(a.downlinkTotal, b.downlinkTotal),
        customRender: ({ value }) => formatBytes(value),
      },
      {
        title: 'home.connections.time',
        key: 'connection.createdAt',
        align: 'center',
        hidden: visible('connection.createdAt'),
        sort: (a, b) => compareBigInt(a.connection.createdAt, b.connection.createdAt),
        customRender: ({ value }) => formatRelativeTime(Number(value)),
      },
    ] as Column[]
  ).sort(
    (a, b) =>
      appSettingsStore.app.connections.order.indexOf(a.key) -
      appSettingsStore.app.connections.order.indexOf(b.key),
  ),
)
const columnTitleMap = computed(() =>
  Object.fromEntries(columns.value.map((column) => [column.key, column.title])),
)
const filteredConnections = computed<DisplayRow[]>(() => {
  const search = keywords.value.toLowerCase()
  return (isActive.value ? displayed.value.active : displayed.value.closed)
    .filter((row) => {
      const connection = row.connection
      return (
        !search ||
        [
          connection.inbound,
          connection.inboundType,
          connection.network,
          connection.source,
          connection.destination,
          connection.domain,
          connection.protocol,
          connection.user,
          connection.rule,
          connection.outbound,
          ...connection.chainList,
          connection.processInfo?.processPath,
          connection.processInfo?.userName,
          ...(connection.processInfo?.packageNames || []),
        ].some((value) => value?.toLowerCase().includes(search))
      )
    })
    .map((row) => ({ ...row, id: row.connection.id }))
})
const activeIds = () => new Set(latest.value.active.map((row) => row.connection.id))
const menu: Menu[] = [
  {
    label: 'common.details',
    handler: (row: DisplayRow) => {
      details.value = stringifyNative(row)
      toggleDetails()
    },
  },
  {
    label: 'home.connections.close',
    handler: async (row: DisplayRow) => {
      if (!isActive.value || !activeIds().has(row.id)) return
      try {
        await closeConnection(row.id)
      } catch (error: any) {
        message.error(error.message || error)
      }
    },
  },
  ...(
    [
      ['home.connections.addToDirect', 'direct'],
      ['home.connections.addToProxy', 'proxy'],
      ['home.connections.addToReject', 'reject'],
    ] as const
  ).map(([label, ruleset]) => ({
    label,
    handler: async (row: DisplayRow) => {
      const options = connectionRuleOptions(row.connection).map(({ type, value }) => ({
        label: t(`kernel.rules.type.${type}`),
        value: { [type]: value },
        description: value,
      }))
      if (!options.length) {
        message.error('Not Matched')
        return
      }
      try {
        const payloads = await picker.multi('rulesets.selectRuleType', options)
        await addToRuleSet(ruleset, payloads)
        message.success('common.success')
      } catch (error: any) {
        message.error(error.message || error)
      }
    },
  })),
]
const handleCloseAll = async () => {
  if (closing.value || !isActive.value) return
  const live = activeIds()
  // Preserve the displayed filter and paused snapshot scope; never close newly hidden rows.
  const ids = filteredConnections.value.map((row) => row.id).filter((id) => live.has(id))
  closing.value = true
  try {
    await Promise.all(ids.map(closeConnection))
  } catch (error: any) {
    message.error(error.message || error)
  } finally {
    closing.value = false
  }
}
const handleClearClosedConns = () => {
  kernelApiStore.clearClosedConnections()
  displayed.value.closed = []
}
const handleResetConnections = () => {
  appSettingsStore.app.connections = DefaultConnections()
  message.success('common.success')
}
const unregisterConnectionsHandler = kernelApiStore.onConnections((snapshot) => {
  latest.value = snapshot
  if (!isPause.value) displayed.value = freezeConnectionSnapshot(snapshot)
})
watch(
  () => kernelApiStore.pid,
  () => {
    latest.value = { active: [], closed: [] }
    displayed.value = { active: [], closed: [] }
  },
  { flush: 'sync' },
)
onUnmounted(unregisterConnectionsHandler)
</script>

<template>
  <div class="flex flex-col h-full">
    <div class="flex items-center">
      <Radio
        v-model="isActive"
        :options="[
          { label: 'home.connections.active', value: true },
          { label: 'home.connections.closed', value: false },
        ]"
        size="small"
      />
      <Input v-model="keywords" clearable size="small" placeholder="Search" class="ml-8 flex-1" />
      <Button
        :icon="isPause ? 'play' : 'pause'"
        size="small"
        type="text"
        class="ml-8"
        @click="togglePause"
      />
      <Button
        v-if="isActive"
        v-tips="'home.connections.closeAll'"
        icon="close"
        size="small"
        type="text"
        :loading="closing"
        @click="handleCloseAll"
      />
      <Button
        v-else
        v-tips="'common.clear'"
        icon="clear"
        size="small"
        type="text"
        @click="handleClearClosedConns"
      />
      <Button icon="settings" size="small" type="text" @click="toggleSettings" />
    </div>
    <Table
      class="flex-1 mt-8"
      :columns="columns"
      :menu="menu"
      :data-source="filteredConnections"
      sort="connection.createdAt"
    />
  </div>

  <Modal
    v-model:open="showDetails"
    :submit="false"
    cancel-text="common.close"
    title="home.connections.details"
    max-height="80"
    max-width="80"
    mask-closable
  >
    <CodeViewer v-model="details" />
  </Modal>

  <Modal
    v-model:open="showSettings"
    :submit="false"
    mask-closable
    max-height="80"
    cancel-text="common.close"
    title="home.connections.sort"
  >
    <template #action>
      <Button type="text" class="mr-auto" @click="handleResetConnections">
        {{ t('common.reset') }}
      </Button>
    </template>
    <div v-draggable="[appSettingsStore.app.connections.order, DraggableOptions]">
      <Card v-for="column in appSettingsStore.app.connections.order" :key="column" class="mb-2">
        <div class="flex items-center justify-between py-2">
          <span class="font-bold">{{ t(columnTitleMap[column] || column) }}</span>
          <Switch v-model="appSettingsStore.app.connections.visibility[column]" />
        </div>
      </Card>
    </div>
  </Modal>
</template>
