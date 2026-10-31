<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { usePluginsStore } from '@/stores/plugins'
import { useSubscribesStore } from '@/stores/subscribes'
import type { PluginRunResult } from '@/types/plugin'

const props = defineProps<{ id: string }>()
const { t } = useI18n()
const store = usePluginsStore()
const diagnostic = computed(() => store.getPluginById(props.id)?.manual_target === 'diagnostic')
const subscriptions = useSubscribesStore()
const subscriptionID = ref('')
const loading = ref(false)
const refreshing = ref(false)
const error = ref('')
const result = ref<PluginRunResult>()
const options = computed(() =>
  subscriptions.subscribes
    .filter((item) => item.proxies?.length)
    .map((item) => ({ label: `${item.name} (${item.proxies.length})`, value: item.id })),
)
onMounted(async () => {
  if (diagnostic.value) return
  refreshing.value = true
  try {
    await subscriptions.setupSubscribes()
  } catch (value) {
    error.value = value instanceof Error ? value.message : String(value)
  } finally {
    refreshing.value = false
  }
})
const run = async () => {
  if ((!diagnostic.value && !subscriptionID.value) || loading.value) return
  loading.value = true
  error.value = ''
  result.value = undefined
  try {
    result.value = await store.runPlugin(
      props.id,
      diagnostic.value ? undefined : subscriptionID.value,
    )
    if (!diagnostic.value) await subscriptions.setupSubscribes()
  } catch (value) {
    error.value = value instanceof Error ? value.message : String(value)
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div class="plugin-run">
    <p>{{ t(diagnostic ? 'plugins.diagnosticHint' : 'plugins.runHint') }}</p>
    <div class="plugin-run-controls">
      <Select
        v-if="!diagnostic"
        v-model="subscriptionID"
        :options="options"
        :placeholder="t('plugins.chooseSubscription')"
        :aria-label="t('plugins.chooseSubscription')"
        class="plugin-run-select"
      />
      <Button
        type="primary"
        :loading="loading"
        :disabled="
          refreshing || (!diagnostic && !options.some((item) => item.value === subscriptionID))
        "
        @click="run"
        >{{ t('common.run') }}</Button
      >
    </div>
    <p v-if="!diagnostic && !options.length && !refreshing">
      {{ t('plugins.noCachedSubscriptions') }}
    </p>
    <p v-if="error" role="alert">{{ error }}</p>
    <div v-if="result" role="status">
      <p v-if="!diagnostic">
        {{ t('plugins.runResult', { before: result.before, after: result.after }) }}
      </p>
      <pre v-if="diagnostic && result.report !== undefined">{{
        JSON.stringify(result.report, null, 2)
      }}</pre>
      <pre v-if="result.logs?.length">{{ result.logs.join('\n') }}</pre>
    </div>
  </div>
</template>

<style scoped lang="less">
.plugin-run {
  min-width: 0;
  overflow-wrap: anywhere;
}
.plugin-run p {
  margin: 8px 0;
}
.plugin-run-controls {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
  margin: 16px 0;
}
.plugin-run-select {
  flex: 1;
  min-width: 0;
}
.plugin-run pre {
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  max-height: 40vh;
  overflow: auto;
  padding: 12px;
  background: var(--card-bg);
  border-radius: 8px;
}
</style>
