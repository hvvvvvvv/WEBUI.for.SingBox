<script setup lang="ts">
import { Code, ConnectError } from '@connectrpc/connect'
import { ref, inject, h, useTemplateRef, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'

import { isResourceConflict, isResourceNotFound, useSubscribesStore } from '@/stores'
import { deepClone, message, omitArray, sampleID } from '@/utils'

import Button from '@/components/Button/index.vue'
import ResourceConflictNotice from '@/components/ResourceConflictNotice/index.vue'

import type { Subscription } from '@/types/app'
import type { ExpectedRevision } from '../../../../gen/common/v1/sync_pb'

interface Props {
  sub: Subscription
  revision: Pick<ExpectedRevision, 'instanceId' | 'revision'>
}

const props = defineProps<Props>()

const loading = ref(false)
const reloading = ref(false)
const conflict = ref<'changed' | 'deleted' | null>(null)
const proxiesText = ref('')
const contentReady = ref(false)
const proxiesEditor = useTemplateRef<{ getContent(): string }>('proxiesEditor')
const sub = ref(deepClone(props.sub))
sub.value.proxies ??= []
let baseRevision = props.revision

const { t } = useI18n()
const subscribeStore = useSubscribesStore()

const handleCancel = inject('cancel') as any
const handleSubmit = inject('submit') as any

const handleSave = async () => {
  if (!contentReady.value || loading.value || reloading.value) return
  loading.value = true
  try {
    await nextTick()
    // CodeMirror emits debounced updates; read its current document before saving.
    proxiesText.value = proxiesEditor.value?.getContent() ?? proxiesText.value
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
  contentReady.value = false
  const { content: latestContent, revision } =
    await subscribeStore.getSubscriptionContentWithRevision(sub.value.id)
  const content = latestContent || '[]'
  if (
    !revision ||
    revision.instanceId !== baseRevision.instanceId ||
    revision.revision !== baseRevision.revision
  ) {
    conflict.value = 'changed'
    throw new ConnectError(t('common.operationConflict'), Code.Aborted)
  }
  const proxies: Subscription['proxies'] = JSON.parse(content)
  const proxyRefs = new Map(sub.value.proxies.map((proxy) => [proxy.tag, proxy.id]))
  const proxiesWithId = proxies.map((proxy) => {
    return {
      __id_in_gui: proxyRefs.get(proxy.tag) || sampleID(),
      ...proxy,
    }
  })
  proxiesText.value = JSON.stringify(proxiesWithId, null, 2)
  contentReady.value = true
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
    baseRevision = subscribeStore.getSubscribeRevision(props.sub.id)
    await initProxiesText()
    conflict.value = null
  } catch (error: any) {
    message.error(error.message || error)
  } finally {
    reloading.value = false
  }
}

void initProxiesText().catch((error: any) => message.error(error.message || error))

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
        disabled: !contentReady.value || reloading.value,
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
    <CodeViewer
      v-if="contentReady"
      ref="proxiesEditor"
      v-model="proxiesText"
      lang="json"
      editable
      class="flex-1 min-h-0"
    />
  </div>
</template>
