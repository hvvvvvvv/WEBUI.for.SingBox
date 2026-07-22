<script setup lang="ts">
import { ref, inject, watch, computed, h } from 'vue'
import { useI18n } from 'vue-i18n'

import { RulesetFormatOptions } from '@/constant/kernel'
import { RulesetFormat } from '@/enums/kernel'
import { isResourceConflict, isResourceNotFound, type RuleSet, useRulesetsStore } from '@/stores'
import { deepClone, message, sampleID } from '@/utils'

import Button from '@/components/Button/index.vue'
import ResourceConflictNotice from '@/components/ResourceConflictNotice/index.vue'

interface Props {
  id?: string
  isUpdate?: boolean
}

const props = withDefaults(defineProps<Props>(), {
  id: '',
  isUpdate: false,
})

const loading = ref(false)
const reloading = ref(false)
const conflict = ref<'changed' | 'deleted' | null>(null)

const ruleset = ref<RuleSet>({
  id: sampleID(),
  tag: '',
  updateTime: 0,
  format: RulesetFormat.Binary,
  type: 'Http',
  url: '',
  count: 0,
  path: '',
  disabled: false,
})

const { t } = useI18n()
const rulesetsStore = useRulesetsStore()
let baseRevision = props.isUpdate ? rulesetsStore.getRulesetRevision(props.id) : undefined

const handleCancel = inject('cancel') as any

const handleSubmit = async () => {
  loading.value = true

  if (props.isUpdate) {
    try {
      await rulesetsStore.editRuleset(props.id, ruleset.value, baseRevision)
      handleCancel()
    } catch (error: any) {
      console.error('editRuleset: ', error)
      if (isResourceConflict(error) || isResourceNotFound(error)) {
        await rulesetsStore.setupRulesets().catch(() => undefined)
        conflict.value = isResourceNotFound(error) ? 'deleted' : 'changed'
      } else {
        message.error(error)
      }
    }

    loading.value = false

    return
  }

  try {
    await rulesetsStore.addRuleset(ruleset.value)
    handleCancel()
  } catch (error: any) {
    console.error('addRuleset: ', error)
    message.error(error)
  }

  loading.value = false
}

const loadLatest = async () => {
  if (!props.isUpdate) return
  reloading.value = true
  try {
    await rulesetsStore.setupRulesets()
    const latest = rulesetsStore.getRulesetById(props.id)
    if (!latest) {
      conflict.value = 'deleted'
      return
    }
    ruleset.value = deepClone(latest)
    baseRevision = rulesetsStore.getRulesetRevision(props.id)
    conflict.value = null
  } catch (error: any) {
    message.error(error.message || error)
  } finally {
    reloading.value = false
  }
}

const disabled = computed(
  () => !ruleset.value.tag || (ruleset.value.type === 'Http' && !ruleset.value.url),
)

watch(
  () => ruleset.value.type,
  (v) => {
    if (v === 'Manual') {
      ruleset.value.format = RulesetFormat.Source
    }
  },
)

watch(
  () => ruleset.value.format,
  (v, old) => {
    const isJson = v === RulesetFormat.Source
    if (!isJson && ruleset.value.type === 'Manual') {
      ruleset.value.format = old
      message.error('Not support')
      return
    }
  },
)

if (props.isUpdate) {
  const r = rulesetsStore.getRulesetById(props.id)
  if (r) {
    ruleset.value = deepClone(r)
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
        disabled: disabled.value,
        loading: loading.value,
        onClick: handleSubmit,
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
    <div class="form-item">
      {{ t('ruleset.rulesetType') }}
      <Radio
        v-model="ruleset.type"
        :options="[
          { label: 'common.http', value: 'Http' },
          { label: 'ruleset.manual', value: 'Manual' },
        ]"
      />
    </div>
    <div v-show="ruleset.type !== 'Manual'" class="form-item">
      {{ t('ruleset.format.name') }}
      <Radio v-model="ruleset.format" :options="RulesetFormatOptions" />
    </div>
    <div class="form-item">
      {{ t('ruleset.name') }} *
      <div class="min-w-[75%]">
        <Input v-model="ruleset.tag" autofocus class="w-full" />
      </div>
    </div>
    <div v-show="ruleset.type !== 'Manual'" class="form-item">
      {{ t('ruleset.url') }} *
      <div class="min-w-[75%]">
        <Input v-model="ruleset.url" allow-paste placeholder="http(s)://" class="w-full" />
      </div>
    </div>
  </div>
</template>
