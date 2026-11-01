<script lang="ts" setup>
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { loginAuthToken } from '@/bridge/http'
import { AuthError, getAuthStatus, type AuthStatus } from '@/api/auth'
import logo from '@/assets/logo'
import Button from '@/components/Button/index.vue'
import Card from '@/components/Card/index.vue'
import Input from '@/components/Input/index.vue'

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
const submitFromInput = (event: KeyboardEvent) => {
  if (event.isComposing || event.keyCode === 229 || !(event.target instanceof HTMLInputElement))
    return
  event.preventDefault()
  void handleLogin()
}
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
  <main class="login-screen flex items-center justify-center">
    <div class="login-content">
      <div class="flex flex-col items-center py-16">
        <img :src="logo" class="w-64" alt="" draggable="false" />
        <div class="text-24 font-bold mt-16">WEBUI for SingBox</div>
      </div>
      <div id="login-heading" class="px-8 py-12 text-18 font-bold">{{ t('auth.title') }}</div>
      <Card aria-labelledby="login-heading" :aria-busy="loading || statusLoading">
        <p v-if="statusLoading" role="status" class="px-8 py-12 text-12 opacity-60">
          {{ t('auth.checkingStatus') }}
        </p>
        <form @submit.prevent="handleLogin" @keydown.enter="submitFromInput">
          <template v-if="status?.enabled">
            <div class="login-setting px-8 py-12 flex items-center justify-between gap-8">
              <label for="login-password" class="text-16 font-bold">{{ t('auth.secret') }}</label>
              <Input
                v-model="password"
                :type="showPassword ? 'text' : 'password'"
                class="login-input"
                :disabled="loading"
                autofocus
                :input-attrs="{
                  id: 'login-password',
                  name: 'password',
                  autocomplete: 'current-password',
                  required: true,
                  'aria-invalid': !!error,
                  'aria-describedby': error ? 'login-error' : undefined,
                }"
              >
                <template #suffix>
                  <Button
                    type="text"
                    size="small"
                    :aria-label="t(showPassword ? 'auth.hidePassword' : 'auth.showPassword')"
                    :aria-pressed="showPassword"
                    @click="showPassword = !showPassword"
                    >{{ t(showPassword ? 'auth.hide' : 'auth.show') }}</Button
                  >
                </template>
              </Input>
            </div>
            <template v-if="status.totp_enabled">
              <div class="login-setting px-8 py-12 flex items-center justify-between gap-8">
                <label for="login-code" class="text-16 font-bold">{{
                  t('auth.verificationCode')
                }}</label>
                <Input
                  v-model="code"
                  class="login-input"
                  :disabled="loading"
                  :input-attrs="{
                    id: 'login-code',
                    name: 'verification-code',
                    autocomplete: 'one-time-code',
                    autocapitalize: 'off',
                    spellcheck: false,
                    required: true,
                    'aria-describedby': 'login-code-hint',
                  }"
                />
              </div>
              <p id="login-code-hint" class="login-hint px-8 pb-12 text-12 opacity-60">
                {{ t('auth.codeHint') }}
              </p>
            </template>
          </template>
          <p
            v-if="error"
            id="login-error"
            class="login-hint px-8 py-12 text-12 font-bold"
            role="alert"
          >
            {{ error }}
          </p>
          <div class="px-8 py-12 flex justify-end">
            <Button v-if="!status && !statusLoading" type="primary" @click="loadStatus">{{
              t('auth.retry')
            }}</Button>
            <Button
              v-else
              type="primary"
              :loading="loading"
              :disabled="
                loading ||
                statusLoading ||
                !status ||
                (status.enabled && !password) ||
                (status.totp_enabled && !code)
              "
              @click="handleLogin"
              >{{ loading ? t('auth.verifying') : t('auth.login') }}</Button
            >
          </div>
        </form>
      </Card>
    </div>
  </main>
</template>

<style scoped>
.login-screen {
  min-height: 100dvh;
  width: 100%;
  box-sizing: border-box;
  padding: 24px 16px;
  color: var(--color);
  background: var(--app-background);
  overflow-y: auto;
}
.login-content {
  width: 480px;
  max-width: 100%;
  min-width: 0;
}
.login-setting > * {
  min-width: 0;
}
.login-input {
  width: 280px;
  max-width: 100%;
  flex-shrink: 0;
}
.login-hint {
  line-height: 1.6;
}
@media (max-width: 520px) {
  .login-setting {
    flex-direction: column;
    align-items: stretch;
  }
  .login-input {
    width: 100%;
    min-width: 0;
    min-height: 44px;
    box-sizing: border-box;
  }
  .login-screen :deep(.gui-button) {
    min-height: 44px;
    width: 100%;
  }
}
</style>
