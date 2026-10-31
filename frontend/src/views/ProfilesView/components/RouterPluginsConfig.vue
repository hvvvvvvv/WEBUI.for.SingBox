<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, useTemplateRef } from 'vue'
import { useI18n } from 'vue-i18n'

import { recoverAuthToken } from '@/bridge/http'
import { useAppSettingsStore } from '@/stores'
import { message } from '@/utils'
import { sampleID } from '@/utils/others'
import {
  MaxRouterPluginBytes,
  MaxRouterPlugins,
  RouterPluginError,
  isNewerRouterPluginVersion,
  moveRouterPlugin,
  parseRouterPluginManifest,
  updateRouterPluginManifest,
  validateProfileRouterPlugins,
  type RouterPluginManifest,
} from '@/utils/profileRouterPlugins'

const props = defineProps<{ profile: IProfile }>()
const model = defineModel<IRouterPlugin[]>({ required: true })
const emit = defineEmits<{ preview: [] }>()
const { t } = useI18n()
const appSettings = useAppSettingsStore()
const fileInput = useTemplateRef<HTMLInputElement>('fileInput')
const catalog = ref<RouterPluginManifest[]>([])
const catalogLoading = ref(false)
const catalogError = ref('')
const catalogWarning = ref('')
const catalogSource = ref('bundled')
const diagnosticsLoading = ref(false)
const diagnosticsError = ref('')
const fieldRevision = ref(0)
type DiagnosticReport = {
  os: string
  arch: string
  checks: { id: string; label: string; status: 'ok' | 'warning' | 'unsupported'; message: string }[]
}
const diagnostics = ref<DiagnosticReport>()
const requests = new Set<AbortController>()

const errorText = (error: unknown) =>
  error instanceof RouterPluginError
    ? t(error.issue.key, { field: error.issue.field || '', name: error.issue.name || '' })
    : error instanceof Error
      ? error.message
      : String(error)

// Use the same token recovery as RPC/legacy calls, with GET for the read-only router APIs.
const requestRouterJson = async <T>(path: string): Promise<T> => {
  const controller = new AbortController()
  requests.add(controller)
  const timeout = setTimeout(() => controller.abort(), 30_000)
  try {
    if (!appSettings.sessionInfo.cacheToken && !(await recoverAuthToken())) {
      throw new Error(t('profile.routerPlugins.errors.authentication'))
    }
    const request = () =>
      fetch(path, {
        method: 'GET',
        headers: { Authorization: `Bearer ${appSettings.sessionInfo.cacheToken}` },
        signal: controller.signal,
        cache: 'no-store',
      })
    let response = await request()
    if (response.status === 401 && (await recoverAuthToken())) response = await request()
    if (!response.ok) throw new Error(`HTTP ${response.status}`)
    return (await response.json()) as T
  } finally {
    clearTimeout(timeout)
    requests.delete(controller)
  }
}

const loadCatalog = async (refresh = false) => {
  catalogLoading.value = true
  catalogError.value = ''
  try {
    const result = await requestRouterJson<{
      plugins: unknown[]
      source: string
      warning?: string
    }>(`/api/router/plugins${refresh ? '?refresh=1' : ''}`)
    if (!Array.isArray(result.plugins))
      throw new RouterPluginError({ key: 'profile.routerPlugins.errors.invalidManifest' })
    catalog.value = result.plugins.map(parseRouterPluginManifest)
    catalogSource.value = ['bundled', 'cache', 'remote'].includes(result.source)
      ? result.source
      : 'bundled'
    catalogWarning.value = result.warning || ''
  } catch (error) {
    catalogError.value = errorText(error)
  } finally {
    catalogLoading.value = false
  }
}

const loadDiagnostics = async () => {
  diagnosticsLoading.value = true
  diagnosticsError.value = ''
  try {
    diagnostics.value = await requestRouterJson<DiagnosticReport>('/api/router/diagnostics')
  } catch (error) {
    diagnosticsError.value = errorText(error)
  } finally {
    diagnosticsLoading.value = false
  }
}

onMounted(() => {
  void loadCatalog()
  void loadDiagnostics()
})
onUnmounted(() => requests.forEach((controller) => controller.abort()))

const inboundOptions = computed(() =>
  props.profile.inbounds
    .filter((inbound) => inbound.enable && inbound.type === 'tun')
    .map((inbound) => ({ label: inbound.tag, value: inbound.tag })),
)
const outboundOptions = computed(() =>
  props.profile.outbounds.map((outbound) => ({ label: outbound.tag, value: outbound.tag })),
)
const rows = computed(() =>
  model.value.map((plugin) => {
    try {
      return { plugin, manifest: parseRouterPluginManifest(plugin.manifest_json), error: '' }
    } catch (error) {
      return { plugin, manifest: undefined, error: errorText(error) }
    }
  }),
)
const validationMessage = computed(() => {
  const issue = validateProfileRouterPlugins({ ...props.profile, router_plugins: model.value })
  return issue ? t(issue.key, { field: issue.field || '', name: issue.name || '' }) : ''
})

const install = (manifest: RouterPluginManifest) => {
  if (model.value.length >= MaxRouterPlugins) {
    throw new RouterPluginError({ key: 'profile.routerPlugins.errors.tooMany' })
  }
  const snapshot = parseRouterPluginManifest(manifest)
  model.value = [
    ...model.value,
    { id: sampleID(), enabled: false, manifest_json: JSON.stringify(snapshot) },
  ]
}
const installFromCatalog = (manifest: RouterPluginManifest) => {
  try {
    install(manifest)
  } catch (error) {
    message.error(errorText(error))
  }
}

const importFile = async (event: Event) => {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = ''
  if (!file) return
  try {
    if (file.size > MaxRouterPluginBytes)
      throw new RouterPluginError({ key: 'profile.routerPlugins.errors.tooLarge' })
    install(parseRouterPluginManifest(await file.text()))
  } catch (error) {
    message.error(errorText(error))
  }
}

const exportManifest = (manifest: RouterPluginManifest) => {
  const pretty = JSON.stringify(manifest, null, 2) + '\n'
  const content =
    new TextEncoder().encode(pretty).length <= MaxRouterPluginBytes
      ? pretty
      : JSON.stringify(manifest)
  const blob = new Blob([content], { type: 'application/json' })
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = `${manifest.id}.json`
  link.click()
  setTimeout(() => URL.revokeObjectURL(url), 1000)
}

const updateConfig = (index: number, key: string, value: unknown) => {
  try {
    const plugin = model.value[index]!
    const manifest = parseRouterPluginManifest(plugin.manifest_json)
    const next = parseRouterPluginManifest({
      ...manifest,
      config: { ...manifest.config, [key]: value },
    })
    model.value = model.value.map((item, i) =>
      i === index ? { ...item, manifest_json: JSON.stringify(next) } : item,
    )
  } catch (error) {
    fieldRevision.value++
    message.error(errorText(error))
  }
}

const setEnabled = (index: number, enabled: boolean) => {
  const next = model.value.map((item, i) => (i === index ? { ...item, enabled } : item))
  if (enabled) {
    const issue = validateProfileRouterPlugins({ ...props.profile, router_plugins: next })
    if (issue) {
      message.error(t(issue.key, { field: issue.field || '', name: issue.name || '' }))
      return
    }
  }
  model.value = next
}

const availableUpdate = (manifest: RouterPluginManifest) =>
  catalog.value.find(
    (item) => item.id === manifest.id && isNewerRouterPluginVersion(item.version, manifest.version),
  )

const updateInstalled = (index: number) => {
  try {
    const plugin = model.value[index]!
    const installed = parseRouterPluginManifest(plugin.manifest_json)
    const latest = availableUpdate(installed)
    if (!latest) return
    const next = updateRouterPluginManifest(installed, latest)
    model.value = model.value.map((item, i) =>
      i === index ? { ...item, manifest_json: JSON.stringify(next) } : item,
    )
    message.success(t('profile.routerPlugins.updated'))
  } catch (error) {
    message.error(errorText(error))
  }
}
</script>

<template>
  <div class="router-plugins">
    <p class="router-notice">{{ t('profile.routerPlugins.intro') }}</p>
    <p class="router-notice">{{ t('profile.routerPlugins.restartNotice') }}</p>
    <div class="router-actions">
      <Button @click="emit('preview')">{{ t('profile.routerPlugins.preview') }}</Button>
      <Button @click="fileInput?.click()">{{ t('profile.routerPlugins.import') }}</Button>
      <input
        ref="fileInput"
        type="file"
        accept="application/json,.json"
        hidden
        @change="importFile"
      />
    </div>

    <section class="router-section">
      <h3>{{ t('profile.routerPlugins.catalog') }}</h3>
      <div class="router-actions">
        <Button :loading="catalogLoading" @click="loadCatalog(true)">{{
          t('profile.routerPlugins.refreshCatalog')
        }}</Button>
        <a
          href="https://github.com/Ghostdehole/WEBUI.for.SingBox-Plugins"
          target="_blank"
          rel="noopener noreferrer"
          >{{ t('profile.routerPlugins.repository') }} ↗</a
        >
        <span>{{ t(`profile.routerPlugins.source.${catalogSource}`) }}</span>
      </div>
      <p v-if="catalogError" role="alert" class="router-warning">
        {{ t('profile.routerPlugins.loadFailed', { error: catalogError }) }}
      </p>
      <p v-if="catalogWarning" role="status" class="router-warning">{{ catalogWarning }}</p>
      <div class="router-catalog">
        <div v-for="manifest in catalog" :key="manifest.id" class="router-package">
          <div class="router-package-heading">
            <strong>{{ manifest.name }}</strong>
            <span>v{{ manifest.version }}</span>
          </div>
          <p>{{ manifest.description }}</p>
          <Button
            size="small"
            :disabled="model.length >= MaxRouterPlugins"
            @click="installFromCatalog(manifest)"
            >{{ t('profile.routerPlugins.install') }}</Button
          >
        </div>
      </div>
    </section>

    <section class="router-section">
      <h3>{{ t('profile.routerPlugins.installed') }} ({{ model.length }})</h3>
      <p class="router-notice">{{ t('profile.routerPlugins.orderNotice') }}</p>
      <p v-if="!model.length" class="router-notice">{{ t('profile.routerPlugins.empty') }}</p>
      <p v-if="validationMessage" role="alert" class="router-warning">{{ validationMessage }}</p>
      <div v-for="(row, index) in rows" :key="row.plugin.id" class="router-package">
        <div class="router-package-heading">
          <strong>{{ row.manifest?.name || row.plugin.id }}</strong>
          <span v-if="row.manifest">v{{ row.manifest.version }}</span>
          <Switch
            :model-value="row.plugin.enabled"
            :aria-label="t('profile.routerPlugins.enabled')"
            @update:model-value="setEnabled(index, $event)"
          />
        </div>
        <p v-if="row.error" class="router-warning" role="alert">{{ row.error }}</p>
        <template v-if="row.manifest">
          <p>{{ row.manifest.description }}</p>
          <p v-if="row.manifest.kind === 'dns-hijack'" class="router-notice">
            {{ t('profile.routerPlugins.dnsNotice') }}
          </p>
          <div class="router-field">
            <span>{{ t('profile.routerPlugins.inbound') }}</span>
            <Select
              :model-value="row.manifest.config.inbound"
              :options="inboundOptions"
              :aria-label="t('profile.routerPlugins.inbound')"
              :placeholder="t('profile.routerPlugins.selectInbound')"
              @update:model-value="updateConfig(index, 'inbound', $event)"
            />
          </div>
          <p v-if="!inboundOptions.length" class="router-warning">
            {{ t('profile.routerPlugins.noTun') }}
          </p>
          <template v-if="row.manifest.kind === 'tun-routing'">
            <p class="router-notice">{{ t('profile.routerPlugins.tunNotice') }}</p>
            <div class="router-field">
              <span>{{ t('profile.routerPlugins.autoRedirect') }}</span>
              <Switch
                :model-value="row.manifest.config.auto_redirect"
                :aria-label="t('profile.routerPlugins.autoRedirect')"
                @update:model-value="updateConfig(index, 'auto_redirect', $event)"
              />
            </div>
            <div class="router-field">
              <span>{{ t('profile.routerPlugins.strictRoute') }}</span>
              <Switch
                :model-value="row.manifest.config.strict_route"
                :aria-label="t('profile.routerPlugins.strictRoute')"
                @update:model-value="updateConfig(index, 'strict_route', $event)"
              />
            </div>
            <div class="router-field router-field-list">
              <span>{{ t('profile.routerPlugins.includeInterface') }}</span>
              <InputList
                :key="fieldRevision"
                :model-value="row.manifest.config.include_interface"
                placeholder="br-lan"
                @update:model-value="updateConfig(index, 'include_interface', $event)"
              />
            </div>
            <div class="router-field router-field-list">
              <span>{{ t('profile.routerPlugins.excludeAddress') }}</span>
              <InputList
                :key="fieldRevision"
                :model-value="row.manifest.config.exclude_address"
                placeholder="192.168.1.0/24"
                @update:model-value="updateConfig(index, 'exclude_address', $event)"
              />
            </div>
          </template>
          <template v-if="row.manifest.kind === 'source-routing'">
            <p class="router-notice">{{ t('profile.routerPlugins.sourceNotice') }}</p>
            <div class="router-field router-field-list">
              <span>{{ t('profile.routerPlugins.sourceAddress') }}</span>
              <InputList
                :key="fieldRevision"
                :model-value="row.manifest.config.source_ip_cidr"
                placeholder="192.168.1.10/32"
                @update:model-value="updateConfig(index, 'source_ip_cidr', $event)"
              />
            </div>
            <div class="router-field">
              <span>{{ t('profile.routerPlugins.outbound') }}</span>
              <Select
                :model-value="row.manifest.config.outbound"
                :options="outboundOptions"
                :aria-label="t('profile.routerPlugins.outbound')"
                :placeholder="t('profile.routerPlugins.selectOutbound')"
                @update:model-value="updateConfig(index, 'outbound', $event)"
              />
            </div>
          </template>
        </template>
        <div class="router-actions">
          <Button
            size="small"
            :disabled="index === 0"
            @click="model = moveRouterPlugin(model, index, -1)"
            >{{ t('profile.routerPlugins.moveUp') }}</Button
          >
          <Button
            size="small"
            :disabled="index === model.length - 1"
            @click="model = moveRouterPlugin(model, index, 1)"
            >{{ t('profile.routerPlugins.moveDown') }}</Button
          >
          <Button v-if="row.manifest" size="small" @click="exportManifest(row.manifest)">{{
            t('profile.routerPlugins.export')
          }}</Button>
          <Button
            v-if="row.manifest && availableUpdate(row.manifest)"
            size="small"
            @click="updateInstalled(index)"
            >{{
              t('profile.routerPlugins.update', { version: availableUpdate(row.manifest)?.version })
            }}</Button
          >
          <Button size="small" @click="model = model.filter((_, i) => i !== index)">{{
            t('profile.routerPlugins.remove')
          }}</Button>
        </div>
      </div>
    </section>

    <section class="router-section">
      <div class="router-package-heading">
        <h3>{{ t('profile.routerPlugins.diagnostics') }}</h3>
        <span v-if="diagnostics">{{ diagnostics.os }} / {{ diagnostics.arch }}</span>
        <Button size="small" :loading="diagnosticsLoading" @click="loadDiagnostics">{{
          t('profile.routerPlugins.refreshDiagnostics')
        }}</Button>
      </div>
      <p class="router-notice">{{ t('profile.routerPlugins.diagnosticsNotice') }}</p>
      <p v-if="diagnosticsError" role="alert" class="router-warning">
        {{ t('profile.routerPlugins.loadFailed', { error: diagnosticsError }) }}
      </p>
      <ul v-if="diagnostics" class="router-checks">
        <li v-for="check in diagnostics.checks" :key="check.id">
          <strong>{{ check.label }}</strong>
          <span :class="{ 'router-warning': check.status !== 'ok' }">{{
            t(`profile.routerPlugins.status.${check.status}`)
          }}</span>
          <p>{{ check.message }}</p>
        </li>
      </ul>
    </section>
  </div>
</template>

<style scoped lang="less">
.router-plugins {
  min-width: 0;
  overflow-wrap: anywhere;
}
.router-section {
  margin-top: 20px;
}
h3 {
  font-weight: 600;
  margin-bottom: 8px;
}
.router-notice {
  opacity: 0.75;
  margin: 8px 0;
  font-size: 13px;
}
.router-warning {
  color: var(--primary-color);
  margin: 8px 0;
}
.router-actions,
.router-package-heading {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
}
.router-actions {
  margin: 10px 0;
}
.router-actions a {
  color: var(--primary-color);
  text-decoration: underline;
}
.router-package-heading strong {
  margin-right: auto;
}
.router-package-heading > strong,
.router-package-heading > span,
.router-field > span {
  min-width: 0;
  overflow-wrap: anywhere;
}
.router-package-heading > .gui-switch,
.router-field > .gui-switch {
  flex-shrink: 0;
  min-width: 50px;
}
.router-catalog {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(min(100%, 240px), 1fr));
  gap: 8px;
}
.router-package {
  padding: 12px;
  margin-bottom: 10px;
  border: 1px solid color-mix(in srgb, currentColor 16%, transparent);
  background: var(--card-bg);
  border-radius: 8px;
  min-width: 0;
}
.router-package p {
  margin: 8px 0;
}
.router-field {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin: 12px 0;
}
.router-field-list {
  align-items: flex-start;
}
.router-field > :last-child:not(.gui-switch) {
  min-width: 0;
  max-width: 60%;
}
.router-checks {
  padding: 0;
  list-style: none;
}
.router-checks li {
  margin: 12px 0;
}
.router-checks span {
  margin-left: 10px;
}
.router-checks p {
  margin-top: 4px;
  opacity: 0.8;
}
@media (max-width: 600px) {
  .router-field {
    flex-wrap: wrap;
  }
  .router-field-list {
    flex-direction: column;
    align-items: stretch;
  }
  .router-field > :last-child:not(.gui-switch) {
    max-width: 100%;
  }
  .router-field-list > :last-child {
    width: 100%;
  }
}
</style>
