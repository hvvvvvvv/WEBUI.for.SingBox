<script lang="ts">
export const IS_IN_MODAL = 'IS_IN_MODAL'
</script>

<script setup lang="ts">
import { computed, markRaw, onUnmounted, provide, ref, useId, watch } from 'vue'

import { useBool } from '@/hooks'
import useI18n from '@/lang'
import { useAppStore } from '@/stores'
import { useAppSettingsStore } from '@/stores/appSettings'
import { useModalLayoutStore } from '@/stores/modalLayout'
import { message } from '@/utils'

export interface Props {
  title?: string
  footer?: boolean
  maxHeight?: string
  maxWidth?: string
  minWidth?: string
  minHeight?: string
  width?: string
  height?: string
  cancel?: boolean
  submit?: boolean
  cancelText?: string
  submitText?: string
  maskClosable?: boolean
  bodyScrollable?: boolean
  sideTab?: boolean
  class?: string
  toolbar?: {
    maximize?: boolean
    minimize?: boolean
    close?: boolean
  }
  onOk?: () => MaybePromise<boolean | void>
  onCancel?: () => MaybePromise<boolean | void>
  beforeClose?: (isOk: boolean) => MaybePromise<boolean | void>
  afterClose?: (isOk: boolean) => void
}

export interface Slots {
  default?: () => any
  title?: () => any
  toolbar?: () => any
  action?: () => any
  cancel?: () => any
  submit?: () => any
}

const slots = defineSlots<Slots>()
const titleId = `modal-title-${useId()}`
const tabId = `${titleId}-tab`

const props = withDefaults(defineProps<Props>(), {
  title: '',
  footer: true,
  maxHeight: '90',
  maxWidth: '90',
  minWidth: '60',
  minHeight: '',
  width: '',
  height: '',
  cancel: true,
  submit: true,
  cancelText: 'common.cancel',
  submitText: 'common.save',
  maskClosable: false,
  bodyScrollable: true,
  sideTab: undefined,
  class: undefined,
  toolbar: () => ({
    maximize: true,
    minimize: true,
  }),
  onOk: undefined,
  onCancel: undefined,
  beforeClose: undefined,
  afterClose: undefined,
})

const open = defineModel<boolean>('open', { default: false })

const cancelLoading = ref(false)
const submitLoading = ref(false)

const modalZindex = ref()
const appStore = useAppStore()
const settings = useAppSettingsStore()
const layout = useModalLayoutStore()
const [isMaximize, toggleMaximize] = useBool(false)
const tabEligible = computed(() => props.sideTab ?? props.toolbar.maximize !== false)
const isSide = computed(() => tabEligible.value && layout.splitActive)
const visible = computed(() => open.value && (!isSide.value || layout.activeTab === tabId))

const { t } = useI18n.global

const handleAction = async (isOk: boolean) => {
  if (cancelLoading.value || submitLoading.value) return
  const loading = isOk ? submitLoading : cancelLoading
  const action = isOk ? props.onOk : props.onCancel

  loading.value = true
  try {
    if (!((await action?.()) ?? true) || !((await props.beforeClose?.(isOk)) ?? true)) {
      return
    }
  } finally {
    loading.value = false
  }

  open.value = false
  props.afterClose?.(isOk)
}

const handleSubmit = () => handleAction(true)
const handleCancel = () => handleAction(false)

const onMaskClick = () => props.maskClosable && handleCancel()

const contentStyle = computed(() =>
  isSide.value
    ? {
        width: '100%',
        height: '100%',
        maxWidth: '100%',
        maxHeight: '100%',
        minWidth: '0',
        minHeight: '0',
      }
    : {
        maxHeight: props.maxHeight + '%',
        maxWidth: props.maxWidth + '%',
        minWidth: isMaximize.value ? '100%' : props.minWidth ? props.minWidth + '%' : '0',
        minHeight: isMaximize.value ? '100%' : props.minHeight ? props.minHeight + '%' : '0',
        width: props.width + '%',
        height: props.height + '%',
      },
)

let lastEscTime = 0
let closeMessage: () => void

const closeFn = () => {
  if (isMaximize.value && !isSide.value) {
    toggleMaximize()
    return
  }
  if (props.maskClosable) {
    handleCancel()
    return
  }
  const now = performance.now()
  if (now - lastEscTime < 1000) {
    handleCancel()
    lastEscTime = 0
    closeMessage?.()
  } else {
    const { destroy } = message.info('common.pressAgainToClose', 1_000)
    closeMessage = destroy
    lastEscTime = now
  }
}

const removeFromStack = () => {
  const index = appStore.modalStack.indexOf(closeFn)
  if (index !== -1) appStore.modalStack.splice(index, 1)
}
const activate = () => {
  if (tabEligible.value) layout.activeTab = tabId
  modalZindex.value = ++appStore.modalZIndexCounter
  removeFromStack()
  appStore.modalStack.push(closeFn)
}
const tab = markRaw({
  id: tabId,
  title: () => props.title || 'settings.modalTabs',
  activate,
  close: handleCancel,
})
watch(
  () => open.value && tabEligible.value,
  (shown) => {
    if (shown) layout.register(tab)
    else layout.unregister(tabId)
  },
  { immediate: true, flush: 'post' },
)
watch(
  open,
  (v) => {
    if (v) {
      activate()
    } else {
      closeMessage?.()
      removeFromStack()
    }
  },
  { immediate: true },
)
onUnmounted(() => {
  layout.unregister(tabId)
  removeFromStack()
  closeMessage?.()
})

provide('cancel', handleCancel)
provide('submit', handleSubmit)
provide(IS_IN_MODAL, true)
</script>

<template>
  <Teleport :to="isSide ? '#app-modal-content' : 'body'">
    <Transition
      :name="tabEligible && settings.app.debugModalSideBySide ? 'modal-tab' : 'modal'"
      :duration="200"
    >
      <div
        v-if="open"
        v-show="visible"
        :aria-hidden="!visible || undefined"
        :class="{ 'is-side': isSide }"
        :style="{ zIndex: modalZindex }"
        class="gui-modal-mask fixed inset-0 flex items-center justify-center backdrop-blur-sm"
        @click.self="onMaskClick"
      >
        <div
          role="dialog"
          :aria-modal="isSide ? undefined : true"
          :aria-labelledby="title && !slots.title ? titleId : undefined"
          :aria-label="title && slots.title ? t(title) : undefined"
          :style="contentStyle"
          :class="props.class"
          class="gui-modal-modal transition duration-200 flex flex-col rounded-8 shadow"
        >
          <div
            v-if="title || slots.title || slots.toolbar"
            class="flex items-center p-16"
            @dblclick.self="!isSide && toggleMaximize()"
          >
            <slot name="title">
              <div v-if="title" :id="titleId" class="font-bold">{{ t(title) }}</div>
            </slot>
            <div class="ml-auto flex items-center">
              <slot name="toolbar"></slot>
              <Button
                v-if="tabEligible && layout.wideScreen"
                type="text"
                icon="maximize2"
                :aria-label="t('settings.debugModalSideBySide')"
                :aria-pressed="settings.app.debugModalSideBySide"
                @click="settings.app.debugModalSideBySide = !settings.app.debugModalSideBySide"
              />
              <Button v-if="toolbar.maximize && !isSide" type="text" @click="toggleMaximize">
                <Icon
                  :class="{ maximize: isMaximize }"
                  icon="arrowDown"
                  class="maximize-normal origin-center duration-200"
                />
              </Button>
            </div>
          </div>
          <div
            class="flex-1 min-h-0 mx-16"
            :class="bodyScrollable ? 'overflow-auto' : 'overflow-hidden'"
          >
            <slot></slot>
          </div>
          <div v-if="footer" class="flex items-center justify-end py-8 px-16 gap-8">
            <slot name="action"></slot>
            <slot name="cancel">
              <Button
                v-if="cancel"
                :loading="cancelLoading"
                :type="maskClosable ? 'text' : 'normal'"
                @click="handleCancel"
              >
                {{ t(cancelText) }}
              </Button>
            </slot>
            <slot name="submit">
              <Button v-if="submit" :loading="submitLoading" type="primary" @click="handleSubmit">
                {{ t(submitText) }}
              </Button>
            </slot>
          </div>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>

<style lang="less" scoped>
.gui-modal-mask.is-side {
  position: absolute;
  background: transparent;
  backdrop-filter: none;
  .gui-modal-modal {
    box-shadow: none;
    border-radius: 0;
  }
}
.gui-modal-modal {
  min-width: 0;
}
.modal-tab-enter-active .gui-modal-modal {
  transition: opacity 180ms ease-out;
}
.modal-tab-enter-from .gui-modal-modal {
  opacity: 0;
}
.modal-tab-leave-active {
  visibility: hidden;
  pointer-events: none;
}
.modal-enter-active .gui-modal-modal,
.modal-leave-active .gui-modal-modal {
  transition:
    transform 0.2s ease-in-out,
    opacity 0.2s ease-in-out;
}

.modal-enter-from .gui-modal-modal,
.modal-leave-to .gui-modal-modal {
  opacity: 0;
  transform: scale(0);
}

.gui-modal-mask {
  background-color: var(--modal-mask-bg);

  .gui-modal-modal {
    background-color: var(--modal-bg);
  }
}
.maximize-normal {
  transform: rotate(-180deg);
}
.maximize {
  transform: rotate(0deg);
}
</style>
