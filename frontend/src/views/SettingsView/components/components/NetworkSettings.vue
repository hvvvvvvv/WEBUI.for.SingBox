<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

import { useAppConfigStore } from '@/stores/appConfig'
import { useAppStore } from '@/stores/app'
import { APP_TITLE, message } from '@/utils'

const { t } = useI18n()
const appConfig = useAppConfigStore()
const appStore = useAppStore()
const current = () => ({
  requestProxyMode: appConfig.config.requestProxyMode,
  customProxy: appConfig.config.customProxy,
  userAgent: appConfig.config.userAgent,
  githubApiToken: appConfig.config.githubApiToken,
  githubDownloadMirror: appConfig.config.githubDownloadMirror,
  githubDownloadAccelerationDisabled: appConfig.config.githubDownloadAccelerationDisabled,
})
const draft = reactive(current())
const saved = ref(JSON.stringify(current()))
const dirty = computed(() => JSON.stringify(draft) !== saved.value)
const busy = ref(false)
watch(
  () => JSON.stringify(current()),
  (value) => {
    if (busy.value) return
    if (!dirty.value) Object.assign(draft, current())
    saved.value = value
  },
  { flush: 'sync' },
)
const testing = ref(false)
const error = ref('')
const mirrorEnabled = computed({
  get: () => !draft.githubDownloadAccelerationDisabled,
  set: (enabled: boolean) => {
    draft.githubDownloadAccelerationDisabled = !enabled
  },
})
const proxyOptions = [
  { label: 'settings.requestProxy.none', value: 'none' },
  { label: 'settings.requestProxy.environment', value: 'environment' },
  { label: 'settings.requestProxy.core', value: 'core' },
  { label: 'settings.requestProxy.custom', value: 'custom' },
]
const persist = async () => {
  Object.assign(appConfig.config, draft)
  await appConfig.saveNow()
  Object.assign(draft, current())
  saved.value = JSON.stringify(current())
}
const save = async () => {
  if (busy.value || !dirty.value) return
  busy.value = true
  error.value = ''
  try {
    await persist()
  } catch (value) {
    error.value = value instanceof Error ? value.message : String(value)
  } finally {
    busy.value = false
  }
}
const reset = (field: 'customProxy' | 'userAgent' | 'githubApiToken' | 'githubDownloadMirror') => {
  if (busy.value) return
  draft[field] = ''
  void save()
}
const test = async () => {
  if (busy.value) return
  busy.value = true
  testing.value = true
  error.value = ''
  try {
    await persist()
    const response = await appConfig.testNetworkRequest('https://www.gstatic.com/generate_204')
    message.success(t('settings.requestProxy.testSuccess', {
      status: response.statusCode,
      duration: String(response.durationMs),
    }))
  } catch (value) {
    const detail = value instanceof Error ? value.message : String(value)
    message.error(detail)
    if (dirty.value) error.value = detail
  } finally {
    busy.value = false
    testing.value = false
  }
}
</script>

<template>
  <div class="px-8 py-12 text-18 font-bold">{{ t('settings.network') }}</div>
  <Card class="network-settings">
    <div class="network-field px-8 py-12 flex items-center justify-between">
      <div class="text-16 font-bold">{{ t('settings.requestProxy.name') }}</div>
      <div class="network-proxy-controls flex items-center gap-4">
        <Button
          v-if="draft.requestProxyMode !== 'none'"
          :loading="testing"
          :disabled="busy && !testing"
          type="primary"
          size="small"
          @click="test"
          >{{ t('settings.requestProxy.test') }}</Button
        >
        <Radio
          v-model="draft.requestProxyMode"
          :options="proxyOptions"
          :disabled="busy"
          :aria-label="t('settings.requestProxy.name')"
          @change="save"
        />
      </div>
    </div>
    <div
      v-if="draft.requestProxyMode === 'custom'"
      class="network-field px-8 py-12 flex items-center justify-between"
    >
      <div class="text-16 font-bold">{{ t('settings.requestProxy.custom') }}</div>
      <Input
        v-model="draft.customProxy"
        :disabled="busy"
        placeholder="scheme://username:password@host:port"
        :aria-label="t('settings.requestProxy.custom')"
        :input-attrs="{ 'aria-label': t('settings.requestProxy.custom') }"
        editable
        clearable
        class="text-14"
        @submit="save"
      >
        <template #suffix>
          <Button
            :aria-label="t('settings.requestProxy.reset')"
            :disabled="busy"
            type="text"
            size="small"
            icon="reset"
            @click="reset('customProxy')"
          />
        </template>
      </Input>
    </div>
    <div class="network-field px-8 py-12 flex items-center justify-between">
      <div class="text-16 font-bold">
        {{ t('settings.userAgent.name') }}
        <span class="font-normal text-12">({{ t('settings.userAgent.tips') }})</span>
      </div>
      <Input
        v-model="draft.userAgent"
        :disabled="busy"
        :placeholder="APP_TITLE + '/' + appStore.currentVersion"
        :aria-label="t('settings.userAgent.name')"
        :input-attrs="{ 'aria-label': t('settings.userAgent.name') }"
        editable
        class="text-14"
        @submit="save"
      >
        <template #suffix>
          <Button
            :aria-label="t('settings.userAgent.reset')"
            :disabled="busy"
            type="text"
            size="small"
            icon="reset"
            @click="reset('userAgent')"
          />
        </template>
      </Input>
    </div>
    <div class="network-field px-8 py-12 flex items-center justify-between">
      <div class="text-16 font-bold">
        {{ t('settings.githubapi.name') }}
        <span class="font-normal text-12">({{ t('settings.githubapi.tips') }})</span>
      </div>
      <Input
        v-model="draft.githubApiToken"
        type="password"
        :disabled="busy"
        :aria-label="t('settings.githubapi.name')"
        :input-attrs="{ 'aria-label': t('settings.githubapi.name'), autocomplete: 'off' }"
        editable
        class="text-14"
        @submit="save"
      >
        <template #editable="{ value }">{{ value ? '••••••••' : t('common.none') }}</template>
        <template #suffix>
          <Button
            :aria-label="t('settings.githubapi.reset')"
            :disabled="busy"
            type="text"
            size="small"
            icon="reset"
            @click="reset('githubApiToken')"
          />
        </template>
      </Input>
    </div>
    <div class="network-field px-8 py-12 flex items-center justify-between">
      <div class="text-16 font-bold">
        {{ t('settings.githubDownloadAcceleration.name') }}
        <span class="font-normal text-12"
          >({{ t('settings.githubDownloadAcceleration.tips') }})</span
        >
      </div>
      <Switch
        v-model="mirrorEnabled"
        :disabled="busy"
        :aria-label="t('settings.githubDownloadAcceleration.name')"
        @change="save"
      />
    </div>
    <div v-if="mirrorEnabled" class="network-field px-8 py-12 flex items-center justify-between">
      <div class="text-16 font-bold">{{ t('settings.githubDownloadAcceleration.mirror') }}</div>
      <Input
        v-model="draft.githubDownloadMirror"
        :disabled="busy"
        placeholder="https://example.com/{url}"
        :aria-label="t('settings.githubDownloadAcceleration.mirror')"
        :input-attrs="{ 'aria-label': t('settings.githubDownloadAcceleration.mirror') }"
        editable
        clearable
        class="text-14"
        @submit="save"
      >
        <template #suffix>
          <Button
            :aria-label="t('settings.githubDownloadAcceleration.reset')"
            :disabled="busy"
            type="text"
            size="small"
            icon="reset"
            @click="reset('githubDownloadMirror')"
          />
        </template>
      </Input>
    </div>
    <div v-if="error" class="network-result px-8 py-12">
      <div v-if="error" role="alert">{{ error }}</div>
      <Button v-if="error && dirty" :loading="busy" type="primary" size="small" @click="save">{{
        t('common.save')
      }}</Button>
    </div>
  </Card>
</template>

<style scoped>
.network-field {
  gap: 12px;
  flex-wrap: wrap;
}
.network-field > .text-16 {
  min-width: 0;
}
.network-field > .gui-input {
  max-width: min(100%, 400px);
}
.network-proxy-controls {
  max-width: 100%;
  margin-left: auto;
}
.network-result {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  overflow-wrap: anywhere;
}
.network-result > div {
  min-width: 0;
}
@media (max-width: 650px) {
  .network-field > .text-16 {
    width: 100%;
  }
  .network-field > .gui-input {
    margin-left: auto;
  }
  .network-proxy-controls {
    flex-wrap: wrap;
    justify-content: flex-end;
  }
  .network-proxy-controls :deep(.gui-radio) {
    max-width: 100%;
    flex-wrap: wrap;
  }
  .network-proxy-controls :deep(.gui-radio-button) {
    flex: 1 0 40%;
    min-height: 44px;
  }
  .network-proxy-controls :deep(.gui-button) {
    min-height: 44px;
  }
  .network-field :deep(.gui-input.is-editable) {
    min-height: 44px;
  }
}
</style>
