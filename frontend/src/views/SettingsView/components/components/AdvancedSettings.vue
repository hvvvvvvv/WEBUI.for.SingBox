<script lang="ts" setup>
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { parse } from 'yaml'

import { MakeDir, OpenDir, ReadFile, clearAuthToken, setAuthToken } from '@/bridge'
import { apiCall } from '@/bridge/http'
import { RollingReleaseDirectory, UserFilePath } from '@/constant/app'
import { useAppSettingsStore } from '@/stores'
import { APP_TITLE, APP_VERSION, message } from '@/utils'

const { t } = useI18n()
const appSettings = useAppSettingsStore()

const authSecret = ref('')
const authSecretConfirm = ref('')
const authLoading = ref(false)
const authEnabled = ref(!!(window as any).__AUTH_REQUIRED__)

const handleOpenRollingReleaseFolder = async () => {
  await MakeDir(RollingReleaseDirectory)
  await OpenDir(RollingReleaseDirectory)
}

const handleClearApiToken = () => {
  appSettings.app.githubApiToken = ''
}

const handleClearUserAgent = () => {
  appSettings.app.userAgent = ''
}

const handleSetupAuth = async () => {
  if (!authSecret.value) return
  if (authSecret.value !== authSecretConfirm.value) {
    message.error(t('auth.mismatch'))
    return
  }
  authLoading.value = true
  try {
    const result = await apiCall<{ flag: boolean; data: string }>('/auth/setup', authSecret.value)
    if (result.flag) {
      // Server returns a fresh session token (old sessions were cleared)
      if (result.data) setAuthToken(result.data)
      // Refresh extraFields so the authSecret written by Go backend is preserved
      const raw = parse((await ReadFile(UserFilePath)) || '{}')
      if (raw?.authSecret) appSettings.setExtraField('authSecret', raw.authSecret)
      message.success(t('auth.updateSuccess'))
      authEnabled.value = true
      authSecret.value = ''
      authSecretConfirm.value = ''
    } else {
      message.error(result.data)
    }
  } catch (e: any) {
    message.error(e.message || e)
  } finally {
    authLoading.value = false
  }
}

const handleClearAuth = async () => {
  authLoading.value = true
  try {
    const result = await apiCall<{ flag: boolean; data: string }>('/auth/setup', '')
    if (result.flag) {
      appSettings.setExtraField('authSecret', '')
      message.success(t('auth.clearSuccess'))
      authEnabled.value = false
      clearAuthToken()
    } else {
      message.error(result.data)
    }
  } catch (e: any) {
    message.error(e.message || e)
  } finally {
    authLoading.value = false
  }
}
</script>

<template>
  <div class="px-8 py-12 text-18 font-bold">{{ $t('settings.advanced') }}</div>

  <Card>
    <div class="px-8 py-12 flex items-center justify-between">
      <div class="text-16 font-bold">
        {{ $t('settings.rollingRelease') }}
        <span class="font-normal text-12">({{ $t('settings.needRestart') }})</span>
      </div>
      <div class="flex items-center gap-4">
        <Button type="primary" icon="folder" size="small" @click="handleOpenRollingReleaseFolder" />
        <Switch v-model="appSettings.app.rollingRelease" />
      </div>
    </div>
    <div class="px-8 py-12 flex items-center justify-between">
      <div class="text-16 font-bold">{{ $t('settings.realMemoryUsage') }}</div>
      <Switch v-model="appSettings.app.kernel.realMemoryUsage" />
    </div>
    <div class="px-8 py-12 flex items-center justify-between">
      <div class="text-16 font-bold">
        {{ $t('settings.autoRestartKernel.name') }}
        <span class="font-normal text-12">({{ $t('settings.autoRestartKernel.tips') }})</span>
      </div>
      <Switch v-model="appSettings.app.autoRestartKernel" />
    </div>
    <div class="px-8 py-12 flex items-center justify-between">
      <div class="text-16 font-bold">
        {{ $t('settings.githubapi.name') }}
        <span class="font-normal text-12">({{ $t('settings.githubapi.tips') }})</span>
      </div>
      <Input v-model.lazy="appSettings.app.githubApiToken" editable class="text-14">
        <template #suffix>
          <Button
            v-tips="'settings.userAgent.reset'"
            type="text"
            size="small"
            icon="reset"
            @click="handleClearApiToken"
          />
        </template>
      </Input>
    </div>
    <div class="px-8 py-12 flex items-center justify-between">
      <div class="text-16 font-bold">
        {{ $t('settings.userAgent.name') }}
        <span class="font-normal text-12">({{ $t('settings.userAgent.tips') }})</span>
      </div>
      <Input
        v-model.lazy="appSettings.app.userAgent"
        :placeholder="APP_TITLE + '/' + APP_VERSION"
        editable
        class="text-14"
      >
        <template #suffix>
          <Button
            v-tips="'settings.userAgent.reset'"
            type="text"
            size="small"
            icon="reset"
            @click="handleClearUserAgent"
          />
        </template>
      </Input>
    </div>
    <div class="px-8 py-12 flex items-center justify-between">
      <div class="text-16 font-bold">
        {{ $t('settings.multipleInstance') }}
        <span class="font-normal text-12">({{ $t('settings.needRestart') }})</span>
      </div>
      <Switch v-model="appSettings.app.multipleInstance" />
    </div>
  </Card>

  <div class="px-8 py-12 text-18 font-bold">{{ $t('auth.setup') }}</div>

  <Card>
    <div class="px-8 py-12 flex items-center justify-between">
      <div class="text-16 font-bold">
        {{ $t('auth.setup') }}
        <span class="font-normal text-12">({{ $t('auth.setupTips') }})</span>
      </div>
      <Tag v-if="authEnabled" color="primary">{{ $t('auth.authEnabled') }}</Tag>
      <Tag v-else>{{ $t('auth.authDisabled') }}</Tag>
    </div>
    <div class="px-8 py-12 flex items-center justify-between">
      <div class="text-16 font-bold">{{ $t('auth.secret') }}</div>
      <input
        v-model="authSecret"
        type="password"
        :placeholder="$t('auth.placeholder')"
        class="auth-input"
      />
    </div>
    <div class="px-8 py-12 flex items-center justify-between">
      <div class="text-16 font-bold">{{ $t('auth.confirmSecret') }}</div>
      <input
        v-model="authSecretConfirm"
        type="password"
        :placeholder="$t('auth.confirmSecret')"
        class="auth-input"
      />
    </div>
    <div class="px-8 py-12 flex items-center justify-end gap-8">
      <Button
        v-if="authEnabled"
        type="text"
        :loading="authLoading"
        @click="handleClearAuth"
      >
        {{ $t('auth.clearAuth') }}
      </Button>
      <Button
        type="primary"
        :loading="authLoading"
        :disabled="!authSecret || !authSecretConfirm"
        @click="handleSetupAuth"
      >
        {{ $t('common.save') }}
      </Button>
    </div>
  </Card>
</template>

<style scoped>
.auth-input {
  width: 220px;
  padding: 6px 10px;
  border: 1px solid var(--border-color, #ddd);
  border-radius: 6px;
  font-size: 14px;
  outline: none;
  background: var(--input-bg-color, #fafafa);
  color: var(--text-color, #333);
  transition: border-color 0.2s;
}

.auth-input:focus {
  border-color: var(--primary-color, #409eff);
}
</style>
