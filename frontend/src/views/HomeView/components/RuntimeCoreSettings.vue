<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

import { GetInterfaces } from '@/bridge'
import { getNativeVersion } from '@/api/kernel'
import { useAppStore, useKernelApiStore } from '@/stores'
import { TunStackOptions } from '@/constant/kernel'
import {
  applyRuntimeCoreSettings,
  canSelectTunStack,
  runtimeAllowsLAN,
  runtimeCoreSettingsDraft,
  setRuntimeAllowLAN,
  type RuntimeCoreSettingsDraft,
} from '@/utils/runtimeCoreSettings'

const { t } = useI18n()
const app = useAppStore()
const kernel = useKernelApiStore()
const key = 'home.overview.runtimeSettings.'
const snapshot = ref<Awaited<ReturnType<typeof kernel.loadRuntimeSettings>>>()
const draft = ref<RuntimeCoreSettingsDraft>()
const loading = ref(false)
const applying = ref(false)
const error = ref('')
const saved = ref(false)
const version = ref('')
const interfaces = ref<string[]>([])
const interfacesUnavailable = ref(false)
const stackEditable = computed(() => canSelectTunStack(version.value))
const busy = computed(() => loading.value || applying.value || kernel.restarting)
const interfaceOptions = computed(() => [
  { label: key + 'systemRoute', value: 'system' },
  { label: key + 'autoInterface', value: 'auto' },
  ...Array.from(new Set([...interfaces.value, draft.value?.outboundInterface || '']))
    .filter(Boolean)
    .map((name) => ({ label: name, value: `interface:${name}` })),
])
const interfaceChoice = computed({
  get: () =>
    draft.value?.outboundMode === 'interface'
      ? `interface:${draft.value.outboundInterface}`
      : draft.value?.outboundMode || 'system',
  set: (value: string) => {
    if (!draft.value) return
    if (value === 'auto' || value === 'system') draft.value.outboundMode = value
    else if (value.startsWith('interface:')) {
      draft.value.outboundMode = 'interface'
      draft.value.outboundInterface = value.slice('interface:'.length)
    }
  },
})
let disposed = false
let loadSequence = 0
let loadController: AbortController | undefined
const errorText = (cause: unknown) => t(cause instanceof Error ? cause.message : String(cause))

const reload = async (keepError = false) => {
  const sequence = ++loadSequence
  loadController?.abort()
  const controller = new AbortController()
  loadController = controller
  loading.value = true
  if (!keepError) {
    error.value = ''
    saved.value = false
  }
  snapshot.value = undefined
  try {
    const [current, core, network] = await Promise.all([
      kernel.loadRuntimeSettings({ signal: controller.signal }),
      getNativeVersion().catch(() => undefined),
      GetInterfaces({ signal: controller.signal, timeoutMs: 10_000 }).catch(() => undefined),
    ])
    if (disposed || sequence !== loadSequence) return
    snapshot.value = current
    draft.value = runtimeCoreSettingsDraft(current.profile)
    version.value = core?.version || ''
    interfaces.value = network || []
    interfacesUnavailable.value = !network
  } catch (cause) {
    if (!disposed && sequence === loadSequence) {
      draft.value = undefined
      if (!keepError) error.value = errorText(cause)
    }
  } finally {
    if (!disposed && sequence === loadSequence) loading.value = false
  }
}

const apply = async () => {
  if (!snapshot.value || !draft.value || busy.value || !kernel.running) return
  const current = snapshot.value
  const options = { platform: app.platformOS, stackEditable: stackEditable.value }
  error.value = ''
  saved.value = false
  // Reject local input errors without a restart or discarding the user's draft.
  try {
    applyRuntimeCoreSettings(current.profile, draft.value, options)
  } catch (cause) {
    error.value = errorText(cause)
    return
  }
  applying.value = true
  try {
    await kernel.updateRuntimeSettings(draft.value, current, options)
    if (disposed) return
    await reload(true)
    saved.value = !!snapshot.value
  } catch (cause) {
    if (disposed) return
    error.value = errorText(cause)
    // Fetch the surviving instance after failed preflight. If startup failed
    // after stopping, clear the form instead of claiming the old core survived.
    await reload(true)
  } finally {
    if (!disposed) applying.value = false
  }
}

watch(
  () => [kernel.pid, kernel.running],
  () => {
    if (applying.value || disposed) return
    if (kernel.running) void reload()
    else {
      ++loadSequence
      loadController?.abort()
      loading.value = false
      snapshot.value = undefined
      draft.value = undefined
    }
  },
)
onMounted(() => void reload())
onUnmounted(() => {
  disposed = true
  ++loadSequence
  loadController?.abort()
})
</script>

<template>
  <div class="runtime-core-settings">
    <Divider class="mb-8">{{ t('home.overview.settingsTips') }}</Divider>
    <p class="mb-12 text-12">{{ t(key + 'restartHint') }}</p>
    <p v-if="error" role="alert" class="mb-12 break-all">{{ error }}</p>
    <p v-if="saved" role="status" class="mb-12">{{ t(key + 'applied') }}</p>
    <p v-if="!kernel.running" role="status">{{ t(key + 'notRunning') }}</p>
    <p v-else-if="loading" role="status">{{ t('common.loading') }}</p>
    <div v-if="draft" class="runtime-settings-grid" :inert="busy || undefined">
      <Card
        v-for="proxy in draft.proxies"
        :key="proxy.id || proxy.type"
        :title="t(`kernel.inbounds.${proxy.type}Port`)"
        :subtitle="proxy.tag"
        :data-proxy="proxy.id || proxy.type"
      >
        <Input
          v-model="proxy.port"
          type="number"
          :input-attrs="{ 'aria-label': `${proxy.tag} ${t(`kernel.inbounds.${proxy.type}Port`)}` }"
          :disabled="busy"
          class="w-full"
        />
        <p class="text-12 my-8">
          {{ t(key + (proxy.dynamicPort && proxy.port === 0 ? 'dynamicPortHint' : 'portHint')) }}
        </p>
        <Switch
          :model-value="runtimeAllowsLAN(proxy.listen)"
          :disabled="busy"
          label="kernel.allow-lan"
          @update:model-value="setRuntimeAllowLAN(proxy, $event)"
        />
        <p class="text-12 my-8 break-all">{{ t(key + 'listen') }}: {{ proxy.listen || '::' }}</p>
      </Card>
      <template v-for="tunnel in draft.tunnels" :key="tunnel.id">
        <Card
          :title="t('kernel.inbounds.tun.stack')"
          :subtitle="tunnel.tag"
          :data-stack="tunnel.id"
        >
          <Select
            v-if="stackEditable"
            v-model="tunnel.stack"
            :options="TunStackOptions"
            :aria-label="`${tunnel.tag} ${t('kernel.inbounds.tun.stack')}`"
          />
          <p v-else class="text-12">
            {{ t(key + (version ? 'stackManaged' : 'stackUnavailable')) }}
          </p>
          <p class="text-12 my-8">{{ t(key + 'stackHint') }}</p>
        </Card>
        <Card :title="t('kernel.inbounds.tun.interface_name')" :subtitle="tunnel.tag">
          <Input
            v-model="tunnel.interfaceName"
            :disabled="busy"
            :placeholder="t('common.auto')"
            :input-attrs="{
              'aria-label': `${tunnel.tag} ${t('kernel.inbounds.tun.interface_name')}`,
            }"
            class="w-full"
          />
        </Card>
      </template>
      <Card :title="t('kernel.route.default_interface')">
        <Select
          v-model="interfaceChoice"
          :options="interfaceOptions"
          :aria-label="t('kernel.route.default_interface')"
        />
        <p v-if="interfacesUnavailable" class="text-12 my-8">
          {{ t(key + 'interfacesUnavailable') }}
        </p>
      </Card>
    </div>
    <p v-if="draft && !draft.tunnels.length" class="text-12 mt-8">
      {{ t('home.overview.needTun') }}
    </p>
    <div class="runtime-settings-actions flex justify-end gap-8 mt-16">
      <Button :disabled="busy || !kernel.running" @click="reload()">{{
        t('home.overview.refresh')
      }}</Button>
      <Button
        type="primary"
        :disabled="busy || !snapshot || !kernel.running"
        data-apply
        @click="apply"
        >{{ t(key + 'apply') }}</Button
      >
    </div>
  </div>
</template>

<style lang="less" scoped>
.runtime-settings-grid {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 8px;
}
@media (max-width: 900px) {
  .runtime-settings-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}
@media (max-width: 500px) {
  .runtime-settings-grid {
    grid-template-columns: minmax(0, 1fr);
  }
  .runtime-settings-actions {
    flex-wrap: wrap;
  }
  .runtime-settings-actions :deep([role='button']) {
    min-height: 44px;
  }
}
</style>
