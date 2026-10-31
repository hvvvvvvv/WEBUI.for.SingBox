<script setup lang="ts">
import { computed, h, inject, ref, useTemplateRef, watch } from 'vue'
import { useI18n } from 'vue-i18n'

import { PluginApiError, usePluginsStore } from '@/stores/plugins'
import { deepClone, sampleID } from '@/utils/others'
import { message } from '@/utils'
import type { Plugin } from '@/types/plugin'
import Button from '@/components/Button/index.vue'
import ResourceConflictNotice from '@/components/ResourceConflictNotice/index.vue'
import PluginConfigItem from './PluginConfigItem.vue'

const props = withDefaults(
  defineProps<{ id?: string; mode?: 'source' | 'metadata' | 'configuration' }>(),
  { id: '', mode: 'metadata' },
)
const { t } = useI18n()
const store = usePluginsStore()
const cancel = inject<() => void>('cancel')!
const submit = inject<() => void>('submit')!
const loading = ref(false)
const reloading = ref(false)
const conflict = ref<'changed' | 'deleted' | null>(null)
const fileInput = useTemplateRef<HTMLInputElement>('fileInput')
const sourceEditor = useTemplateRef<{ getContent(): string }>('sourceEditor')
const configurationEditor = useTemplateRef<{ getContent(): string }>('configurationEditor')
const draft = ref<Plugin>(
  deepClone(
    store.getPluginById(props.id) || {
      id: sampleID(),
      name: '',
      description: '',
      version: '1.0.0',
      triggers: ['on::subscribe'],
      disabled: false,
      configuration: {},
      source: 'function onSubscribe(proxies, subscription) {\n  return proxies\n}\n',
    },
  ),
)
const configuration = ref(JSON.stringify(draft.value.configuration || {}, null, 2))
const configurationMode = ref<'form' | 'json'>(
  draft.value.configuration_schema?.length ? 'form' : 'json',
)
let baseRevision = store.revision
const allTriggerOptions = [
  { label: 'plugin.on::subscribe', value: 'on::subscribe' },
  { label: 'plugin.on::generate', value: 'on::generate' },
  { label: 'plugin.on::manual', value: 'on::manual' },
]
const triggerOptions = computed(() =>
  draft.value.manual_target === 'diagnostic'
    ? allTriggerOptions.filter((item) => item.value === 'on::manual')
    : allTriggerOptions,
)
watch(
  () => draft.value.manual_target,
  (target) => {
    if (target === 'diagnostic') draft.value.triggers = ['on::manual']
  },
)

const readConfiguration = (): Record<string, unknown> => {
  if (configurationMode.value === 'form') return deepClone(draft.value.configuration || {})
  configuration.value = configurationEditor.value?.getContent() ?? configuration.value
  let config: unknown
  try {
    config = JSON.parse(configuration.value || '{}')
  } catch {
    throw new Error(t('plugins.invalidConfiguration'))
  }
  if (!config || typeof config !== 'object' || Array.isArray(config))
    throw new Error(t('plugins.invalidConfiguration'))
  return config as Record<string, unknown>
}
const switchConfigurationMode = (mode: 'form' | 'json') => {
  if (mode === configurationMode.value) return
  try {
    draft.value.configuration = readConfiguration()
    configuration.value = JSON.stringify(draft.value.configuration, null, 2)
    configurationMode.value = mode
  } catch (error) {
    message.error(error instanceof Error ? error.message : String(error))
  }
}
const save = async (asCopy = false) => {
  loading.value = true
  try {
    // CodeMirror emits debounced updates; read its current document before saving.
    draft.value.source = sourceEditor.value?.getContent() ?? draft.value.source
    const plugin = { ...deepClone(draft.value), configuration: readConfiguration() }
    if (asCopy) {
      plugin.id = sampleID()
      plugin.name += ' (Copy)'
    }
    await store.savePlugin(plugin, asCopy ? store.revision : baseRevision)
    submit()
  } catch (error) {
    if (error instanceof PluginApiError && [409, 404].includes(error.status)) {
      conflict.value = props.id && !store.getPluginById(props.id) ? 'deleted' : 'changed'
    } else {
      message.error(error instanceof Error ? error.message : String(error))
    }
  } finally {
    loading.value = false
  }
}
const loadLatest = async () => {
  reloading.value = true
  try {
    await store.setupPlugins()
    const latest = store.getPluginById(props.id)
    if (!latest && props.id) {
      conflict.value = 'deleted'
      return
    }
    if (latest) {
      draft.value = deepClone(latest)
      configuration.value = JSON.stringify(latest.configuration || {}, null, 2)
      configurationMode.value = latest.configuration_schema?.length ? 'form' : 'json'
    }
    baseRevision = store.revision
    conflict.value = null
  } catch (error) {
    message.error(error instanceof Error ? error.message : String(error))
  } finally {
    reloading.value = false
  }
}
const importSource = async (event: Event) => {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = ''
  if (!file) return
  try {
    if (file.size > 256 * 1024) throw new Error(t('plugins.sourceTooLarge'))
    draft.value.source = await file.text()
    if (!draft.value.name) draft.value.name = file.name.replace(/\.js$/i, '')
  } catch (error) {
    message.error(error instanceof Error ? error.message : String(error))
  }
}
const exportSource = () => {
  draft.value.source = sourceEditor.value?.getContent() ?? draft.value.source
  const url = URL.createObjectURL(new Blob([draft.value.source], { type: 'text/javascript' }))
  const link = document.createElement('a')
  link.href = url
  link.download = `${draft.value.id}.js`
  link.click()
  setTimeout(() => URL.revokeObjectURL(url), 1000)
}
const modalSlots = {
  cancel: () => h(Button, { disabled: loading.value, onClick: cancel }, () => t('common.cancel')),
  submit: () =>
    h(
      Button,
      {
        type: 'primary',
        loading: loading.value,
        disabled: !!conflict.value || !draft.value.name.trim() || !draft.value.source.trim(),
        onClick: () => save(),
      },
      () => t('common.save'),
    ),
}
defineExpose({ modalSlots })
</script>

<template>
  <div class="plugin-editor">
    <ResourceConflictNotice
      v-if="conflict"
      :kind="conflict"
      :loading="reloading"
      @reload="loadLatest"
    />
    <div v-if="conflict" class="plugin-editor-conflict">
      <span>{{ t('plugins.draftPreserved') }}</span>
      <Button :loading="loading" @click="save(true)">{{ t('plugins.saveCopy') }}</Button>
    </div>
    <template v-if="mode === 'metadata'">
      <div class="plugin-field">
        <span>{{ t('plugin.name') }}</span
        ><Input v-model="draft.name" :aria-label="t('plugin.name')" />
      </div>
      <div class="plugin-field">
        <span>{{ t('plugin.version') }}</span
        ><Input v-model="draft.version" :aria-label="t('plugin.version')" />
      </div>
      <div class="plugin-field">
        <span>{{ t('plugin.description') }}</span
        ><Input v-model="draft.description" :aria-label="t('plugin.description')" />
      </div>
      <div class="plugin-field">
        <span>{{ t('plugin.trigger') }}</span
        ><Select
          v-model="draft.triggers"
          multiple
          :options="triggerOptions"
          :aria-label="t('plugin.trigger')"
        />
      </div>
      <div class="plugin-field">
        <span>{{ t('plugins.manualTarget') }}</span>
        <Radio
          :model-value="draft.manual_target || 'subscription'"
          :options="[
            { label: 'plugins.subscriptionTarget', value: 'subscription' },
            { label: 'plugins.diagnosticTarget', value: 'diagnostic' },
          ]"
          @update:model-value="
            (value) => (draft.manual_target = value as 'subscription' | 'diagnostic')
          "
        />
      </div>
      <div class="plugin-field">
        <span>{{ t('common.disable') }}</span
        ><Switch v-model="draft.disabled" :aria-label="t('common.disable')" />
      </div>
    </template>
    <template v-if="mode !== 'source'">
      <div class="plugin-source-toolbar">
        <strong>{{ t('plugins.configuration') }}</strong>
        <template v-if="draft.configuration_schema?.length">
          <Button
            :type="configurationMode === 'form' ? 'primary' : 'text'"
            size="small"
            @click="switchConfigurationMode('form')"
            >{{ t('plugins.formConfiguration') }}</Button
          >
          <Button
            :type="configurationMode === 'json' ? 'primary' : 'text'"
            size="small"
            @click="switchConfigurationMode('json')"
            >{{ t('plugins.advancedConfiguration') }}</Button
          >
        </template>
      </div>
      <PluginConfigItem
        v-if="configurationMode === 'form'"
        v-model="draft.configuration"
        :plugin="draft"
        :disabled="loading || reloading"
      />
      <CodeViewer
        v-else
        ref="configurationEditor"
        v-model="configuration"
        lang="json"
        editable
        class="plugin-config-editor"
      />
    </template>
    <template v-if="mode !== 'configuration'">
      <div class="plugin-source-toolbar">
        <strong>{{ t('plugins.source') }}</strong>
        <Button size="small" @click="fileInput?.click()">{{ t('plugins.importSource') }}</Button>
        <Button size="small" @click="exportSource">{{ t('plugins.exportSource') }}</Button>
        <input
          ref="fileInput"
          type="file"
          accept=".js,text/javascript,application/javascript"
          hidden
          @change="importSource"
        />
      </div>
      <p class="plugin-help">{{ t('plugins.sourceHelp') }}</p>
      <CodeViewer
        ref="sourceEditor"
        v-model="draft.source"
        lang="javascript"
        editable
        class="plugin-source-editor"
      />
    </template>
  </div>
</template>

<style scoped lang="less">
.plugin-editor {
  min-width: 0;
}
.plugin-field {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 6px 0;
}
.plugin-field > span {
  min-width: 0;
  overflow-wrap: anywhere;
}
.plugin-field > :last-child:not(.gui-switch) {
  min-width: 0;
  width: 65%;
}
.plugin-field > .gui-switch {
  min-width: 50px;
  flex-shrink: 0;
}
.plugin-source-toolbar,
.plugin-editor-conflict {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
  margin: 12px 0;
}
.plugin-source-toolbar strong {
  margin-right: auto;
}
.plugin-section-title {
  margin: 12px 0 8px;
}
.plugin-config-editor {
  height: 160px;
}
.plugin-source-editor {
  height: 48vh;
  min-height: 220px;
}
.plugin-help {
  opacity: 0.7;
  font-size: 12px;
  margin-bottom: 8px;
}
@media (max-width: 600px) {
  .plugin-field {
    flex-wrap: wrap;
  }
  .plugin-field > :last-child:not(.gui-switch) {
    width: 100%;
  }
}
</style>
