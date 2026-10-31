<script lang="ts" setup>
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { loginAuthToken } from '@/bridge/http'
import { AuthError, getAuthStatus, type AuthStatus } from '@/api/auth'
import logo from '@/assets/logo'
import '@/assets/styles/access.css'

const { t } = useI18n()
const password = ref('')
const code = ref('')
const showPassword = ref(false)
const status = ref<AuthStatus | null>(null)
const statusLoading = ref(true)
const loading = ref(false)
const error = ref('')
let disposed = false
const emit = defineEmits<{ authenticated: [] }>()
const loadStatus = async () => {
  if (loading.value) return
  statusLoading.value = true
  error.value = ''
  try {
    const result = await getAuthStatus()
    if (!disposed) status.value = result
  } catch {
    if (!disposed) error.value = t('auth.statusFailed')
  } finally {
    if (!disposed) statusLoading.value = false
  }
}
const handleLogin = async () => {
  if (loading.value || statusLoading.value || !status.value) return
  if (status.value.enabled && !password.value) return
  if (status.value.totp_enabled && !code.value) return
  loading.value = true
  error.value = ''
  try {
    if (await loginAuthToken(password.value, code.value)) {
      password.value = ''
      code.value = ''
      if (!disposed) emit('authenticated')
    } else if (!disposed) {
      error.value = t('auth.invalidSecret')
    }
  } catch (value) {
    if (!disposed) {
      error.value =
        value instanceof AuthError && (value.status === 429 || value.kind === 'rate_limited')
          ? t('auth.rateLimited')
          : value instanceof AuthError &&
              (value.kind === 'credentials' || [400, 401, 403].includes(value.status))
            ? t('auth.invalidSecret')
            : t('auth.loginFailed')
      code.value = ''
    }
  } finally {
    if (!disposed) loading.value = false
  }
}
onMounted(loadStatus)
onBeforeUnmount(() => {
  disposed = true
  password.value = ''
  code.value = ''
})
</script>

<template>
  <main class="access-colors login-screen">
    <section
      class="login-panel"
      aria-labelledby="login-heading"
      :aria-busy="loading || statusLoading"
    >
      <div class="login-brand">
        <img class="login-mark" :src="logo" alt="" draggable="false" />
        <span>WEBUI <span class="login-brand-small">for SingBox</span></span>
      </div>
      <div class="login-heading-icon" aria-hidden="true">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6">
          <path d="M12 3 4.5 6v5.5c0 4.2 3.2 7.4 7.5 9.5 4.3-2.1 7.5-5.3 7.5-9.5V6L12 3Z" />
          <path d="m8.5 12 2.3 2.3 4.7-4.7" />
        </svg>
      </div>
      <h1 id="login-heading">{{ t('auth.title') }}</h1>
      <p class="access-muted login-description">{{ t('auth.subtitle') }}</p>
      <p v-if="statusLoading" role="status" class="access-muted">{{ t('auth.checkingStatus') }}</p>
      <form class="access-form" @submit.prevent="handleLogin">
        <template v-if="status?.enabled">
          <label class="access-field" for="login-password">
            <span>{{ t('auth.secret') }}</span>
            <span class="access-password-wrap">
              <input
                id="login-password"
                v-model="password"
                :type="showPassword ? 'text' : 'password'"
                name="password"
                autocomplete="current-password"
                :disabled="loading"
                required
                :aria-invalid="!!error"
                :aria-describedby="error ? 'login-error' : undefined"
                autofocus
              />
              <button
                type="button"
                class="access-reveal"
                :aria-label="t(showPassword ? 'auth.hidePassword' : 'auth.showPassword')"
                :aria-pressed="showPassword"
                @click="showPassword = !showPassword"
              >
                {{ t(showPassword ? 'auth.hide' : 'auth.show') }}
              </button>
            </span>
          </label>
          <label v-if="status.totp_enabled" class="access-field" for="login-code">
            <span>{{ t('auth.verificationCode') }}</span>
            <input
              id="login-code"
              v-model="code"
              name="verification-code"
              type="text"
              autocomplete="one-time-code"
              autocapitalize="off"
              spellcheck="false"
              :disabled="loading"
              required
              aria-describedby="login-code-hint"
            />
            <small id="login-code-hint" class="access-muted">{{ t('auth.codeHint') }}</small>
          </label>
        </template>
        <p v-if="error" id="login-error" class="access-error" role="alert">{{ error }}</p>
        <button
          v-if="!status && !statusLoading"
          type="button"
          class="access-button primary"
          @click="loadStatus"
        >
          {{ t('auth.retry') }}
        </button>
        <button
          v-else
          class="access-button primary"
          type="submit"
          :disabled="
            loading ||
            statusLoading ||
            !status ||
            (status.enabled && !password) ||
            (status.totp_enabled && !code)
          "
        >
          {{ loading ? t('auth.verifying') : t('auth.login') }}
          <span v-if="!loading" aria-hidden="true">→</span>
        </button>
      </form>
      <p class="login-footer access-muted">{{ t('auth.loginHint') }}</p>
    </section>
  </main>
</template>

<style scoped>
.login-screen {
  min-height: 100dvh;
  box-sizing: border-box;
  width: 100%;
  display: grid;
  place-items: center;
  padding: 32px 20px;
  color: var(--access-text);
  background:
    radial-gradient(ellipse at 20% 12%, var(--access-glow), transparent 55%),
    radial-gradient(ellipse at 85% 85%, var(--access-glow), transparent 50%), var(--access-bg);
  overflow-y: auto;
}
.login-panel {
  box-sizing: border-box;
  width: min(100%, 440px);
  padding: 32px;
  background: var(--access-card);
  border: 1px solid var(--access-border);
  border-radius: 24px;
  box-shadow: 0 24px 80px var(--access-shadow);
  backdrop-filter: blur(24px);
}
.login-brand {
  display: flex;
  align-items: center;
  gap: 10px;
  font-size: 17px;
  font-weight: 650;
  letter-spacing: 0.02em;
  margin-bottom: 32px;
}
.login-brand-small {
  font-size: 12px;
  font-weight: 500;
  color: var(--access-muted);
}
.login-mark {
  width: 34px;
  height: 34px;
  object-fit: contain;
}
.login-heading-icon {
  width: 40px;
  height: 40px;
  color: var(--access-accent);
  margin-bottom: 14px;
}
.login-heading-icon svg {
  width: 100%;
  height: 100%;
}
h1 {
  margin: 0;
  font-size: 28px;
  line-height: 1.3;
  letter-spacing: -0.025em;
}
.login-description {
  margin: 10px 0 26px;
  line-height: 1.6;
}
.login-footer {
  margin: 24px 0 0;
  padding-top: 20px;
  border-top: 1px solid var(--access-border);
  font-size: 12px;
  line-height: 1.7;
}
@media (max-width: 480px) {
  .login-screen {
    padding: 18px 14px;
  }
  .login-panel {
    padding: 26px 22px;
    border-radius: 20px;
  }
  h1 {
    font-size: 25px;
  }
}
</style>
