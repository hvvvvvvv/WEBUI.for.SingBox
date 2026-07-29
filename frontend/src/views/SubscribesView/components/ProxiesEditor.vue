<script setup lang="ts">
import { ref, inject, h } from 'vue'
import { useI18n } from 'vue-i18n'

import { isResourceConflict, isResourceNotFound, useSubscribesStore } from '@/stores'
import { deepClone, message, omitArray, sampleID } from '@/utils'

import Button from '@/components/Button/index.vue'
import ResourceConflictNotice from '@/components/ResourceConflictNotice/index.vue'

import type { Subscription } from '@/types/app'
import type { ExpectedRevision } from '../../../../gen/common/v1/sync_pb'

interface Props {
  sub: Subscription
}

const props = defineProps<Props>()

const loading = ref(false)
const reloading = ref(false)
const conflict = ref<'changed' | 'deleted' | null>(null)
const proxiesText = ref('')
const sub = ref(deepClone(props.sub))
sub.value.proxies ??= []
let baseRevision: Pick<ExpectedRevision, 'instanceId' | 'revision'> | undefined

const { t } = useI18n()
const subscribeStore = useSubscribesStore()

const handleCancel = inject('cancel') as any
const handleSubmit = inject('submit') as any

const handleSave = async () => {
  loading.value = true
  try {
    const { id } = sub.value
    const proxiesWithId: Record<string, any>[] = JSON.parse(proxiesText.value)
    const proxyIds = proxiesWithId.map((proxy) =>
      typeof proxy.__id_in_gui === 'string' ? proxy.__id_in_gui : '',
    )
    await subscribeStore.saveSubscriptionContent(
      id,
      JSON.stringify(omitArray(proxiesWithId, ['__id_in_gui']), null, 2),
      proxyIds,
      baseRevision,
    )
    await handleSubmit()
  } catch (error: any) {
    if (isResourceConflict(error) || isResourceNotFound(error)) {
      await subscribeStore.setupSubscribes().catch(() => undefined)
      conflict.value = isResourceNotFound(error) ? 'deleted' : 'changed'
    } else {
      message.error(error.message || error)
    }
  }
  loading.value = false
}

const initProxiesText = async () => {
  const { content: latestContent, revision } =
    await subscribeStore.getSubscriptionContentWithRevision(sub.value.id)
  const content = latestContent || '[]'
  baseRevision = revision
  const proxies: Subscription['proxies'] = JSON.parse(content)
  const proxyRefs = sub.value.proxies ?? []
  const proxiesWithId = proxies.map((proxy) => {
    return {
      __id_in_gui: proxyRefs.find((v) => v.tag === proxy.tag)?.id || sampleID(),
      ...proxy,
    }
  })
  proxiesText.value = JSON.stringify(proxiesWithId, null, 2)
}

const loadLatest = async () => {
  reloading.value = true
  try {
    await subscribeStore.setupSubscribes()
    const latest = subscribeStore.getSubscribeById(props.sub.id)
    if (!latest) {
      conflict.value = 'deleted'
      return
    }
    sub.value = deepClone(latest)
    sub.value.proxies ??= []
    await initProxiesText()
    conflict.value = null
  } catch (error: any) {
    message.error(error.message || error)
  } finally {
    reloading.value = false
  }
}

initProxiesText()

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
  <div class="h-full flex flex-col">
    <ResourceConflictNotice
      v-if="conflict"
      :kind="conflict"
      :loading="reloading"
      @reload="loadLatest"
    />
    <CodeViewer v-model="proxiesText" lang="json" editable class="flex-1 min-h-0" />
  </div>
</template>
