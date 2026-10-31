<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { usePluginsStore } from '@/stores/plugins'
import { message } from '@/utils'
import type { PluginCatalogItem } from '@/types/plugin'

const { t } = useI18n()
const store = usePluginsStore()
const keywords = ref('')
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
const filtered = computed(() =>
  store.catalog.plugins.filter((plugin) =>
    `${plugin.name} ${plugin.description}`
      .toLocaleLowerCase()
      .includes(keywords.value.trim().toLocaleLowerCase()),
  ),
)
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
  try {
    await store.installPlugin(id)
  } catch (error) {
    message.error(error instanceof Error ? error.message : String(error))
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
        :placeholder="t('plugins.search')"
        clearable
        class="plugin-search"
      />
      <Button :loading="store.catalogLoading" @click="refresh()">{{
        t('plugins.checkForUpdates')
      }}</Button>
      <a :href="repositoryURL" target="_blank" rel="noopener noreferrer"
        >{{ t('plugins.repository') }} ↗</a
      >
    </div>
    <p class="plugin-hub-note">{{ t('plugins.hubHint') }}</p>
    <p v-if="store.catalog.source !== 'empty'" class="plugin-hub-note">
      {{ t(store.catalog.source === 'cache' ? 'plugins.catalogCached' : 'plugins.catalogRemote') }}
    </p>
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
    <div class="plugin-hub-grid">
      <Card v-for="plugin in filtered" :key="plugin.id" :title="plugin.name">
        <template #extra
          ><Tag size="small">{{ plugin.version }}</Tag></template
        >
        <p class="plugin-hub-description">{{ plugin.description }}</p>
        <p v-if="enabledConflicts(plugin).length" class="plugin-hub-note">
          {{ t('plugins.conflictsWith') }} {{ enabledConflicts(plugin).join('、') }}
        </p>
        <div class="plugin-hub-bottom">
          <Tag v-for="trigger in plugin.triggers" :key="trigger" size="small">{{
            t('plugin.' + trigger)
          }}</Tag>
          <Button
            v-if="store.getPluginById(plugin.id)"
            size="small"
            icon="selected"
            disabled
            class="ml-auto"
            >{{ t('plugins.installed') }}</Button
          >
          <Button
            v-else
            size="small"
            icon="add"
            type="primary"
            :disabled="store.busy || enabledConflicts(plugin).length > 0"
            class="ml-auto"
            @click="install(plugin.id)"
            >{{ t('plugins.install') }}</Button
          >
        </div>
      </Card>
    </div>
    <div v-if="!store.catalog.plugins.length && !store.catalogLoading" class="plugin-hub-empty">
      <p role="status">
        {{
          t(
            store.catalog.source === 'empty'
              ? 'plugins.catalogUnavailable'
              : 'plugins.catalogEmpty',
          )
        }}
      </p>
      <Button @click="refresh(true)">{{ t('plugins.retry') }}</Button>
    </div>
    <p
      v-else-if="store.catalog.plugins.length && !filtered.length"
      role="status"
      class="plugin-hub-note"
    >
      {{ t('plugins.catalogNoResults') }}
    </p>
  </div>
</template>

<style scoped lang="less">
.plugin-hub {
  min-width: 0;
}
.plugin-hub-toolbar,
.plugin-hub-bottom {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
}
.plugin-search {
  flex: 1;
  min-width: 160px;
}
.plugin-hub-toolbar a {
  color: var(--primary-color);
  text-decoration: underline;
}
.plugin-hub-note {
  opacity: 0.7;
  margin: 12px 0;
  font-size: 12px;
  overflow-wrap: anywhere;
}
.plugin-hub-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 12px;
}
.plugin-hub-empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 12px;
  padding: 24px 12px;
  text-align: center;
}
.plugin-hub-description {
  margin: 8px 0 12px;
  overflow-wrap: anywhere;
}
@media (max-width: 650px) {
  .plugin-hub-grid {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
