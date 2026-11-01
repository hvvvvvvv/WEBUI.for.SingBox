<script lang="ts" setup>
import { ref } from 'vue'
import { ColorOptions, DefaultFontFamily, ThemeOptions } from '@/constant/app'
import CheckBox from '@/components/CheckBox/index.vue'
import { Color } from '@/enums/app'
import routes from '@/router/routes'
import { useAppSettingsStore, useAppStore } from '@/stores'
import { useBackgroundStore } from '@/stores/background'

const appStore = useAppStore()
const appSettings = useAppSettingsStore()
const background = useBackgroundStore()
const backgroundInput = ref<HTMLInputElement>()
const uploadBackground = async (event: Event) => {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = ''
  if (file) await background.upload(file)
}
const pages = routes
  .filter((route) => route.name !== 'Settings')
  .map((route) => ({ label: String(route.meta?.name), value: String(route.name) }))

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
        <div class="text-16 font-bold">{{ $t('settings.background.name') }}</div>
        <div class="text-12 opacity-60 mt-4">{{ $t('settings.background.browserOnly') }}</div>
      </div>
      <div class="flex items-center gap-8">
        <input
          ref="backgroundInput"
          type="file"
          accept="image/png,image/jpeg,image/webp"
          hidden
          :disabled="background.busy"
          @change="uploadBackground"
        />
        <Button :loading="background.busy" @click="backgroundInput?.click()">
          {{ $t('settings.background.upload') }}
        </Button>
        <Button
          type="text"
          :disabled="background.busy || !background.url"
          @click="background.remove"
        >
          {{ $t('settings.background.remove') }}
        </Button>
      </div>
      <div v-if="background.error" class="background-error text-12" role="alert">
        {{ $t(background.error) }}
      </div>
    </div>
    <div class="px-8 py-12 flex items-center justify-between">
      <div class="text-16 font-bold">{{ $t('settings.pages.name') }}</div>
      <CheckBox
        v-model="appSettings.app.pages"
        :options="pages"
        :aria-label="$t('settings.pages.name')"
      />
    </div>
  </Card>
</template>

<style scoped lang="less">
.personalization-settings :deep(.px-8.py-12) {
  gap: 12px;
  flex-wrap: wrap;
}
.background-error {
  width: 100%;
  overflow-wrap: anywhere;
}
</style>
