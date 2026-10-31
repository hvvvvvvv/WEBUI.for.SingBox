<script lang="ts" setup>
import { ColorOptions, DefaultFontFamily, ThemeOptions } from '@/constant/app'
import { Color } from '@/enums/app'
import routes from '@/router/routes'
import { useAppSettingsStore, useAppStore } from '@/stores'

const appStore = useAppStore()
const appSettings = useAppSettingsStore()
const pages = routes.filter((route) => route.name !== 'Settings')

const resetFontFamily = () => {
  appSettings.app.fontFamily = DefaultFontFamily
}

const onThemeClick = (e: MouseEvent) => {
  document.documentElement.style.setProperty('--x', e.clientX + 'px')
  document.documentElement.style.setProperty('--y', e.clientY + 'px')
}
</script>
<template>
  <div class="px-8 py-12 text-18 font-bold">{{ $t('settings.personalization') }}</div>

  <Card class="personalization-settings">
    <div class="px-8 py-12 flex items-center justify-between">
      <div class="text-16 font-bold">{{ $t('settings.theme.name') }}</div>
      <Radio v-model="appSettings.app.theme" :options="ThemeOptions" @click="onThemeClick" />
    </div>
    <div class="px-8 py-12 flex items-center justify-between">
      <div class="text-16 font-bold">{{ $t('settings.color.name') }}</div>
      <div class="flex items-center">
        <div v-if="appSettings.app.color === Color.Custom" class="flex items-center mr-4">
          <ColorPicker v-model="appSettings.app.primaryColor">
            <template #suffix>
              <div class="text-12">{{ $t('settings.color.primary') }}</div>
            </template>
          </ColorPicker>
          <ColorPicker v-model="appSettings.app.secondaryColor">
            <template #suffix>
              <div class="text-12">{{ $t('settings.color.secondary') }}</div>
            </template>
          </ColorPicker>
        </div>
        <Radio v-model="appSettings.app.color" :options="ColorOptions" />
      </div>
    </div>
    <div class="px-8 py-12 flex items-center justify-between">
      <div class="flex items-center text-16 font-bold">
        <div class="mr-4">{{ $t('settings.lang.name') }}</div>
      </div>
      <Radio v-model="appSettings.app.lang" :options="appStore.locales" />
    </div>
    <div class="px-8 py-12 flex items-center justify-between">
      <div class="text-16 font-bold">{{ $t('settings.fontFamily') }}</div>
      <Input v-model="appSettings.app.fontFamily" editable class="text-14">
        <template #suffix>
          <Button
            v-tips="'settings.resetFont'"
            type="text"
            size="small"
            icon="reset"
            @click="resetFontFamily"
          />
        </template>
      </Input>
    </div>
    <div class="px-8 py-12 flex items-center justify-between">
      <div>
        <div class="text-16 font-bold">{{ $t('settings.pages.name') }}</div>
        <div class="page-help">{{ $t('settings.pages.hint') }}</div>
      </div>
      <div class="page-choices">
        <label v-for="page in pages" :key="page.path" class="page-choice cursor-pointer">
          <input v-model="appSettings.app.pages" type="checkbox" :value="String(page.name)" />
          <span>{{ $t(String(page.meta?.name)) }}</span>
        </label>
      </div>
    </div>
  </Card>
</template>

<style scoped lang="less">
.personalization-settings :deep(.px-8.py-12) {
  gap: 12px;
  flex-wrap: wrap;
}
.page-choices {
  display: flex;
  flex-wrap: wrap;
  gap: 8px 16px;
}
.page-choice {
  display: flex;
  align-items: center;
  gap: 6px;
}
.page-choice input {
  accent-color: var(--primary-color);
}
.page-help {
  font-size: 12px;
  opacity: 0.7;
  margin-top: 4px;
}
</style>
