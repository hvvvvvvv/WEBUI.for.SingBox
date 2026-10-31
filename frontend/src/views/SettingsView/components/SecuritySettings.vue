<script setup lang="ts">
import {
  computed,
  nextTick,
  onActivated,
  onBeforeUnmount,
  onDeactivated,
  onMounted,
  ref,
} from 'vue'
import { useI18n } from 'vue-i18n'
import QRCode from 'qrcode'
import {
  AuthError,
  beginTotpSetup,
  cancelTotpSetup,
  disableTotp,
  enableTotp,
  getAuthStatus,
  regenerateRecoveryCodes,
  replaceAuthSession,
  setAccessPassword,
  type AuthStatus,
  type TotpEnrollment,
} from '@/api/auth'
import { initWebSocket } from '@/bridge/ws'
import '@/assets/styles/access.css'

const { t } = useI18n()
const status = ref<AuthStatus | null>(null)
const statusLoading = ref(false)
const busy = ref(false)
const error = ref('')
const notice = ref('')
const currentPassword = ref('')
const newPassword = ref('')
const confirmPassword = ref('')
const passwordCode = ref('')
const clearConfirmed = ref(false)
const showClear = ref(false)
const totpPassword = ref('')
const totpCode = ref('')
const pending = ref<TotpEnrollment | null>(null)
const qrCanvas = ref<HTMLCanvasElement | null>(null)
const qrError = ref(false)
const action = ref<'none' | 'disable' | 'regenerate'>('none')
const recoveryCodes = ref<string[]>([])
const codesSaved = ref(false)
const setupStarting = ref(false)
let epoch = 0
let active = true
let expiryTimer: ReturnType<typeof setTimeout> | undefined
const locked = computed(() => busy.value || statusLoading.value || recoveryCodes.value.length > 0)
const passwordLocked = computed(() => locked.value || !!pending.value)
const validNewPassword = computed(
  () =>
    Array.from(newPassword.value).length >= 15 &&
    new TextEncoder().encode(newPassword.value).length <= 1024,
)

const describeError = (value: unknown) => {
  if (value instanceof AuthError && (value.status === 401 || value.kind === 'session'))
    return t('auth.sessionExpired')
  if (value instanceof AuthError && (value.status === 429 || value.kind === 'rate_limited'))
    return t('auth.rateLimited')
  if (value instanceof AuthError && value.kind === 'credentials') return t('auth.invalidSecret')
  return value instanceof AuthError ? value.message : t('auth.requestFailed')
}
const loadStatus = async () => {
  const requestEpoch = epoch
  statusLoading.value = true
  error.value = ''
  try {
    const result = await getAuthStatus()
    if (active && requestEpoch === epoch) status.value = result
  } catch (value) {
    if (active && requestEpoch === epoch) error.value = describeError(value)
  } finally {
    if (active && requestEpoch === epoch) statusLoading.value = false
  }
}
const adoptToken = (token: string, enabled: boolean) => {
  replaceAuthSession(token, enabled)
  initWebSocket(token)
}
const resetCredentials = () => {
  currentPassword.value = ''
  newPassword.value = ''
  confirmPassword.value = ''
  passwordCode.value = ''
  totpPassword.value = ''
  totpCode.value = ''
  clearConfirmed.value = false
  showClear.value = false
  action.value = 'none'
}
const clearEnrollment = () => {
  clearTimeout(expiryTimer)
  expiryTimer = undefined
  pending.value = null
  if (qrCanvas.value) qrCanvas.value.width = 0
  qrError.value = false
  totpCode.value = ''
}
const cancelEnrollment = async () => {
  if (busy.value) return
  busy.value = true
  error.value = ''
  clearEnrollment()
  try {
    await cancelTotpSetup()
  } catch (value) {
    error.value = describeError(value)
  } finally {
    busy.value = false
    totpPassword.value = ''
  }
}
const savePassword = async (clear = false) => {
  if (passwordLocked.value || !status.value) return
  error.value = ''
  notice.value = ''
  if (clear) {
    if (!clearConfirmed.value || status.value.totp_enabled || !status.value.enabled) return
  } else {
    if (!validNewPassword.value) {
      error.value = t('auth.passwordRules')
      return
    }
    if (newPassword.value !== confirmPassword.value) {
      error.value = t('auth.mismatch')
      return
    }
  }
  if (status.value.enabled && !currentPassword.value) return
  if (status.value.totp_enabled && !passwordCode.value) return
  const requestEpoch = epoch
  busy.value = true
  try {
    const token = await setAccessPassword(
      clear ? '' : newPassword.value,
      currentPassword.value,
      passwordCode.value,
    )
    adoptToken(token, !clear)
    if (active && requestEpoch === epoch) {
      status.value = { enabled: !clear, totp_enabled: status.value.totp_enabled }
      resetCredentials()
      notice.value = t(clear ? 'auth.clearSuccess' : 'auth.updateSuccess')
    }
  } catch (value) {
    if (active && requestEpoch === epoch) error.value = describeError(value)
  } finally {
    passwordCode.value = ''
    busy.value = false
  }
}
const startEnrollment = async () => {
  if (locked.value || !totpPassword.value || !status.value?.enabled || status.value.totp_enabled)
    return
  const requestEpoch = epoch
  busy.value = true
  setupStarting.value = true
  error.value = ''
  notice.value = ''
  try {
    const result = await beginTotpSetup(totpPassword.value)
    if (!active || requestEpoch !== epoch) {
      await cancelTotpSetup()
      return
    }
    const expiresIn = Date.parse(result.expires_at) - Date.now()
    if (!(expiresIn > 0) || !result.secret || !result.otpauth_uri.startsWith('otpauth://totp/')) {
      await cancelTotpSetup()
      error.value = t('auth.setupExpired')
      return
    }
    pending.value = result
    expiryTimer = setTimeout(
      () => {
        clearEnrollment()
        error.value = t('auth.setupExpired')
        void cancelTotpSetup().catch(() => undefined)
      },
      Math.min(expiresIn, 2_147_483_647),
    )
    await nextTick()
    if (qrCanvas.value) {
      try {
        await QRCode.toCanvas(qrCanvas.value, result.otpauth_uri, {
          width: 224,
          margin: 3,
          errorCorrectionLevel: 'M',
          color: { dark: '#101c2fff', light: '#ffffffff' },
        })
      } catch {
        if (active && requestEpoch === epoch) qrError.value = true
      }
    }
  } catch (value) {
    if (active && requestEpoch === epoch) error.value = describeError(value)
  } finally {
    totpPassword.value = ''
    setupStarting.value = false
    busy.value = false
  }
}
const confirmEnrollment = async () => {
  if (locked.value || !pending.value || !/^\d{6}$/.test(totpCode.value)) return
  const requestEpoch = epoch
  busy.value = true
  error.value = ''
  try {
    const result = await enableTotp(totpCode.value)
    adoptToken(result.token, true)
    clearEnrollment()
    if (active && requestEpoch === epoch) {
      status.value = { enabled: true, totp_enabled: true }
      recoveryCodes.value = result.recovery_codes
      codesSaved.value = false
      resetCredentials()
      notice.value = t('auth.totpEnabled')
    }
  } catch (value) {
    if (active && requestEpoch === epoch) error.value = describeError(value)
  } finally {
    totpCode.value = ''
    busy.value = false
  }
}
const confirmTotpAction = async () => {
  if (
    locked.value ||
    !status.value?.totp_enabled ||
    !totpPassword.value ||
    !totpCode.value ||
    action.value === 'none'
  )
    return
  const requestedAction = action.value
  const requestEpoch = epoch
  busy.value = true
  error.value = ''
  notice.value = ''
  try {
    if (requestedAction === 'disable') {
      const token = await disableTotp(totpPassword.value, totpCode.value)
      adoptToken(token, true)
      if (active && requestEpoch === epoch) {
        status.value = { enabled: true, totp_enabled: false }
        notice.value = t('auth.totpDisabled')
      }
    } else {
      const result = await regenerateRecoveryCodes(totpPassword.value, totpCode.value)
      adoptToken(result.token, true)
      if (active && requestEpoch === epoch) {
        recoveryCodes.value = result.recovery_codes
        codesSaved.value = false
      }
    }
    resetCredentials()
  } catch (value) {
    if (active && requestEpoch === epoch) error.value = describeError(value)
  } finally {
    totpCode.value = ''
    busy.value = false
  }
}
const finishRecovery = () => {
  if (!codesSaved.value) return
  recoveryCodes.value = []
  codesSaved.value = false
}
const downloadRecovery = () => {
  const url = URL.createObjectURL(
    new Blob([recoveryCodes.value.join('\n') + '\n'], { type: 'text/plain;charset=utf-8' }),
  )
  const link = document.createElement('a')
  link.href = url
  link.download = 'webui-recovery-codes.txt'
  link.click()
  setTimeout(() => URL.revokeObjectURL(url), 0)
}
const beforeUnload = (event: BeforeUnloadEvent) => {
  if (!recoveryCodes.value.length) return
  event.preventDefault()
  event.returnValue = ''
}
const cleanup = () => {
  if (!active) return
  active = false
  epoch++
  const hasPending = pending.value || setupStarting.value
  clearEnrollment()
  resetCredentials()
  recoveryCodes.value = []
  codesSaved.value = false
  notice.value = ''
  error.value = ''
  window.removeEventListener('beforeunload', beforeUnload)
  if (hasPending) void cancelTotpSetup().catch(() => undefined)
}
onMounted(() => {
  window.addEventListener('beforeunload', beforeUnload)
  void loadStatus()
})
onActivated(() => {
  if (active) return
  active = true
  window.addEventListener('beforeunload', beforeUnload)
  void loadStatus()
})
onDeactivated(cleanup)
onBeforeUnmount(cleanup)
</script>

<template>
  <div class="access-colors security-settings" :aria-busy="busy || statusLoading">
    <header>
      <h1>{{ t('auth.security') }}</h1>
      <p class="access-muted">{{ t('auth.securityHint') }}</p>
    </header>
    <p v-if="statusLoading" role="status" class="access-muted">{{ t('auth.checkingStatus') }}</p>
    <p v-if="error" role="alert" class="access-error security-feedback">{{ error }}</p>
    <p v-if="notice" role="status" class="access-success security-feedback">{{ notice }}</p>
    <button v-if="!status && !statusLoading" class="access-button" @click="loadStatus">
      {{ t('auth.retry') }}
    </button>
    <section
      v-if="recoveryCodes.length"
      class="security-card recovery-card"
      aria-labelledby="recovery-heading"
    >
      <h2 id="recovery-heading">{{ t('auth.saveRecoveryCodes') }}</h2>
      <p class="access-muted">{{ t('auth.recoveryHint') }}</p>
      <label class="access-field" for="recovery-codes"
        ><span>{{ t('auth.recoveryCodes') }}</span
        ><textarea
          id="recovery-codes"
          :value="recoveryCodes.join('\n')"
          rows="10"
          readonly
          spellcheck="false"
        />
      </label>
      <div class="access-actions">
        <button class="access-button" @click="downloadRecovery">
          {{ t('auth.downloadCodes') }}
        </button>
      </div>
      <label class="security-check"
        ><input v-model="codesSaved" type="checkbox" />{{ t('auth.codesSaved') }}</label
      >
      <button class="access-button primary" :disabled="!codesSaved" @click="finishRecovery">
        {{ t('auth.finish') }}
      </button>
    </section>
    <template v-if="status">
      <section class="security-card" aria-labelledby="password-heading">
        <div class="security-card-header">
          <h2 id="password-heading">{{ t('auth.passwordAccess') }}</h2>
          <span class="security-badge">{{
            t(status.enabled ? 'auth.authEnabled' : 'auth.authDisabled')
          }}</span>
        </div>
        <p class="access-muted">{{ t('auth.passwordHint') }}</p>
        <form class="access-form" @submit.prevent="savePassword()">
          <label v-if="status.enabled" class="access-field" for="current-password"
            ><span>{{ t('auth.currentPassword') }}</span
            ><input
              id="current-password"
              v-model="currentPassword"
              type="password"
              autocomplete="current-password"
              :disabled="passwordLocked"
              required
          /></label>
          <div class="security-field-grid">
            <label class="access-field" for="new-password"
              ><span>{{ t('auth.newPassword') }}</span
              ><input
                id="new-password"
                v-model="newPassword"
                type="password"
                autocomplete="new-password"
                :disabled="passwordLocked"
                required
                aria-describedby="password-rules"
            /></label>
            <label class="access-field" for="confirm-password"
              ><span>{{ t('auth.confirmSecret') }}</span
              ><input
                id="confirm-password"
                v-model="confirmPassword"
                type="password"
                autocomplete="new-password"
                :disabled="passwordLocked"
                required
            /></label>
          </div>
          <small id="password-rules" class="access-muted">{{ t('auth.passwordRules') }}</small>
          <label v-if="status.totp_enabled" class="access-field" for="password-code"
            ><span>{{ t('auth.verificationCode') }}</span
            ><input
              id="password-code"
              v-model="passwordCode"
              type="text"
              autocomplete="one-time-code"
              autocapitalize="off"
              spellcheck="false"
              :disabled="passwordLocked"
              required
            /><small class="access-muted">{{ t('auth.codeHint') }}</small></label
          >
          <div class="access-actions">
            <button
              type="submit"
              class="access-button primary"
              :disabled="
                passwordLocked ||
                !newPassword ||
                !confirmPassword ||
                (status.enabled && !currentPassword) ||
                (status.totp_enabled && !passwordCode)
              "
            >
              {{ t(status.enabled ? 'auth.changePassword' : 'auth.setPassword') }}</button
            ><button
              v-if="status.enabled && !status.totp_enabled"
              type="button"
              class="access-button danger"
              :disabled="passwordLocked"
              @click="showClear = !showClear"
            >
              {{ t('auth.clearAuth') }}
            </button>
          </div>
        </form>
        <div v-if="showClear" class="security-danger">
          <p>{{ t('auth.clearAuthConfirm') }}</p>
          <label class="security-check"
            ><input v-model="clearConfirmed" type="checkbox" :disabled="passwordLocked" />{{
              t('auth.clearConfirmLabel')
            }}</label
          >
          <button
            class="access-button danger"
            :disabled="passwordLocked || !clearConfirmed || !currentPassword"
            @click="savePassword(true)"
          >
            {{ t('auth.confirmClear') }}
          </button>
        </div>
      </section>
      <section class="security-card" aria-labelledby="totp-heading">
        <div class="security-card-header">
          <h2 id="totp-heading">{{ t('auth.totpTitle') }}</h2>
          <span class="security-badge">{{
            t(status.totp_enabled ? 'auth.totpEnabled' : 'auth.totpOptional')
          }}</span>
        </div>
        <p class="access-muted">{{ t('auth.totpHint') }}</p>
        <p class="security-tip">{{ t('auth.timeHint') }}</p>
        <p v-if="!status.enabled" class="access-muted">{{ t('auth.passwordFirst') }}</p>
        <template v-else-if="!status.totp_enabled">
          <form v-if="!pending" class="access-form" @submit.prevent="startEnrollment">
            <label class="access-field" for="totp-password"
              ><span>{{ t('auth.currentPassword') }}</span
              ><input
                id="totp-password"
                v-model="totpPassword"
                type="password"
                autocomplete="current-password"
                :disabled="locked"
                required
            /></label>
            <div class="access-actions">
              <button
                class="access-button primary"
                type="submit"
                :disabled="locked || !totpPassword"
              >
                {{ t('auth.startTotp') }}
              </button>
            </div>
          </form>
          <div v-else class="totp-enrollment">
            <h3>{{ t('auth.scanQr') }}</h3>
            <p class="access-muted">{{ t('auth.qrPrivacy') }}</p>
            <canvas ref="qrCanvas" class="totp-qr" role="img" :aria-label="t('auth.qrLabel')" />
            <p v-if="qrError" role="status" class="access-muted">{{ t('auth.qrFailed') }}</p>
            <label class="access-field" for="totp-secret"
              ><span>{{ t('auth.manualKey') }}</span
              ><input
                id="totp-secret"
                :value="pending.secret"
                type="text"
                readonly
                spellcheck="false"
                autocomplete="off"
            /></label>
            <p class="access-muted">
              {{
                t('auth.enrollmentExpires', {
                  time: new Date(pending.expires_at).toLocaleTimeString(),
                })
              }}
            </p>
            <form class="access-form" @submit.prevent="confirmEnrollment">
              <label class="access-field" for="totp-confirm-code"
                ><span>{{ t('auth.confirmTotpCode') }}</span
                ><input
                  id="totp-confirm-code"
                  v-model="totpCode"
                  type="text"
                  inputmode="numeric"
                  autocomplete="one-time-code"
                  pattern="[0-9]{6}"
                  maxlength="6"
                  required
                  :disabled="locked"
              /></label>
              <div class="access-actions">
                <button
                  class="access-button primary"
                  type="submit"
                  :disabled="locked || !/^\d{6}$/.test(totpCode)"
                >
                  {{ t('auth.enableTotp') }}</button
                ><button
                  class="access-button"
                  type="button"
                  :disabled="busy"
                  @click="cancelEnrollment"
                >
                  {{ t('auth.cancel') }}
                </button>
              </div>
            </form>
          </div>
        </template>
        <template v-else>
          <p class="access-muted">{{ t('auth.enabledHint') }}</p>
          <div v-if="action === 'none'" class="access-actions">
            <button class="access-button" :disabled="locked" @click="action = 'regenerate'">
              {{ t('auth.regenerateCodes') }}</button
            ><button class="access-button danger" :disabled="locked" @click="action = 'disable'">
              {{ t('auth.disableTotp') }}
            </button>
          </div>
          <form v-else class="access-form" @submit.prevent="confirmTotpAction">
            <h3>{{ t(action === 'disable' ? 'auth.disableTotp' : 'auth.regenerateCodes') }}</h3>
            <p class="access-muted">
              {{ t(action === 'disable' ? 'auth.disableHint' : 'auth.regenerateHint') }}
            </p>
            <label class="access-field" for="totp-action-password"
              ><span>{{ t('auth.currentPassword') }}</span
              ><input
                id="totp-action-password"
                v-model="totpPassword"
                type="password"
                autocomplete="current-password"
                :disabled="locked"
                required
            /></label>
            <label class="access-field" for="totp-action-code"
              ><span>{{ t('auth.verificationCode') }}</span
              ><input
                id="totp-action-code"
                v-model="totpCode"
                type="text"
                autocomplete="one-time-code"
                autocapitalize="off"
                spellcheck="false"
                :disabled="locked"
                required
              /><small class="access-muted">{{ t('auth.codeHint') }}</small></label
            >
            <div class="access-actions">
              <button
                class="access-button"
                :class="action === 'disable' ? 'danger' : 'primary'"
                type="submit"
                :disabled="locked || !totpPassword || !totpCode"
              >
                {{ t('auth.confirmAction') }}</button
              ><button
                class="access-button"
                type="button"
                :disabled="busy"
                @click="resetCredentials"
              >
                {{ t('auth.cancel') }}
              </button>
            </div>
          </form>
        </template>
      </section>
    </template>
  </div>
</template>

<style scoped>
.security-settings {
  padding: 8px 16px 24px 8px;
  max-width: 880px;
  box-sizing: border-box;
}
h1 {
  font-size: 22px;
  margin: 8px 0;
}
h2 {
  font-size: 17px;
  margin: 0;
}
h3 {
  font-size: 15px;
  margin: 0;
}
p {
  line-height: 1.7;
  font-size: 13px;
}
.security-card {
  padding: 22px;
  border: 1px solid var(--access-border);
  background: var(--access-card);
  border-radius: 14px;
  margin-top: 18px;
}
.security-card-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 10px;
}
.security-badge {
  font-size: 11px;
  padding: 4px 8px;
  border-radius: 20px;
  background: var(--access-glow);
  color: var(--access-accent);
}
.security-field-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 16px;
}
.security-feedback {
  padding: 12px 0;
}
.security-tip {
  padding: 12px;
  border-left: 3px solid var(--access-accent);
  background: var(--access-glow);
  border-radius: 0 7px 7px 0;
}
.security-check {
  display: flex;
  align-items: start;
  gap: 10px;
  margin: 16px 0;
  font-size: 13px;
  line-height: 1.6;
}
.security-check input {
  flex-shrink: 0;
  width: 17px;
  height: 17px;
  margin: 2px 0;
  accent-color: var(--access-accent);
}
.security-danger {
  margin-top: 16px;
  padding-top: 12px;
  border-top: 1px solid var(--access-border);
  color: var(--access-danger);
}
.totp-enrollment {
  display: flex;
  flex-direction: column;
  gap: 16px;
}
.totp-enrollment p {
  margin: 0;
}
.totp-qr {
  max-width: 100%;
  height: auto !important;
  width: 224px !important;
  align-self: start;
  border-radius: 10px;
}
.recovery-card {
  border-color: var(--access-accent);
}
.recovery-card textarea,
#totp-secret {
  font-family: ui-monospace, monospace;
}
.recovery-card .access-actions {
  margin-top: 16px;
}
@media (max-width: 650px) {
  .security-settings {
    padding: 8px 8px 24px 0;
  }
  .security-card {
    padding: 16px;
  }
  .security-field-grid {
    grid-template-columns: 1fr;
  }
}
</style>
