<script setup lang="ts">
import { useI18n } from "vue-i18n";

import Button from "@/components/Button/index.vue";
import Card from "@/components/Card/index.vue";
import Input from "@/components/Input/index.vue";
import Radio from "@/components/Radio/index.vue";
import Switch from "@/components/Switch/index.vue";
import type { Plugin, PluginSetting } from "@/types/plugin";

const props = withDefaults(
  defineProps<{
    plugin: Plugin;
    modelValue?: Record<string, unknown>;
    disabled?: boolean;
    heading?: string;
  }>(),
  { modelValue: () => ({}), disabled: false, heading: '' },
);
const emit = defineEmits<{
  "update:modelValue": [value: Record<string, unknown>];
  change: [value: Record<string, unknown>];
}>();
const { t } = useI18n();
const getOptions = (options: string[] = []) =>
  options.map((option) => {
    const comma = option.indexOf(",");
    return comma === -1
      ? { label: option, value: option }
      : { label: option.slice(0, comma), value: option.slice(comma + 1) };
  });
const update = (value: Record<string, unknown>) => {
  if (props.disabled) return;
  emit("update:modelValue", value);
  emit("change", value);
};
const onChange = (setting: PluginSetting, value: unknown) => {
  const next = { ...props.modelValue, [setting.key]: value };
  if (value === setting.value) delete next[setting.key];
  update(next);
};
const reset = (key: string) => {
  const next = { ...props.modelValue };
  delete next[key];
  update(next);
};
const resetAll = () => update({});
const overridden = (key: string) => Object.hasOwn(props.modelValue, key);
const valueOf = (setting: PluginSetting) =>
  overridden(setting.key) ? props.modelValue[setting.key] : setting.value;
defineExpose({ reset: resetAll });
</script>

<template>
  <div class="plugin-config-form flex flex-col gap-8">
    <div class="flex items-center justify-end gap-8 px-8 py-8">
      <div v-if="heading" class="text-18 font-bold mr-auto">{{ heading }}</div>
      <Button type="text" size="small" :disabled="disabled" @click="resetAll">{{
        t("plugins.resetSettings")
      }}</Button>
    </div>
    <Card
      v-for="(setting, index) in plugin.configuration_schema || []"
      :key="setting.key"
      :title="`${index + 1}. ${setting.title}`"
      :data-setting-key="setting.key"
      :class="{ overridden: overridden(setting.key) }"
      class="plugin-setting-card"
    >
      <template v-if="overridden(setting.key)" #extra>
        <Button
          :aria-label="`${t('plugins.resetSetting')}: ${setting.title}`"
          :title="t('plugins.resetSetting')"
          icon="clear"
          type="text"
          size="small"
          :disabled="disabled"
          @click="reset(setting.key)"
        />
      </template>
      <div v-if="setting.description" class="mb-8 text-12">{{ setting.description }}</div>
      <Switch
        v-if="setting.component === 'Switch'"
        :model-value="Boolean(valueOf(setting))"
        :disabled="disabled"
        :aria-label="setting.title"
        @update:model-value="(value) => onChange(setting, value)"
      />
      <Radio
        v-else-if="setting.component === 'Radio'"
        :model-value="String(valueOf(setting))"
        :inert="disabled"
        :aria-disabled="disabled || undefined"
        :options="getOptions(setting.options)"
        :aria-label="setting.title"
        @update:model-value="(value) => onChange(setting, value)"
      />
      <Input
        v-else
        :model-value="String(valueOf(setting))"
        :disabled="disabled"
        :aria-label="setting.title"
        editable
        class="plugin-setting-input"
        @change="(value) => onChange(setting, value)"
      />
    </Card>
  </div>
</template>

<style scoped>
.plugin-setting-card {
  border-left: 2px solid transparent;
  min-width: 0;
}
.plugin-setting-card.overridden {
  border-left-color: var(--primary-color);
}
.plugin-setting-input {
  max-width: 100%;
}
.plugin-setting-card :deep(.gui-radio) {
  max-width: 100%;
  flex-wrap: wrap;
}
</style>
