<script setup lang="ts">
import { ref, computed, onUnmounted } from 'vue'
import { useI18n } from 'vue-i18n'

import {
  ControllerCloseModeOptions,
  DefaultCardColumns,
  DefaultControllerSensitivity,
} from '@/constant/app'
import { ControllerCloseMode } from '@/enums/app'
import type { Group, GroupItem } from '@/types/kernel'
import { useBool } from '@/hooks'
import { useNativeUrlTest } from '@/hooks/useNativeUrlTest'
import { useAppSettingsStore, useKernelApiStore, useProfilesStore } from '@/stores'
import { handleUseProxy, message, buildSmartRegExp } from '@/utils'

const expandedSet = ref<Set<string>>(new Set())
const filterKeywordsMap = ref<Record<string, string>>({})
const loading = ref(false)
const { t } = useI18n()
const [showMoreSettings, toggleMoreSettings] = useBool(false)
const appSettings = useAppSettingsStore()
const kernelApiStore = useKernelApiStore()
const profilesStore = useProfilesStore()
const unregisterGroups = kernelApiStore.subscribeGroups()
onUnmounted(unregisterGroups)
const { start: startDelay, isLoading } = useNativeUrlTest({
  groups: () => kernelApiStore.groups,
  outbounds: () => kernelApiStore.outbounds,
  pid: () => kernelApiStore.pid,
  running: () => kernelApiStore.running,
  onTimeout: ({ tag, isGroup, remainingCount }) => {
    message.warn(
      t(isGroup ? 'home.controller.groupTestTimeout' : 'home.controller.testTimeout', {
        tag,
        count: remainingCount,
      }),
    )
  },
})

const groups = computed(() => {
  const profileOutbounds = profilesStore.currentProfile?.outbounds || []
  const nativeGroups = new Map(kernelApiStore.groups.map((group) => [group.tag, group]))
  return kernelApiStore.groups
    .filter((group) => !profileOutbounds.some((item) => item.tag === group.tag && item.hidden))
    .map((group) => {
      const items = group.items
        .filter((item) => {
          const visible =
            appSettings.app.kernel.unAvailable ||
            ['direct', 'block', 'reject'].includes(item.type) ||
            nativeGroups.has(item.tag) ||
            isLoading(item.tag) ||
            item.urlTestDelay > 0
          const keywords = filterKeywordsMap.value[group.tag]
          return visible && (!keywords || buildSmartRegExp(keywords, 'i').test(item.tag))
        })
        .slice()
        .sort((a, b) => {
          if (!appSettings.app.kernel.sortByDelay || a.urlTestDelay === b.urlTestDelay) return 0
          if (!a.urlTestDelay) return 1
          if (!b.urlTestDelay) return -1
          return a.urlTestDelay - b.urlTestDelay
        })
      const chains: string[] = []
      const visited = new Set([group.tag])
      let selected = group.selected
      while (selected && !visited.has(selected)) {
        visited.add(selected)
        chains.push(selected)
        selected = nativeGroups.get(selected)?.selected || ''
      }
      return {
        ...group,
        items,
        chains,
        icon: profileOutbounds.find((item) => item.tag === group.tag)?.icon,
      }
    })
})

const useProxyWithCatchError = (group: Group, proxy: GroupItem) => {
  handleUseProxy(group, proxy).catch((error: any) => message.error(error.message || error))
}
const toggleExpanded = (tag: string) => {
  if (expandedSet.value.has(tag)) expandedSet.value.delete(tag)
  else expandedSet.value.add(tag)
}
const expandAll = () => groups.value.forEach(({ tag }) => expandedSet.value.add(tag))
const collapseAll = () => expandedSet.value.clear()
const isExpanded = (tag: string) => expandedSet.value.has(tag)
const isFiltered = (tag: string) => filterKeywordsMap.value[tag]

const handleDelay = async (tag: string) => {
  try {
    await startDelay(tag)
  } catch (error: any) {
    message.error(error.message || error)
  }
}
const handleRefresh = async () => {
  if (loading.value) return
  loading.value = true
  try {
    await Promise.all([kernelApiStore.refreshConfig(), kernelApiStore.refreshProviderProxies()])
  } catch (error: any) {
    message.error(error.message || error)
  } finally {
    loading.value = false
  }
}
const locateGroup = (group: Group, chain: string) => {
  collapseAll()
  toggleExpanded(kernelApiStore.groups.some((item) => item.tag === chain) ? chain : group.tag)
}
const delayColor = (delay = 0) => {
  if (delay === 0) return 'var(--level-0-color)'
  if (delay < 500) return 'var(--level-1-color)'
  if (delay < 1000) return 'var(--level-2-color)'
  if (delay < 1500) return 'var(--level-3-color)'
  return 'var(--level-4-color)'
}
const handleResetMoreSettings = () => {
  appSettings.app.kernel.controllerCloseMode = ControllerCloseMode.All
  appSettings.app.kernel.controllerSensitivity = DefaultControllerSensitivity
  appSettings.app.kernel.cardColumns = DefaultCardColumns
  message.success('common.success')
}
</script>

<template>
  <div class="m-8 mt-0 sticky top-0 z-3">
    <div
      class="sticky flex gap-8 items-center p-8 rounded-8 backdrop-blur-sm"
      style="background-color: var(--card-bg)"
    >
      <Switch v-model="appSettings.app.kernel.autoClose" label="home.controller.autoClose" />
      <Switch v-model="appSettings.app.kernel.unAvailable" label="home.controller.unAvailable" />
      <Switch v-model="appSettings.app.kernel.cardMode" label="home.controller.cardMode" />
      <Switch v-model="appSettings.app.kernel.sortByDelay" label="home.controller.sortBy" />
      <Button type="primary" size="small" @click="toggleMoreSettings"> ... </Button>
      <div class="ml-auto flex items-center">
        <Button v-tips="'home.overview.expandAll'" type="text" icon="expand" @click="expandAll" />
        <Button
          v-tips="'home.overview.collapseAll'"
          type="text"
          icon="collapse"
          @click="collapseAll"
        />
        <Button
          v-tips="'home.overview.refresh'"
          :loading="loading"
          icon="refresh"
          type="text"
          @click="handleRefresh"
        />
      </div>
    </div>
  </div>
  <div v-for="group in groups" :key="group.tag" class="m-8">
    <div
      class="sticky z-2 flex gap-8 items-center p-8 rounded-8 backdrop-blur-sm"
      style="top: 52px; background-color: var(--card-bg)"
      @click="toggleExpanded(group.tag)"
    >
      <div class="text-14 flex items-center gap-2 text-nowrap overflow-hidden">
        <img v-if="group.icon" :src="group.icon" class="w-24 h-24 mr-4" draggable="false" />
        <span class="font-bold text-18">{{ group.tag }}</span>
        <span class="mx-8">
          {{ group.type }}
        </span>
        <span> :: </span>
        <template v-for="(chain, index) in group.chains" :key="chain">
          <span v-if="index !== 0" style="color: gray"> / </span>
          <Button type="text" size="small" @click.stop="locateGroup(group, chain)">
            {{ chain }}
          </Button>
        </template>
      </div>
      <div class="ml-auto flex items-center" @click.stop>
        <Input
          v-model="filterKeywordsMap[group.tag]"
          :placeholder="t('common.keywords')"
          editable
          clearable
        >
          <template #editable>
            <Button
              type="text"
              icon="filter"
              :icon-color="isFiltered(group.tag) ? 'var(--primary-color)' : ''"
            />
          </template>
        </Input>
        <Button
          v-tips="'home.overview.delayTest'"
          :loading="isLoading(group.tag)"
          icon="speedTest"
          type="text"
          @click="handleDelay(group.tag)"
        />
        <Button type="text" @click="toggleExpanded(group.tag)">
          <Icon
            :class="{ 'action-expand-expanded': isExpanded(group.tag) }"
            class="action-expand origin-center duration-200"
            icon="arrowDown"
          />
        </Button>
      </div>
    </div>
    <Transition name="expand">
      <div v-if="isExpanded(group.tag)" class="py-8 px-4">
        <Empty v-if="group.items.length === 0" />
        <div
          v-else-if="appSettings.app.kernel.cardMode"
          :class="`grid-cols-${appSettings.app.kernel.cardColumns}`"
          class="grid gap-8"
        >
          <Card
            v-for="proxy in group.items"
            :key="proxy.tag"
            :title="proxy.tag"
            :selected="proxy.tag === group.selected"
            :class="group.selectable ? 'cursor-pointer' : ''"
            @click="useProxyWithCatchError(group, proxy)"
          >
            <Button
              :style="{ color: delayColor(proxy.urlTestDelay) }"
              :loading="isLoading(proxy.tag)"
              type="text"
              size="small"
              style="margin-left: -2px; padding-left: 2px"
              @click.stop="handleDelay(proxy.tag)"
            >
              <div class="text-12">
                {{ proxy.urlTestDelay && proxy.urlTestDelay + 'ms' }}
              </div>
            </Button>
            <div class="text-12 my-2">{{ proxy.type }}</div>
          </Card>
        </div>
        <div v-else class="grid grid-cols-32 gap-8">
          <div
            v-for="proxy in group.items"
            :key="proxy.tag"
            v-tips.fast="proxy.tag"
            :style="{ background: delayColor(proxy.urlTestDelay) }"
            :class="proxy.tag === group.selected ? 'rounded-full shadow' : ''"
            class="w-12 h-12 rounded-4 flex items-center justify-center"
            @click="useProxyWithCatchError(group, proxy)"
          >
            <Icon v-if="isLoading(proxy.tag)" icon="loading" :size="12" class="rotation" />
          </div>
        </div>
      </div>
    </Transition>
  </div>

  <Modal
    v-model:open="showMoreSettings"
    :submit="false"
    mask-closable
    cancel-text="common.close"
    title="common.more"
  >
    <template #action>
      <Button type="text" class="mr-auto" @click="handleResetMoreSettings">
        {{ t('common.reset') }}
      </Button>
    </template>

    <div class="form-item">
      {{ t('home.controller.closeMode.name') }}
      <Radio
        v-model="appSettings.app.kernel.controllerCloseMode"
        :options="ControllerCloseModeOptions"
      />
    </div>

    <div
      v-if="appSettings.app.kernel.controllerCloseMode === ControllerCloseMode.All"
      class="form-item"
    >
      {{ t('home.controller.sensitivity') }}
      <Input
        v-model="appSettings.app.kernel.controllerSensitivity"
        type="number"
        :min="1"
        :max="6"
        placeholder="1-6"
        editable
      />
    </div>

    <div class="form-item">
      {{ t('home.controller.cardColumns') }}
      <Radio
        v-model="appSettings.app.kernel.cardColumns"
        :options="Array.from({ length: 5 }, (_, i) => ({ label: String(i + 1), value: i + 1 }))"
      />
    </div>
  </Modal>
</template>

<style lang="less" scoped>
.expand-enter-active,
.expand-leave-active {
  transform-origin: top;
  transition:
    transform 0.2s ease-in-out,
    opacity 0.2s ease-in-out;
}

.expand-enter-from,
.expand-leave-to {
  transform: scaleY(0);
}

.action-expand {
  transform: rotate(-90deg);
  &-expanded {
    transform: rotate(0deg);
  }
}
</style>
