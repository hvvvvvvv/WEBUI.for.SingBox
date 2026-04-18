<script lang="ts" setup>
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { apiCall } from '@/bridge/http'
import { useAppSettingsStore } from '@/stores'

const { t } = useI18n()
const appSettings = useAppSettingsStore()

const secret = ref('')
const loading = ref(false)
const error = ref('')

const emit = defineEmits<{
  authenticated: []
}>()

const handleLogin = async () => {
  if (!secret.value) return
  loading.value = true
  error.value = ''

  try {
    const result = await apiCall<{ flag: boolean; data: string }>('/auth/login', secret.value)
    if (result.flag) {
      appSettings.sessionInfo.cacheToken = result.data
      emit('authenticated')
    } else {
      error.value = t('auth.invalidSecret')
    }
  } catch {
    error.value = t('auth.loginFailed')
  } finally {
    loading.value = false
  }
}

const handleKeydown = (e: KeyboardEvent) => {
  if (e.key === 'Enter') {
    handleLogin()
  }
}
</script>

<template>
  <div class="login-container">
    <div class="login-card">
      <div class="login-title">{{ $t('auth.title') }}</div>
      <div class="login-subtitle">{{ $t('auth.subtitle') }}</div>
      <div class="login-form">
        <input
          v-model="secret"
          type="password"
          class="login-input"
          :placeholder="$t('auth.placeholder')"
          autofocus
          @keydown="handleKeydown"
        />
        <div v-if="error" class="login-error">{{ error }}</div>
        <button class="login-btn" :disabled="loading || !secret" @click="handleLogin">
          {{ loading ? $t('auth.verifying') : $t('auth.login') }}
        </button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.login-container {
  display: flex;
  align-items: center;
  justify-content: center;
  height: 100vh;
  width: 100vw;
  background: var(--bg-color, #f5f5f5);
}

.login-card {
  background: var(--card-bg-color, #fff);
  border-radius: 12px;
  padding: 40px 36px;
  min-width: 360px;
  max-width: 420px;
  box-shadow: 0 2px 12px rgba(0, 0, 0, 0.08);
  text-align: center;
}

.login-title {
  font-size: 22px;
  font-weight: bold;
  margin-bottom: 8px;
  color: var(--primary-color, #333);
}

.login-subtitle {
  font-size: 14px;
  color: var(--secondary-text-color, #888);
  margin-bottom: 28px;
}

.login-form {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.login-input {
  width: 100%;
  padding: 10px 14px;
  border: 1px solid var(--border-color, #ddd);
  border-radius: 8px;
  font-size: 15px;
  outline: none;
  background: var(--input-bg-color, #fafafa);
  color: var(--text-color, #333);
  box-sizing: border-box;
  transition: border-color 0.2s;
}

.login-input:focus {
  border-color: var(--primary-color, #409eff);
}

.login-error {
  color: #e74c3c;
  font-size: 13px;
  text-align: left;
}

.login-btn {
  width: 100%;
  padding: 10px 0;
  border: none;
  border-radius: 8px;
  background: var(--primary-color, #409eff);
  color: #fff;
  font-size: 15px;
  font-weight: 500;
  cursor: pointer;
  transition: opacity 0.2s;
}

.login-btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.login-btn:not(:disabled):hover {
  opacity: 0.85;
}
</style>
