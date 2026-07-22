<script setup lang="ts">
import { ref, inject, h } from 'vue'
import { useI18n } from 'vue-i18n'

import { isResourceConflict, isResourceNotFound, useSubscribesStore } from '@/stores'
import { deepClone, message } from '@/utils'

import Button from '@/components/Button/index.vue'
import ResourceConflictNotice from '@/components/ResourceConflictNotice/index.vue'

import type { Subscription } from '@/types/app'

interface Props {
  id: string
}

const props = defineProps<Props>()

const loading = ref(false)
const reloading = ref(false)
const conflict = ref<'changed' | 'deleted' | null>(null)
const subscribe = ref<Subscription>()
const code = ref('')

const { t } = useI18n()
const subscribeStore = useSubscribesStore()
let baseRevision = subscribeStore.getSubscribeRevision(props.id)

const handleCancel = inject('cancel') as any
const handleSubmit = inject('submit') as any

const handleSave = async () => {
  if (!subscribe.value) return
  loading.value = true
  try {
    subscribe.value.script = code.value
    await subscribeStore.editSubscribe(props.id, subscribe.value, baseRevision)
    await handleSubmit()
  } catch (error: any) {
    if (isResourceConflict(error) || isResourceNotFound(error)) {
      await subscribeStore.setupSubscribes().catch(() => undefined)
      conflict.value = isResourceNotFound(error) ? 'deleted' : 'changed'
    } else {
      message.error(error)
    }
  }
  loading.value = false
}

const s = subscribeStore.getSubscribeById(props.id)
if (s) {
  subscribe.value = deepClone(s)
  code.value = s.script
}

const loadLatest = async () => {
  reloading.value = true
  try {
    await subscribeStore.setupSubscribes()
    const latest = subscribeStore.getSubscribeById(props.id)
    if (!latest) {
      conflict.value = 'deleted'
      return
    }
    subscribe.value = deepClone(latest)
    code.value = latest.script
    baseRevision = subscribeStore.getSubscribeRevision(props.id)
    conflict.value = null
  } catch (error: any) {
    message.error(error.message || error)
  } finally {
    reloading.value = false
  }
}

const modalSlots = {
  cancel: () =>
    h(
      Button,
      {
        disabled: loading.value,
        onClick: handleCancel,
      },
      () => t('common.cancel'),
    ),
  submit: () =>
    h(
      Button,
      {
        type: 'primary',
        loading: loading.value,
        onClick: handleSave,
      },
      () => t('common.save'),
    ),
}

defineExpose({ modalSlots })
</script>

<template>
  <div>
    <ResourceConflictNotice
      v-if="conflict"
      :kind="conflict"
      :loading="reloading"
      @reload="loadLatest"
    />
    <CodeViewer v-model="code" lang="javascript" editable />
  </div>
</template>
