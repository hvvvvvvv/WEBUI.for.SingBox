<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { usePluginsStore } from '@/stores/plugins'
import { message } from '@/utils'

const { t } = useI18n()
const store = usePluginsStore()
const keywords = ref('')
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
  <div class="plugin-hub">
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
    <p v-if="store.catalog.warning" role="status" class="plugin-hub-note">
      {{ store.catalog.warning }}
    </p>
    <div class="plugin-hub-grid">
      <Card v-for="plugin in filtered" :key="plugin.id" :title="plugin.name">
        <template #extra
          ><Tag size="small">{{ plugin.version }}</Tag></template
        >
        <p class="plugin-hub-description">{{ plugin.description }}</p>
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
            :disabled="store.busy"
            class="ml-auto"
            @click="install(plugin.id)"
            >{{ t('plugins.install') }}</Button
          >
        </div>
      </Card>
    </div>
    <Empty v-if="!filtered.length && !store.catalogLoading" />
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
