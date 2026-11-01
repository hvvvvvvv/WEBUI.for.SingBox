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
import Button from '@/components/Button/index.vue'
import Card from '@/components/Card/index.vue'
import Input from '@/components/Input/index.vue'

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
const submitFromInput = (event: KeyboardEvent, submit: () => unknown) => {
  if (event.isComposing || event.keyCode === 229 || !(event.target instanceof HTMLInputElement))
    return
  event.preventDefault()
  void submit()
}
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
  <div class="security-settings flex flex-col gap-8 pr-20 mb-8" :aria-busy="busy || statusLoading">
    <p v-if="statusLoading" role="status" class="px-8 py-12 text-12 opacity-60">
      {{ t('auth.checkingStatus') }}
    </p>
    <p v-if="error" role="alert" class="px-8 py-12 text-12 font-bold">{{ error }}</p>
    <p v-if="notice" role="status" class="px-8 py-12 text-12">{{ notice }}</p>
    <div v-if="!status && !statusLoading" class="px-8 py-12">
      <Button @click="loadStatus">{{ t('auth.retry') }}</Button>
    </div>

    <section v-if="recoveryCodes.length" aria-labelledby="recovery-heading">
      <div id="recovery-heading" class="px-8 py-12 text-18 font-bold">
        {{ t('auth.saveRecoveryCodes') }}
      </div>
      <Card class="recovery-card">
        <p class="security-description px-8 py-12 text-12 opacity-60">
          {{ t('auth.recoveryHint') }}
        </p>
        <div class="px-8 py-12 flex flex-col gap-8">
          <label for="recovery-codes" class="text-16 font-bold">{{
            t('auth.recoveryCodes')
          }}</label>
          <textarea
            id="recovery-codes"
            :value="recoveryCodes.join('\n')"
            rows="10"
            readonly
            spellcheck="false"
            class="recovery-codes rounded-4 px-8 py-6"
          />
        </div>
        <div class="security-actions px-8 py-12 flex flex-wrap items-center justify-end gap-8">
          <Button @click="downloadRecovery">{{ t('auth.downloadCodes') }}</Button>
        </div>
        <label class="security-check px-8 py-12 flex items-center gap-8 text-12">
          <input v-model="codesSaved" type="checkbox" />{{ t('auth.codesSaved') }}
        </label>
        <div class="security-actions px-8 py-12 flex flex-wrap justify-end gap-8">
          <Button type="primary" :disabled="!codesSaved" @click="finishRecovery">{{
            t('auth.finish')
          }}</Button>
        </div>
      </Card>
    </section>

    <template v-if="status">
      <section aria-labelledby="password-heading">
        <div id="password-heading" class="px-8 py-12 text-18 font-bold">
          {{ t('auth.passwordAccess') }}
        </div>
        <Card>
          <div class="px-8 py-12 text-16 font-bold">
            {{ t(status.enabled ? 'auth.authEnabled' : 'auth.authDisabled') }}
          </div>
          <p class="security-description px-8 pb-12 text-12 opacity-60">
            {{ t('auth.passwordHint') }}
          </p>
          <form
            @submit.prevent="savePassword()"
            @keydown.enter="submitFromInput($event, savePassword)"
          >
            <div
              v-if="status.enabled"
              class="security-setting px-8 py-12 flex items-center justify-between gap-8"
            >
              <label for="current-password" class="text-16 font-bold">{{
                t('auth.currentPassword')
              }}</label>
              <Input
                v-model="currentPassword"
                type="password"
                class="security-input"
                :disabled="passwordLocked"
                :input-attrs="{
                  id: 'current-password',
                  autocomplete: 'current-password',
                  required: true,
                }"
              />
            </div>
            <div class="security-setting px-8 py-12 flex items-center justify-between gap-8">
              <label for="new-password" class="text-16 font-bold">{{
                t('auth.newPassword')
              }}</label>
              <Input
                v-model="newPassword"
                type="password"
                class="security-input"
                :disabled="passwordLocked"
                :input-attrs="{
                  id: 'new-password',
                  autocomplete: 'new-password',
                  required: true,
                  'aria-describedby': 'password-rules',
                }"
              />
            </div>
            <div class="security-setting px-8 py-12 flex items-center justify-between gap-8">
              <label for="confirm-password" class="text-16 font-bold">{{
                t('auth.confirmSecret')
              }}</label>
              <Input
                v-model="confirmPassword"
                type="password"
                class="security-input"
                :disabled="passwordLocked"
                :input-attrs="{
                  id: 'confirm-password',
                  autocomplete: 'new-password',
                  required: true,
                }"
              />
            </div>
            <p id="password-rules" class="security-description px-8 pb-12 text-12 opacity-60">
              {{ t('auth.passwordRules') }}
            </p>
            <template v-if="status.totp_enabled">
              <div class="security-setting px-8 py-12 flex items-center justify-between gap-8">
                <label for="password-code" class="text-16 font-bold">{{
                  t('auth.verificationCode')
                }}</label>
                <Input
                  v-model="passwordCode"
                  class="security-input"
                  :disabled="passwordLocked"
                  :input-attrs="{
                    id: 'password-code',
                    autocomplete: 'one-time-code',
                    autocapitalize: 'off',
                    spellcheck: false,
                    required: true,
                    'aria-describedby': 'password-code-hint',
                  }"
                />
              </div>
              <p id="password-code-hint" class="security-description px-8 pb-12 text-12 opacity-60">
                {{ t('auth.codeHint') }}
              </p>
            </template>
            <div class="security-actions px-8 py-12 flex flex-wrap justify-end gap-8">
              <Button
                v-if="status.enabled && !status.totp_enabled"
                :disabled="passwordLocked"
                @click="showClear = !showClear"
                >{{ t('auth.clearAuth') }}</Button
              >
              <Button
                type="primary"
                :disabled="
                  passwordLocked ||
                  !newPassword ||
                  !confirmPassword ||
                  (status.enabled && !currentPassword) ||
                  (status.totp_enabled && !passwordCode)
                "
                @click="savePassword()"
                >{{ t(status.enabled ? 'auth.changePassword' : 'auth.setPassword') }}</Button
              >
            </div>
          </form>
          <div v-if="showClear" class="security-danger">
            <p class="security-description px-8 py-12 text-12">{{ t('auth.clearAuthConfirm') }}</p>
            <label class="security-check px-8 py-12 flex items-center gap-8 text-12"
              ><input v-model="clearConfirmed" type="checkbox" :disabled="passwordLocked" />{{
                t('auth.clearConfirmLabel')
              }}</label
            >
            <div class="security-actions px-8 py-12 flex flex-wrap justify-end gap-8">
              <Button
                type="primary"
                :disabled="passwordLocked || !clearConfirmed || !currentPassword"
                @click="savePassword(true)"
                >{{ t('auth.confirmClear') }}</Button
              >
            </div>
          </div>
        </Card>
      </section>

      <section aria-labelledby="totp-heading">
        <div id="totp-heading" class="px-8 py-12 text-18 font-bold">{{ t('auth.totpTitle') }}</div>
        <Card>
          <div class="px-8 py-12 text-16 font-bold">
            {{ t(status.totp_enabled ? 'auth.totpEnabled' : 'auth.totpOptional') }}
          </div>
          <p class="security-description px-8 pb-12 text-12 opacity-60">{{ t('auth.totpHint') }}</p>
          <p class="security-description px-8 pb-12 text-12 opacity-60">{{ t('auth.timeHint') }}</p>
          <p v-if="!status.enabled" class="px-8 py-12 text-12">{{ t('auth.passwordFirst') }}</p>
          <template v-else-if="!status.totp_enabled">
            <form
              v-if="!pending"
              @submit.prevent="startEnrollment"
              @keydown.enter="submitFromInput($event, startEnrollment)"
            >
              <div class="security-setting px-8 py-12 flex items-center justify-between gap-8">
                <label for="totp-password" class="text-16 font-bold">{{
                  t('auth.currentPassword')
                }}</label>
                <Input
                  v-model="totpPassword"
                  type="password"
                  class="security-input"
                  :disabled="locked"
                  :input-attrs="{
                    id: 'totp-password',
                    autocomplete: 'current-password',
                    required: true,
                  }"
                />
              </div>
              <div class="security-actions px-8 py-12 flex flex-wrap justify-end gap-8">
                <Button
                  type="primary"
                  :disabled="locked || !totpPassword"
                  @click="startEnrollment"
                  >{{ t('auth.startTotp') }}</Button
                >
              </div>
            </form>
            <div v-else>
              <div class="px-8 py-12 text-16 font-bold">{{ t('auth.scanQr') }}</div>
              <p class="security-description px-8 pb-12 text-12 opacity-60">
                {{ t('auth.qrPrivacy') }}
              </p>
              <div class="px-8 py-12">
                <canvas ref="qrCanvas" class="totp-qr" role="img" :aria-label="t('auth.qrLabel')" />
              </div>
              <p v-if="qrError" role="status" class="px-8 py-12 text-12">
                {{ t('auth.qrFailed') }}
              </p>
              <div class="security-setting px-8 py-12 flex items-center justify-between gap-8">
                <label for="totp-secret" class="text-16 font-bold">{{ t('auth.manualKey') }}</label>
                <Input
                  :model-value="pending.secret"
                  class="security-input"
                  :input-attrs="{
                    id: 'totp-secret',
                    readonly: true,
                    spellcheck: false,
                    autocomplete: 'off',
                  }"
                />
              </div>
              <p class="security-description px-8 pb-12 text-12 opacity-60">
                {{
                  t('auth.enrollmentExpires', {
                    time: new Date(pending.expires_at).toLocaleTimeString(),
                  })
                }}
              </p>
              <form
                @submit.prevent="confirmEnrollment"
                @keydown.enter="submitFromInput($event, confirmEnrollment)"
              >
                <div class="security-setting px-8 py-12 flex items-center justify-between gap-8">
                  <label for="totp-confirm-code" class="text-16 font-bold">{{
                    t('auth.confirmTotpCode')
                  }}</label>
                  <Input
                    v-model="totpCode"
                    class="security-input"
                    :disabled="locked"
                    :input-attrs="{
                      id: 'totp-confirm-code',
                      inputmode: 'numeric',
                      autocomplete: 'one-time-code',
                      pattern: '[0-9]{6}',
                      maxlength: 6,
                      required: true,
                    }"
                  />
                </div>
                <div class="security-actions px-8 py-12 flex flex-wrap justify-end gap-8">
                  <Button :disabled="busy" @click="cancelEnrollment">{{ t('auth.cancel') }}</Button>
                  <Button
                    type="primary"
                    :disabled="locked || !/^\d{6}$/.test(totpCode)"
                    @click="confirmEnrollment"
                    >{{ t('auth.enableTotp') }}</Button
                  >
                </div>
              </form>
            </div>
          </template>
          <template v-else>
            <p class="security-description px-8 pb-12 text-12 opacity-60">
              {{ t('auth.enabledHint') }}
            </p>
            <div
              v-if="action === 'none'"
              class="security-actions px-8 py-12 flex flex-wrap justify-end gap-8"
            >
              <Button :disabled="locked" @click="action = 'disable'">{{
                t('auth.disableTotp')
              }}</Button>
              <Button type="primary" :disabled="locked" @click="action = 'regenerate'">{{
                t('auth.regenerateCodes')
              }}</Button>
            </div>
            <form
              v-else
              @submit.prevent="confirmTotpAction"
              @keydown.enter="submitFromInput($event, confirmTotpAction)"
            >
              <div class="px-8 py-12 text-16 font-bold">
                {{ t(action === 'disable' ? 'auth.disableTotp' : 'auth.regenerateCodes') }}
              </div>
              <p class="security-description px-8 pb-12 text-12 opacity-60">
                {{ t(action === 'disable' ? 'auth.disableHint' : 'auth.regenerateHint') }}
              </p>
              <div class="security-setting px-8 py-12 flex items-center justify-between gap-8">
                <label for="totp-action-password" class="text-16 font-bold">{{
                  t('auth.currentPassword')
                }}</label>
                <Input
                  v-model="totpPassword"
                  type="password"
                  class="security-input"
                  :disabled="locked"
                  :input-attrs="{
                    id: 'totp-action-password',
                    autocomplete: 'current-password',
                    required: true,
                  }"
                />
              </div>
              <div class="security-setting px-8 py-12 flex items-center justify-between gap-8">
                <label for="totp-action-code" class="text-16 font-bold">{{
                  t('auth.verificationCode')
                }}</label>
                <Input
                  v-model="totpCode"
                  class="security-input"
                  :disabled="locked"
                  :input-attrs="{
                    id: 'totp-action-code',
                    autocomplete: 'one-time-code',
                    autocapitalize: 'off',
                    spellcheck: false,
                    required: true,
                    'aria-describedby': 'totp-code-hint',
                  }"
                />
              </div>
              <p id="totp-code-hint" class="security-description px-8 pb-12 text-12 opacity-60">
                {{ t('auth.codeHint') }}
              </p>
              <div class="security-actions px-8 py-12 flex flex-wrap justify-end gap-8">
                <Button :disabled="busy" @click="resetCredentials">{{ t('auth.cancel') }}</Button>
                <Button
                  type="primary"
                  :disabled="locked || !totpPassword || !totpCode"
                  @click="confirmTotpAction"
                  >{{ t('auth.confirmAction') }}</Button
                >
              </div>
            </form>
          </template>
        </Card>
      </section>
    </template>
  </div>
</template>

<style scoped>
.security-settings,
.security-settings section,
.security-setting > * {
  min-width: 0;
}
.security-input {
  width: 280px;
  max-width: 100%;
  flex-shrink: 0;
}
.security-description {
  line-height: 1.6;
}
.security-check {
  line-height: 1.6;
}
.security-check input {
  flex-shrink: 0;
  accent-color: var(--primary-color);
}
.recovery-codes {
  width: 100%;
  box-sizing: border-box;
  resize: vertical;
  color: var(--input-color);
  background: var(--input-bg);
  border: 1px solid var(--primary-color);
  font-family: monospace;
  line-height: 1.6;
}
.totp-qr {
  display: block;
  width: 224px !important;
  max-width: 100%;
  height: auto !important;
}
.security-actions :deep(.gui-button) {
  white-space: normal;
}
@media (max-width: 700px) {
  .security-settings {
    padding-right: 8px;
  }
  .security-setting {
    align-items: stretch;
    flex-direction: column;
  }
  .security-input {
    width: 100%;
    min-width: 0;
    min-height: 44px;
    box-sizing: border-box;
  }
  .security-actions :deep(.gui-button) {
    flex: 1 1 140px;
    min-height: 44px;
  }
}
</style>
