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
const result = ref('')
const testURL = ref('https://www.gstatic.com/generate_204')
const proxyOptions = [
  { label: 'settings.requestProxy.none', value: 'none' },
  { label: 'settings.requestProxy.environment', value: 'environment' },
  { label: 'settings.requestProxy.core', value: 'core' },
  { label: 'settings.requestProxy.custom', value: 'custom' },
]
const guardBusy = (event: Event) => {
  if (!busy.value) return
  event.preventDefault()
  event.stopImmediatePropagation()
}
const persist = async () => {
  Object.assign(appConfig.config, draft)
  await appConfig.saveNow()
  Object.assign(draft, current())
  saved.value = JSON.stringify(current())
}
const save = async () => {
  if (busy.value) return
  busy.value = true
  error.value = ''
  try {
    await persist()
    message.success('common.success')
  } catch (value) {
    error.value = value instanceof Error ? value.message : String(value)
  } finally {
    busy.value = false
  }
}
const test = async () => {
  if (busy.value) return
  busy.value = true
  testing.value = true
  error.value = ''
  result.value = ''
  try {
    await persist()
    const response = await appConfig.testNetworkRequest(testURL.value)
    result.value = t('settings.requestProxy.testSuccess', {
      status: response.statusCode,
      duration: String(response.durationMs),
    })
  } catch (value) {
    error.value = value instanceof Error ? value.message : String(value)
  } finally {
    busy.value = false
    testing.value = false
  }
}
</script>

<template>
  <div class="px-8 py-12 text-18 font-bold">{{ t('settings.network') }}</div>
  <Card>
    <div class="network-settings">
      <div class="network-field">
        <span>{{ t('settings.requestProxy.name') }}</span>
        <div
          :inert="busy || undefined"
          :aria-disabled="busy || undefined"
          @click.capture="guardBusy"
          @keydown.capture="guardBusy"
        >
          <Select
            v-model="draft.requestProxyMode"
            :options="proxyOptions"
            :aria-label="t('settings.requestProxy.name')"
          />
        </div>
      </div>
      <p v-if="draft.requestProxyMode === 'environment'" class="network-hint">
        {{ t('settings.requestProxy.environmentHint') }}
      </p>
      <p v-if="draft.requestProxyMode === 'core'" class="network-hint">
        {{ t('settings.requestProxy.coreHint') }}
      </p>
      <div v-if="draft.requestProxyMode === 'custom'" class="network-field">
        <span>{{ t('settings.requestProxy.custom') }}</span>
        <Input
          v-model="draft.customProxy"
          :disabled="busy"
          placeholder="http(s)://host:port / socks5://host:port"
          :aria-label="t('settings.requestProxy.custom')"
        />
      </div>
      <div class="network-field">
        <span>{{ t('settings.userAgent.name') }}</span>
        <Input
          v-model="draft.userAgent"
          :disabled="busy"
          :placeholder="APP_TITLE + '/' + appStore.currentVersion"
          :aria-label="t('settings.userAgent.name')"
        />
      </div>
      <div class="network-field">
        <span>{{ t('settings.githubapi.name') }}</span>
        <input
          v-model="draft.githubApiToken"
          class="network-secret"
          type="password"
          autocomplete="off"
          :disabled="busy"
          :aria-label="t('settings.githubapi.name')"
        />
      </div>
      <div class="network-field">
        <span>{{ t('settings.githubDownloadAcceleration.name') }}</span>
        <Input
          v-model="draft.githubDownloadMirror"
          :disabled="busy"
          placeholder="https://example.com/{url}"
          :aria-label="t('settings.githubDownloadAcceleration.mirror')"
        />
      </div>
      <p class="network-hint">
        {{ t('settings.githubDownloadAcceleration.tips', { url: '{url}' }) }}
      </p>
      <p class="network-hint">{{ t('settings.requestProxy.saveHint') }}</p>
      <div class="network-field">
        <span>{{ t('settings.requestProxy.testURL') }}</span>
        <Input
          v-model="testURL"
          :disabled="busy"
          :aria-label="t('settings.requestProxy.testURL')"
        />
      </div>
      <div class="network-actions">
        <Button :loading="testing" :disabled="busy && !testing" @click="test">{{
          t('settings.requestProxy.test')
        }}</Button>
        <Button
          type="primary"
          :loading="busy && !testing"
          :disabled="!dirty || testing"
          @click="save"
          >{{ t('common.save') }}</Button
        >
      </div>
      <p v-if="error" role="alert" class="network-result">{{ error }}</p>
      <p v-if="result" role="status" class="network-result">{{ result }}</p>
    </div>
  </Card>
</template>

<style scoped>
.network-settings {
  padding: 12px 8px;
}
.network-field {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  margin: 12px 0;
}
.network-field > span {
  flex-shrink: 0;
  font-weight: 600;
}
.network-field > :last-child {
  width: min(65%, 480px);
  min-width: 0;
}
.network-hint {
  font-size: 12px;
  opacity: 0.7;
  margin: 8px 0;
  overflow-wrap: anywhere;
}
.network-secret {
  padding: 8px 12px;
  color: inherit;
  background: transparent;
  border: 1px solid var(--border-color, #8886);
  border-radius: 6px;
}
.network-actions {
  display: flex;
  justify-content: flex-end;
  flex-wrap: wrap;
  gap: 8px;
  margin-top: 16px;
}
.network-result {
  overflow-wrap: anywhere;
  margin-top: 12px;
}
@media (max-width: 650px) {
  .network-field {
    align-items: stretch;
    flex-direction: column;
    gap: 6px;
  }
  .network-field > :last-child {
    width: 100%;
  }
}
</style>
