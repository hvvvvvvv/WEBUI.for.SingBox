<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, useTemplateRef, watch } from 'vue'
import { useI18n } from 'vue-i18n'

import QRCode from 'qrcode'

import { BrowserOpenURL } from '@/bridge'
import { useRulesetsStore } from '@/stores'
import {
  buildProfileQRS,
  generateConfigViaRpc,
  prepareConfigForQRS,
  ProfileType,
  QRSConfigPreparationError,
  QRS_DEFAULT_FPS,
  QRS_DEFAULT_SLICE_SIZE,
  QRS_MAX_FPS,
  QRS_MAX_SLICE_SIZE,
  QRS_MIN_FPS,
  QRS_MIN_SLICE_SIZE,
  type QRSFrame,
} from '@/utils'

const props = defineProps<{
  profileId: string
  profileName: string
}>()

const emit = defineEmits<{
  close: []
}>()

const QRS_PROJECT_URL = 'https://github.com/qifi-dev/qrs'
const REBUILD_DELAY = 180

const { t } = useI18n()
const rulesetsStore = useRulesetsStore()
const canvas = useTemplateRef<HTMLCanvasElement>('canvas')

const fps = ref(QRS_DEFAULT_FPS)
const sliceSize = ref(QRS_DEFAULT_SLICE_SIZE)
const frames = ref<QRSFrame[]>([])
const currentFrame = ref(0)
const configText = ref<string>()
const loading = ref(true)
const encoding = ref(false)
const errorMessage = ref('')

const busy = computed(() => loading.value || encoding.value)

let playbackToken = 0
let playbackTimer: number | undefined
let rebuildTimer: number | undefined
let disposed = false

const describeError = (error: unknown) => {
  if (error instanceof QRSConfigPreparationError) {
    const reason = t(`profiles.qrs.rulesetError.${error.code}`, { detail: error.detail })
    return t('profiles.qrs.rulesetConversionFailed', {
      ruleSet: error.ruleSet,
      reason,
    })
  }
  if (error instanceof Error && error.message) return error.message
  return String(error)
}

const stopPlayback = () => {
  playbackToken++
  if (playbackTimer !== undefined) window.clearTimeout(playbackTimer)
  playbackTimer = undefined
}

const renderFrame = async (token: number) => {
  const frame = frames.value[currentFrame.value]
  const target = canvas.value
  if (!frame || !target || token !== playbackToken) return false

  try {
    await QRCode.toCanvas(target, frame.content, {
      errorCorrectionLevel: 'L',
      margin: 4,
      width: 512,
      color: {
        dark: '#000000ff',
        light: '#ffffffff',
      },
    })
  } catch {
    if (token === playbackToken) errorMessage.value = t('profiles.qrs.qrTooLarge')
    return false
  }
  return token === playbackToken
}

const scheduleNextFrame = (token: number) => {
  if (token !== playbackToken || frames.value.length === 0 || errorMessage.value) return

  playbackTimer = window.setTimeout(async () => {
    if (token !== playbackToken) return
    currentFrame.value = (currentFrame.value + 1) % frames.value.length
    if (await renderFrame(token)) scheduleNextFrame(token)
  }, 1000 / fps.value)
}

const startPlayback = async () => {
  stopPlayback()
  if (frames.value.length === 0 || errorMessage.value) return

  const token = playbackToken
  await nextTick()
  if (await renderFrame(token)) scheduleNextFrame(token)
}

const rebuildFrames = async () => {
  if (disposed || configText.value === undefined) return

  stopPlayback()
  encoding.value = true
  errorMessage.value = ''
  currentFrame.value = 0
  await nextTick()
  if (disposed) return

  try {
    const result = buildProfileQRS(
      {
        type: ProfileType.Local,
        name: props.profileName,
        config: configText.value,
      },
      { sliceSize: sliceSize.value },
    )
    frames.value = result.frames
  } catch (error) {
    frames.value = []
    errorMessage.value = t('profiles.qrs.generateFailed', { error: describeError(error) })
  } finally {
    encoding.value = false
  }

  await startPlayback()
}

const loadProfile = async () => {
  loading.value = true
  configText.value = undefined
  frames.value = []
  errorMessage.value = ''
  try {
    const [config] = await Promise.all([
      generateConfigViaRpc(props.profileId),
      rulesetsStore.setupRulesets(),
    ])
    if (disposed) return
    const resources = rulesetsStore.rulesets.map(({ id, type, format, path, url }) => ({
      id,
      type,
      format,
      path,
      url,
    }))
    const preparedConfig = await prepareConfigForQRS(config, {
      resources,
      loadSource: (resourceId) => rulesetsStore.getRulesetContent(resourceId),
    })
    if (disposed) return
    configText.value = JSON.stringify(preparedConfig, null, 2)
    await rebuildFrames()
  } catch (error) {
    stopPlayback()
    frames.value = []
    errorMessage.value = t('profiles.qrs.loadFailed', { error: describeError(error) })
  } finally {
    loading.value = false
  }
}

watch(fps, () => void startPlayback())
watch(sliceSize, () => {
  if (rebuildTimer !== undefined) window.clearTimeout(rebuildTimer)
  rebuildTimer = window.setTimeout(() => void rebuildFrames(), REBUILD_DELAY)
})

onMounted(() => void loadProfile())
onBeforeUnmount(() => {
  disposed = true
  stopPlayback()
  if (rebuildTimer !== undefined) window.clearTimeout(rebuildTimer)
})
</script>

<template>
  <div class="qrs-share flex flex-col gap-16 pb-16">
    <div class="qr-surface relative overflow-hidden">
      <canvas v-show="frames.length > 0 && !errorMessage" ref="canvas" />

      <div
        v-if="busy"
        class="qr-state absolute inset-0 flex flex-col items-center justify-center gap-8"
      >
        <Icon icon="loading" :size="28" class="rotation" />
        <span>{{ t(loading ? 'profiles.qrs.loading' : 'profiles.qrs.encoding') }}</span>
      </div>

      <div
        v-else-if="errorMessage"
        class="qr-state error-state absolute inset-0 flex flex-col items-center justify-center gap-8 p-20 text-center"
        role="alert"
      >
        <Icon icon="error" :size="32" />
        <span>{{ errorMessage }}</span>
      </div>
    </div>

    <div class="control-group flex flex-col gap-16" :class="{ disabled: !configText }">
      <label class="flex flex-col gap-6">
        <span class="flex items-center justify-between">
          <span>{{ t('profiles.qrs.fps') }}</span>
          <span class="control-value">{{ fps }} Hz</span>
        </span>
        <input
          v-model.number="fps"
          type="range"
          :min="QRS_MIN_FPS"
          :max="QRS_MAX_FPS"
          :disabled="!configText"
        />
      </label>

      <label class="flex flex-col gap-6">
        <span class="flex items-center justify-between">
          <span>{{ t('profiles.qrs.sliceSize') }}</span>
          <span class="control-value">{{ sliceSize }}</span>
        </span>
        <input
          v-model.number="sliceSize"
          type="range"
          :min="QRS_MIN_SLICE_SIZE"
          :max="QRS_MAX_SLICE_SIZE"
          :disabled="!configText"
        />
      </label>
    </div>

    <div class="actions grid grid-cols-2 gap-12">
      <Button type="normal" icon="messageInfo" @click="BrowserOpenURL(QRS_PROJECT_URL)">
        {{ t('profiles.qrs.whatIs') }}
      </Button>
      <Button type="primary" icon="close" @click="emit('close')">
        {{ t('common.close') }}
      </Button>
    </div>
  </div>
</template>

<style scoped lang="less">
.qrs-share {
  width: min(420px, calc(90vw - 32px));
  min-width: min(300px, calc(90vw - 32px));
}

.qr-surface {
  width: 100%;
  aspect-ratio: 1;
  background: #fff;
  border: 8px solid color-mix(in srgb, var(--modal-bg), #fff 16%);
  border-radius: 10px;

  canvas {
    display: block;
    width: 100% !important;
    height: 100% !important;
    background: #fff;
  }
}

.qr-state {
  color: var(--color);
  background: var(--input-bg);
}

.error-state {
  color: #ea6060;
}

.control-value {
  color: color-mix(in srgb, var(--color), transparent 28%);
  font-size: 12px;
}

.control-group.disabled {
  opacity: 0.6;
}

input[type='range'] {
  width: 100%;
  height: 20px;
  margin: 0;
  cursor: pointer;
  accent-color: var(--primary-color);
}

input[type='range']:disabled {
  cursor: not-allowed;
}

@media (max-width: 420px) {
  .actions {
    grid-template-columns: 1fr;
  }
}
</style>
