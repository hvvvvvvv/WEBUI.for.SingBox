<script setup lang="ts">
import { onActivated, ref } from 'vue'
import { useI18n } from 'vue-i18n'

import { DraggableOptions, ViewOptions } from '@/constant/app'
import { View } from '@/enums/app'
import { useAppSettingsStore } from '@/stores/appSettings'
import { PluginApiError, usePluginsStore } from '@/stores/plugins'
import { deepClone } from '@/utils/others'
import { confirmDelete, message } from '@/utils'
import { useModal } from '@/components/Modal'
import type { Plugin } from '@/types/plugin'
import type { Menu } from '@/types/app'
import PluginEditor from './components/PluginEditor.vue'
import PluginHub from './components/PluginHub.vue'
import PluginRun from './components/PluginRun.vue'

const { t } = useI18n()
const store = usePluginsStore()
const settings = useAppSettingsStore()
const [Modal, modal] = useModal({})
const error = ref('')
const showError = (value: unknown) => {
  const detail =
    value instanceof PluginApiError && value.status === 409
      ? t('common.operationConflict')
      : value instanceof Error
        ? value.message
        : String(value)
  message.error(detail)
}
const refresh = async () => {
  error.value = ''
  try {
    await store.setupPlugins()
  } catch (value) {
    error.value = value instanceof Error ? value.message : String(value)
  }
}
onActivated(() => {
  void refresh()
})
const openHub = () => {
  modal.setProps({
    title: 'plugins.hub',
    width: '85',
    height: '85',
    minWidth: '0',
    submit: false,
    cancelText: 'common.close',
    maskClosable: true,
  })
  modal.setContent(PluginHub).open()
}
const openEditor = (id = '', mode: 'source' | 'metadata' | 'configuration' = 'metadata') => {
  const title =
    mode === 'source'
      ? store.getPluginById(id)?.name
      : mode === 'configuration'
        ? 'plugins.configuration'
        : id
          ? 'common.edit'
          : 'common.add'
  modal.setProps({ title, width: '85', height: '88', minWidth: '0', bodyScrollable: true })
  modal.setContent(PluginEditor, { id, mode }).open()
}
const checkUpdates = async () => {
  try {
    await store.loadCatalog(true)
    if (store.catalog.warning) {
      message.warn(store.catalog.warning)
    } else if (store.catalog.source === 'empty') {
      message.warn('plugins.catalogUnavailable')
    } else {
      message.success('plugins.updateSuccess')
    }
  } catch (value) {
    showError(value)
  }
}
const update = async (plugin: Plugin) => {
  try {
    await store.updatePlugin(plugin.id)
    message.success('common.success')
  } catch (value) {
    showError(value)
  }
}
const toggle = async (plugin: Plugin) => {
  try {
    await store.savePlugin({ ...deepClone(plugin), disabled: !plugin.disabled })
  } catch (value) {
    showError(value)
  }
}
const remove = async (plugin: Plugin) => {
  if (!(await confirmDelete())) return
  try {
    await store.deletePlugin(plugin.id)
  } catch (value) {
    showError(value)
  }
}
const run = (plugin: Plugin) => {
  modal.setProps({
    title: plugin.name,
    width: '70',
    minWidth: '0',
    submit: false,
    cancelText: 'common.close',
    maskClosable: false,
  })
  modal.setContent(PluginRun, { id: plugin.id }).open()
}
const move = async (plugin: Plugin, direction: -1 | 1) => {
  const ids = store.plugins.map((item) => item.id)
  const index = ids.indexOf(plugin.id)
  const target = index + direction
  if (index < 0 || target < 0 || target >= ids.length) return
  ;[ids[index], ids[target]] = [ids[target]!, ids[index]!]
  try {
    await store.reorderPlugins(ids)
  } catch (value) {
    showError(value)
  }
}
const menus = (plugin: Plugin): Menu[] => [
  { label: 'plugins.source', handler: () => openEditor(plugin.id, 'source') },
  { label: 'plugins.configuration', handler: () => openEditor(plugin.id, 'configuration') },
  { label: plugin.disabled ? 'common.enable' : 'common.disable', handler: () => toggle(plugin) },
  { label: 'common.edit', handler: () => openEditor(plugin.id) },
  { label: 'plugins.moveUp', handler: () => move(plugin, -1) },
  { label: 'plugins.moveDown', handler: () => move(plugin, 1) },
  { label: 'common.delete', handler: () => remove(plugin) },
]
const menuAction = (close: () => void, action: () => unknown) => {
  close()
  void action()
}
let sortRevision = 0
let sortStartIDs: string[] = []
const sortStart = () => {
  sortRevision = store.revision
  sortStartIDs = store.plugins.map((item) => item.id)
}
const sortEnd = () => {
  void store
    .reorderPlugins(
      store.plugins.map((item) => item.id),
      sortRevision,
      sortStartIDs,
    )
    .catch(showError)
}
</script>

<template>
  <div class="plugins-page">
    <div class="grid-list-header plugins-header">
      <Radio v-model="settings.app.pluginsView" :options="ViewOptions" class="mr-auto" />
      <Button type="link" @click="openHub">{{ t('plugins.hub') }}</Button>
      <Button type="link" :loading="store.catalogLoading" @click="checkUpdates">{{
        t('plugins.checkForUpdates')
      }}</Button>
      <Button type="primary" icon="add" @click="openEditor()">{{ t('common.add') }}</Button>
    </div>
    <p v-if="error" role="alert" class="plugins-note">
      {{ error }} <Button size="small" @click="refresh">{{ t('plugins.retry') }}</Button>
    </p>
    <p v-if="store.catalog.warning" role="status" class="plugins-note">
      {{ store.catalog.warning }}
    </p>
    <div v-if="!store.plugins.length && !store.loading" class="plugins-empty">
      <Empty
        ><template #description>
          <p>{{ t('plugins.empty') }}</p>
          <Button type="link" @click="openHub">{{ t('plugins.hub') }}</Button>
          <Button type="link" @click="openEditor()">{{ t('common.add') }}</Button>
        </template></Empty
      >
    </div>
    <div
      v-else
      v-draggable="[
        store.plugins,
        { ...DraggableOptions, disabled: store.busy, onStart: sortStart, onUpdate: sortEnd },
      ]"
      :class="'grid-list-' + settings.app.pluginsView"
      class="plugins-cards"
    >
      <Card
        v-for="(plugin, index) in store.plugins"
        :key="plugin.id"
        v-menu="menus(plugin)"
        :title="plugin.name"
        :disabled="plugin.disabled"
        class="grid-list-item"
      >
        <template #title-prefix
          ><Tag v-if="store.availableUpdate(plugin)" size="small" color="cyan">{{
            t('plugins.newVersion')
          }}</Tag></template
        >
        <template #extra>
          <Dropdown :trigger="['click', 'hover']">
            <Button type="link" size="small" icon="more" :aria-label="t('common.more')" />
            <template #overlay="{ close }">
              <div class="plugin-menu">
                <Button
                  type="text"
                  :disabled="
                    store.busy || !store.catalog.plugins.some((item) => item.id === plugin.id)
                  "
                  @click="menuAction(close, () => update(plugin))"
                  >{{ t('common.update') }}</Button
                >
                <Button
                  type="text"
                  :disabled="store.busy"
                  @click="menuAction(close, () => toggle(plugin))"
                  >{{ plugin.disabled ? t('common.enable') : t('common.disable') }}</Button
                >
                <Button type="text" @click="menuAction(close, () => openEditor(plugin.id))">{{
                  t('common.edit')
                }}</Button>
                <Button
                  type="text"
                  @click="menuAction(close, () => openEditor(plugin.id, 'configuration'))"
                  >{{ t('plugins.configuration') }}</Button
                >
                <Button
                  type="text"
                  :disabled="store.busy || index === 0"
                  @click="menuAction(close, () => move(plugin, -1))"
                  >{{ t('plugins.moveUp') }}</Button
                >
                <Button
                  type="text"
                  :disabled="store.busy || index === store.plugins.length - 1"
                  @click="menuAction(close, () => move(plugin, 1))"
                  >{{ t('plugins.moveDown') }}</Button
                >
                <Button
                  type="text"
                  :disabled="store.busy"
                  @click="menuAction(close, () => remove(plugin))"
                  >{{ t('common.delete') }}</Button
                >
              </div>
            </template>
          </Dropdown>
        </template>
        <div class="plugin-triggers">
          <span>{{ t('plugin.trigger') }}:</span>
          <Tag v-if="!plugin.triggers.length" size="small" color="red">{{ t('common.none') }}</Tag>
          <Tag v-for="trigger in plugin.triggers" :key="trigger" size="small">{{
            t('plugin.' + trigger)
          }}</Tag>
        </div>
        <div
          v-tips="plugin.description"
          :class="{ 'line-clamp-1': settings.app.pluginsView === View.Grid }"
          class="plugin-description"
        >
          {{ t('plugin.description') }}: {{ plugin.description || '--' }}
        </div>
        <div class="plugin-card-actions">
          <Button type="link" size="small" @click="openEditor(plugin.id, 'source')">{{
            t('plugins.source')
          }}</Button>
          <Button
            v-if="plugin.triggers.includes('on::manual') && !plugin.disabled"
            type="primary"
            size="small"
            :disabled="store.busy"
            class="ml-auto"
            @click="run(plugin)"
            >{{ t('common.run') }}</Button
          >
        </div>
      </Card>
    </div>
    <p class="plugins-note">{{ t('plugins.runtimeHint') }}</p>
    <Modal />
  </div>
</template>

<style scoped lang="less">
.plugins-page {
  display: flex;
  flex-direction: column;
  min-height: 0;
  height: 100%;
  min-width: 0;
}
.plugins-header {
  flex-wrap: wrap;
  gap: 8px;
}
.plugins-cards {
  min-height: 0;
}
.plugins-empty {
  flex: 1;
  display: grid;
  place-items: center;
}
.plugins-note {
  font-size: 12px;
  opacity: 0.7;
  margin: 8px;
  overflow-wrap: anywhere;
}
.plugin-triggers,
.plugin-card-actions {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 4px;
}
.plugin-description {
  margin: 8px 0;
  overflow-wrap: anywhere;
}
.plugin-menu {
  display: flex;
  flex-direction: column;
  gap: 4px;
  min-width: 96px;
  padding: 4px;
}
.plugins-cards.grid-list-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 16px;
  padding: 8px;
  align-content: start;
}
.plugins-cards.grid-list-grid > .grid-list-item {
  width: auto;
  min-width: 0;
  margin: 0;
}
@media (max-width: 1000px) {
  .plugins-cards.grid-list-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}
@media (max-width: 650px) {
  .plugins-cards.grid-list-grid {
    grid-template-columns: minmax(0, 1fr);
  }
  .plugins-header {
    padding: 0;
  }
}
</style>
