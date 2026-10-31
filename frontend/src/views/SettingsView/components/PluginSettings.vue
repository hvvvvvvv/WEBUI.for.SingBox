<script setup lang="ts">
import { onActivated, onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";

import Button from "@/components/Button/index.vue";
import { PluginApiError, usePluginsStore } from "@/stores/plugins";
import type { Plugin } from "@/types/plugin";
import { deepClone } from "@/utils/others";
import PluginConfigItem from "@/views/PluginsView/components/PluginConfigItem.vue";

interface SettingsDraft {
  plugin: Plugin;
  configuration: Record<string, unknown>;
  dirty: boolean;
  version: number;
}

const { t } = useI18n();
const store = usePluginsStore();
const drafts = ref<SettingsDraft[]>([]);
const refreshing = ref(false);
const saving = ref(false);
const saved = ref(false);
const error = ref("");
const conflict = ref(false);
let initialized = false;
let baseRevision = store.revision;

const refresh = async () => {
  if (refreshing.value || saving.value) return;
  refreshing.value = true;
  try {
    await store.setupPlugins();
    drafts.value = store.plugins
      .filter((plugin) => plugin.configuration_schema?.length)
      .map((plugin) => ({
        plugin: deepClone(plugin),
        configuration: deepClone(plugin.configuration || {}),
        dirty: false,
        version: 0,
      }));
    baseRevision = store.revision;
    conflict.value = false;
    error.value = "";
    saved.value = false;
    initialized = true;
  } catch (value) {
    error.value = value instanceof Error ? value.message : String(value);
  } finally {
    refreshing.value = false;
  }
};

// All cards share one backend revision. Serialize saves, keeping edits made while
// a request is pending in the next request rather than replacing them with its response.
const persist = async () => {
  if (saving.value || conflict.value || error.value) return;
  saving.value = true;
  saved.value = false;
  try {
    for (;;) {
      const draft = drafts.value.find((item) => item.dirty);
      if (!draft) break;
      if (store.revision !== baseRevision) {
        conflict.value = true;
        break;
      }
      const version = draft.version;
      const snapshot = await store.savePlugin(
        { ...deepClone(draft.plugin), configuration: deepClone(draft.configuration) },
        baseRevision,
      );
      baseRevision = snapshot.revision;
      const updated = snapshot.plugins.find((plugin) => plugin.id === draft.plugin.id);
      if (updated) draft.plugin = deepClone(updated);
      if (draft.version === version) draft.dirty = false;
    }
    saved.value = !conflict.value && !drafts.value.some((draft) => draft.dirty);
  } catch (value) {
    if (value instanceof PluginApiError && [409, 404].includes(value.status)) {
      conflict.value = true;
    } else {
      error.value = value instanceof Error ? value.message : String(value);
    }
  } finally {
    saving.value = false;
  }
};
const change = (draft: SettingsDraft, configuration: Record<string, unknown>) => {
  draft.configuration = configuration;
  draft.dirty = true;
  draft.version++;
  void persist();
};
const retry = () => {
  error.value = "";
  if (!initialized) void refresh();
  else void persist();
};
const activate = () => {
  if (!drafts.value.some((draft) => draft.dirty) && !conflict.value) void refresh();
};
onMounted(activate);
onActivated(activate);
</script>

<template>
  <div class="plugin-settings flex flex-col gap-8">
    <p v-if="conflict" role="alert" class="plugin-settings-notice">
      {{ t("plugins.settingsConflict") }}
      <Button type="link" :loading="refreshing" @click="refresh">{{
        t("common.loadLatest")
      }}</Button>
    </p>
    <p v-else-if="error" role="alert" class="plugin-settings-notice">
      {{ t("plugins.settingsSaveFailed") }}: {{ error }}
      <Button type="link" @click="retry">{{ t("plugins.retry") }}</Button>
    </p>
    <div v-else-if="saving || saved" role="status" class="text-12 px-8">
      {{ t(saving ? "plugins.savingSettings" : "plugins.settingsSaved") }}
    </div>
    <div v-if="!drafts.length && !refreshing" class="px-8 py-12">
      <div class="text-18 font-bold mb-8">{{ t("plugins.configuration") }}</div>
      <p>{{ t("plugins.settingsEmpty") }}</p>
    </div>
    <section v-for="draft in drafts" :key="draft.plugin.id" :data-plugin-id="draft.plugin.id">
      <PluginConfigItem
        :plugin="draft.plugin"
        :heading="draft.plugin.name"
        :model-value="draft.configuration"
        :disabled="refreshing"
        @change="(value) => change(draft, value)"
      />
    </section>
  </div>
</template>

<style scoped>
.plugin-settings-notice {
  padding: 12px;
  border: 1px solid var(--primary-color);
  border-radius: 6px;
  overflow-wrap: anywhere;
}
</style>
