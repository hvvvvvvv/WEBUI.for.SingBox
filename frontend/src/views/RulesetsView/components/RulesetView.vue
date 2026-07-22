<script setup lang="ts">
import { ref, inject, h } from 'vue'
import { useI18n } from 'vue-i18n'

import { isResourceConflict, isResourceNotFound, type RuleSet, useRulesetsStore } from '@/stores'
import { deepClone, isValidJson, message } from '@/utils'

import Button from '@/components/Button/index.vue'
import ResourceConflictNotice from '@/components/ResourceConflictNotice/index.vue'

import type { ExpectedRevision } from '../../../../gen/common/v1/sync_pb'

interface Props {
  id: string
}

const props = defineProps<Props>()

const loading = ref(false)
const reloading = ref(false)
const conflict = ref<'changed' | 'deleted' | null>(null)
const ruleset = ref<RuleSet>()
const rulesetContent = ref<string>('')
let baseRevision: Pick<ExpectedRevision, 'instanceId' | 'revision'> | undefined

const handleCancel = inject('cancel') as any
const handleSubmit = inject('submit') as any

const { t } = useI18n()
const rulesetsStore = useRulesetsStore()

const handleSave = async () => {
  if (!ruleset.value) return
  loading.value = true
  try {
    if (!isValidJson(rulesetContent.value)) {
      throw 'syntax error'
    }
    await rulesetsStore.saveRulesetContent(ruleset.value.id, rulesetContent.value, baseRevision)
    await handleSubmit()
  } catch (error: any) {
    console.log(error)
    if (isResourceConflict(error) || isResourceNotFound(error)) {
      await rulesetsStore.setupRulesets().catch(() => undefined)
      conflict.value = isResourceNotFound(error) ? 'deleted' : 'changed'
    } else {
      message.error(error)
    }
  } finally {
    loading.value = false
  }
}

const initContent = async () => {
  const r = rulesetsStore.getRulesetById(props.id)
  if (r) {
    ruleset.value = deepClone(r)
    const { content, revision } = await rulesetsStore.getRulesetContentWithRevision(r.id)
    rulesetContent.value = content
    baseRevision = revision
  }
}

const loadLatest = async () => {
  reloading.value = true
  try {
    await rulesetsStore.setupRulesets()
    if (!rulesetsStore.getRulesetById(props.id)) {
      conflict.value = 'deleted'
      return
    }
    await initContent()
    conflict.value = null
  } catch (error: any) {
    message.error(error.message || error)
  } finally {
    reloading.value = false
  }
}

initContent()

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
    <CodeViewer v-model="rulesetContent" lang="json" editable class="flex-1 min-h-0" />
  </div>
</template>
