<script setup lang="ts">
import { ref, useTemplateRef } from 'vue'
import { useI18n } from 'vue-i18n'
import { parse, stringify } from 'yaml'

import { message } from '@/utils'

const model = defineModel<{ mixin: IProfile['mixin']; script: IProfile['script'] }>({
  required: true,
})

const { t } = useI18n()

const activeTab = ref('mixin')
const mixinEditor = useTemplateRef<{ getContent(): string; flush(): void }>('mixinEditor')
const scriptEditor = useTemplateRef<{ getContent(): string; flush(): void }>('scriptEditor')
const flush = () => {
  mixinEditor.value?.flush()
  scriptEditor.value?.flush()
  if (mixinEditor.value) model.value.mixin.config = mixinEditor.value.getContent()
  if (scriptEditor.value) model.value.script.code = scriptEditor.value.getContent()
}
defineExpose({ flush })

const tabItems = [
  { key: 'mixin', tab: 'profile.mixinSettings.name' },
  { key: 'script', tab: 'profile.scriptSettings.name' },
]

const MixinPriorityOptions = [
  { label: 'profile.mixinSettings.mixin', value: 'mixin' },
  { label: 'profile.mixinSettings.gui', value: 'gui' },
]

const MixinFormatOptions = [
  { label: 'JSON', value: 'json' },
  { label: 'YAML', value: 'yaml' },
]

const onFormatChange = (val: 'json' | 'yaml', old: 'json' | 'yaml') => {
  try {
    flush()
    const config = parse(model.value.mixin.config)
    if (config) {
      if (val === 'json') {
        model.value.mixin.config = JSON.stringify(config, null, 2)
      } else {
        model.value.mixin.config = stringify(config)
      }
    }
  } catch (e: any) {
    model.value.mixin.format = old
    message.error(e.message || e)
  }
}
</script>

<template>
  <Tabs v-model:active-key="activeTab" :items="tabItems">
    <template #mixin>
      <div class="form-item">
        {{ t('profile.mixinSettings.priority') }}
        <Radio v-model="model.mixin.priority" :options="MixinPriorityOptions" />
      </div>
      <div class="form-item">
        {{ t('profile.mixinSettings.format') }}
        <Radio
          v-model="model.mixin.format"
          :options="MixinFormatOptions"
          @change="onFormatChange"
        />
      </div>
      <CodeViewer
        ref="mixinEditor"
        v-model="model.mixin.config"
        :lang="model.mixin.format"
        editable
      />
    </template>
    <template #script>
      <CodeViewer ref="scriptEditor" v-model="model.script.code" lang="javascript" editable />
    </template>
  </Tabs>
</template>
