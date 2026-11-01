<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { usePluginsStore } from '@/stores/plugins'
import { message } from '@/utils'
import type { PluginCatalogItem } from '@/types/plugin'

const { t } = useI18n()
const store = usePluginsStore()
const keywords = ref('')
const installing = ref('')
const enabledConflicts = (candidate: PluginCatalogItem) =>
  store.plugins
    .filter(
      (installed) =>
        !installed.disabled &&
        installed.id !== candidate.id &&
        (candidate.conflicts?.includes(installed.id) ||
          installed.conflicts?.includes(candidate.id)),
    )
    .map((plugin) => plugin.name)
const groups = computed(() => {
  const tokens = keywords.value.trim().toLocaleLowerCase().split(/\s+/).filter(Boolean)
  const filtered = store.catalog.plugins.filter((plugin) => {
    const text = `${plugin.id} ${plugin.name} ${plugin.description}`.toLocaleLowerCase()
    return tokens.every((token) => text.includes(token))
  })
  const group = (plugin: PluginCatalogItem) =>
    plugin.manual_target === 'diagnostic'
      ? 'diagnostics'
      : plugin.triggers.includes('on::subscribe')
        ? 'subscriptions'
        : plugin.triggers.includes('on::generate')
          ? 'configuration'
          : 'others'
  return ['subscriptions', 'configuration', 'diagnostics', 'others']
    .map((name) => ({ name, plugins: filtered.filter((plugin) => group(plugin) === name) }))
    .filter((item) => item.plugins.length)
})
const repositoryURL = computed(() =>
  /^[\w.-]+\/[\w.-]+$/.test(store.catalog.repository)
    ? `https://github.com/${store.catalog.repository}`
    : 'https://github.com/Ghostdehole/WEBUI.for.SingBox-Plugins',
)
const warnings = computed(() =>
  [...new Set([store.catalog.warning, store.catalogError])].filter(Boolean),
)
const refresh = async (remote = true) => {
  try {
    await store.loadCatalog(remote)
  } catch (error) {
    message.error(error instanceof Error ? error.message : String(error))
  }
}
const install = async (id: string) => {
  if (store.busy || installing.value) return
  installing.value = id
  try {
    await store.installPlugin(id)
  } catch (error) {
    message.error(error instanceof Error ? error.message : String(error))
  } finally {
    installing.value = ''
  }
}
onMounted(() => {
  if (!store.catalog.plugins.length) void refresh(false)
})
</script>

<template>
  <div class="plugin-hub" :aria-busy="store.catalogLoading">
    <div class="plugin-hub-toolbar">
      <Input
        v-model="keywords"
        :border="false"
        :placeholder="`${t('plugins.search')} (${store.catalog.plugins.length})`"
        :input-attrs="{ 'aria-label': t('plugins.search') }"
        clearable
        class="w-full"
      >
        <template #prefix><Icon icon="filter" :size="22" /></template>
        <template #suffix>
          <Button
            v-tips="'plugins.checkForUpdates'"
            icon="refresh"
            type="text"
            :aria-label="t('plugins.checkForUpdates')"
            :loading="store.catalogLoading"
            @click="refresh()"
          />
          <a
            :href="repositoryURL"
            :aria-label="t('plugins.repository')"
            :title="t('plugins.repository')"
            target="_blank"
            rel="noopener noreferrer"
            class="plugin-repository"
            ><Icon icon="link" :size="20"
          /></a>
        </template>
      </Input>
    </div>
    <div class="plugin-hub-body">
      <p v-for="warning in warnings" :key="warning" role="alert" class="plugin-hub-note">
        {{ warning }}
      </p>
      <p
        v-if="store.catalogLoading && !store.catalog.plugins.length"
        role="status"
        class="plugin-hub-note"
      >
        {{ t('plugins.catalogLoading') }}
      </p>
      <div v-for="group in groups" :key="group.name" class="plugin-hub-group">
        <h3 class="plugin-group-title">{{ t(`plugins.groups.${group.name}`) }}</h3>
        <div class="plugin-hub-grid">
          <Card
            v-for="plugin in group.plugins"
            :key="plugin.id"
            :title="plugin.name"
            class="plugin-hub-card"
          >
            <div class="plugin-hub-row">
              <div v-tips="plugin.description" class="plugin-hub-description line-clamp-1">
                {{ plugin.description }}
              </div>
              <Button
                v-if="store.getPluginById(plugin.id)"
                icon="selected"
                type="text"
                size="small"
                :aria-label="t('plugins.installed')"
                :title="t('plugins.installed')"
                disabled
              />
              <Button
                v-else
                icon="add"
                type="text"
                size="small"
                :aria-label="t('plugins.install')"
                :title="t('plugins.install')"
                :loading="installing === plugin.id"
                :disabled="store.busy || enabledConflicts(plugin).length > 0"
                @click="install(plugin.id)"
              />
            </div>
            <p v-if="enabledConflicts(plugin).length" class="plugin-conflict">
              {{ t('plugins.conflictsWith') }} {{ enabledConflicts(plugin).join('、') }}
            </p>
          </Card>
        </div>
      </div>
      <div v-if="!store.catalog.plugins.length && !store.catalogLoading" class="plugin-hub-empty">
        <Empty
          ><template #description>
            <p role="status">
              {{
                t(
                  store.catalog.source === 'empty'
                    ? 'plugins.catalogUnavailable'
                    : 'plugins.catalogEmpty',
                )
              }}
            </p>
            <Button icon="refresh" type="primary" size="small" @click="refresh(true)">{{
              t('plugins.retry')
            }}</Button>
          </template></Empty
        >
      </div>
      <p
        v-else-if="store.catalog.plugins.length && !groups.length"
        role="status"
        class="plugin-hub-note"
      >
        {{ t('plugins.catalogNoResults') }}
      </p>
    </div>
    <div class="plugin-hub-footer">
      <span>{{ t('plugins.hubHint') }}</span>
      <span v-if="store.catalog.source !== 'empty'">{{
        t(store.catalog.source === 'cache' ? 'plugins.catalogCached' : 'plugins.catalogRemote')
      }}</span>
    </div>
  </div>
</template>

<style scoped lang="less">
.plugin-hub {
  min-width: 0;
  height: 100%;
  display: flex;
  flex-direction: column;
}
.plugin-hub-toolbar {
  flex-shrink: 0;
  border-bottom: 1px solid var(--card-bg);
  padding-bottom: 8px;
}
.plugin-repository {
  display: inline-flex;
  align-items: center;
  padding: 8px;
  color: var(--card-color);
}
.plugin-hub-body {
  min-height: 0;
  flex: 1;
  overflow-y: auto;
  padding: 0 4px 12px;
}
.plugin-group-title {
  font-size: 16px;
  font-weight: bold;
  padding: 12px 4px;
  margin: 0;
}
.plugin-hub-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 8px;
}
.plugin-hub-card {
  min-width: 0;
}
.plugin-hub-card :deep(.card-header_title) {
  font-size: 14px;
}
.plugin-hub-row {
  display: flex;
  align-items: center;
  gap: 8px;
}
.plugin-hub-description {
  flex: 1;
  min-width: 0;
  font-size: 12px;
  overflow-wrap: anywhere;
}
.plugin-hub-note,
.plugin-conflict {
  margin: 8px 0;
  font-size: 12px;
  overflow-wrap: anywhere;
}
.plugin-hub-footer {
  display: flex;
  flex-wrap: wrap;
  gap: 4px 12px;
  padding-top: 8px;
  font-size: 12px;
  opacity: 0.7;
}
.plugin-hub-empty {
  padding: 32px 8px;
  text-align: center;
}
@media (max-width: 650px) {
  .plugin-hub-grid {
    grid-template-columns: minmax(0, 1fr);
  }
  .plugin-hub :deep(.gui-button),
  .plugin-repository {
    min-width: 44px;
    min-height: 44px;
    flex-shrink: 0;
    justify-content: center;
  }
  .plugin-hub-description {
    -webkit-line-clamp: 2;
  }
  .plugin-hub-footer {
    flex-direction: column;
  }
}
</style>
