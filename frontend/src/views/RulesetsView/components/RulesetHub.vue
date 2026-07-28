<script setup lang="ts">
import { computed, h, inject, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

import { RulesetFormat } from '@/enums/kernel'
import { useRulesetsStore, type RuleSetHub } from '@/stores'
import { message } from '@/utils'

import Button from '@/components/Button/index.vue'
import CodeViewer from '@/components/CodeViewer/index.vue'
import { useModal } from '@/components/Modal'
import Pagination from '@/components/Pagination/index.vue'

const pageSize = 27
const currentPage = ref(1)

const { t } = useI18n()
const rulesetsStore = useRulesetsStore()
const [PreviewModal, previewModalApi] = useModal({})

const keywords = ref('')
const handleCancel = inject('cancel') as any

watch(keywords, () => (currentPage.value = 1))

const filteredList = computed(() => {
  if (!keywords.value) return rulesetsStore.rulesetHub.list
  return rulesetsStore.rulesetHub.list.filter((ruleset) => ruleset.name.includes(keywords.value))
})

const currentList = computed(() => {
  return filteredList.value.slice(
    (currentPage.value - 1) * pageSize,
    (currentPage.value - 1) * pageSize + pageSize,
  )
})

const getRulesetUrlAndSuffix = (ruleset: RuleSetHub['list'][number], format: RulesetFormat) => {
  const suffix = { [RulesetFormat.Binary]: '.srs', [RulesetFormat.Source]: '.json' }[format]
  const basrUrl = {
    geosite: rulesetsStore.rulesetHub.geosite,
    geoip: rulesetsStore.rulesetHub.geoip,
  }[ruleset.type]
  return [basrUrl + ruleset.name + suffix, suffix] as const
}

const getRulesetHubIndex = (ruleset: RuleSetHub['list'][number]) => {
  return rulesetsStore.rulesetHub.list.indexOf(ruleset)
}

const handleAddRuleset = async (ruleset: RuleSetHub['list'][number], format: RulesetFormat) => {
  const [url, suffix] = getRulesetUrlAndSuffix(ruleset, format)
  const id = ruleset.type + '_' + ruleset.name + '.' + format
  try {
    await rulesetsStore.addRuleset({
      id,
      tag: `${ruleset.name}-${ruleset.type}${suffix}`,
      updateTime: 0,
      disabled: false,
      type: 'Http',
      format,
      path: '',
      url,
      count: ruleset.count,
    })
    const { success } = message.info('rulesets.updating')
    await rulesetsStore.updateRuleset(id)
    success('common.success')
  } catch (error: any) {
    console.error(error)
    message.error(error.message || error)
  }
}

const handlePreview = async (
  index: number,
  ruleset: RuleSetHub['list'][number],
  format: RulesetFormat,
) => {
  const { destroy, error } = message.info('rulesets.fetching', 15_000)
  try {
    const content = await rulesetsStore.previewRuleSetHub(index, format)
    const formattedContent = JSON.stringify(JSON.parse(content), null, 2)
    destroy()
    previewModalApi.setProps({
      title: ruleset.name,
      height: '90',
      width: '60',
      submit: false,
      cancelText: 'common.close',
      maskClosable: true,
      bodyScrollable: false,
    })
    previewModalApi.setContent(CodeViewer, {
      modelValue: formattedContent,
      editable: false,
      lang: 'json',
      class: 'h-full min-h-0',
    })
    previewModalApi.open()
  } catch (err: any) {
    error(err.message || err)
    setTimeout(destroy, 2000)
  }
}

const handleUpdateRulesetHub = async () => {
  try {
    await rulesetsStore.updateRulesetHub()
    message.success('rulesets.updateSuccess')
  } catch (err: any) {
    message.error(err.message || err)
  }
}

const isAlreadyAdded = (id: string) => rulesetsStore.getRulesetById(id)

if (rulesetsStore.rulesetHub.list.length === 0) {
  rulesetsStore.updateRulesetHub()
}

const modalSlots = {
  action: () =>
    !rulesetsStore.rulesetHubLoading
      ? h(Pagination, {
          current: currentPage.value,
          'onUpdate:current': (current: number) => (currentPage.value = current),
          total: filteredList.value.length,
          pageSize: pageSize,
          size: 'small',
          class: 'mr-auto',
        })
      : null,
  close: () =>
    h(
      Button,
      {
        type: 'text',
        onClick: handleCancel,
      },
      () => t('common.close'),
    ),
}

defineExpose({ modalSlots })
</script>

<template>
  <div class="h-full">
    <div v-if="rulesetsStore.rulesetHubLoading" class="flex items-center justify-center h-full">
      <Button type="text" loading />
    </div>
    <div v-else class="flex flex-col h-full">
      <div class="flex items-center gap-8">
        <Input
          v-model="keywords"
          :border="false"
          :placeholder="t('rulesets.total') + ': ' + rulesetsStore.rulesetHub.list.length"
          clearable
          size="small"
          class="flex-1"
        />
        <Button icon="refresh" size="small" @click="handleUpdateRulesetHub">
          {{ t('common.update') }}
        </Button>
      </div>

      <Empty v-if="filteredList.length === 0" />

      <div class="overflow-y-auto grid grid-cols-3 text-12 gap-8 mt-8 pb-16 pr-8">
        <Card
          v-for="ruleset in currentList"
          :key="ruleset.name + ruleset.type"
          :title="ruleset.name"
        >
          <template #extra>
            <Tag size="small" color="cyan">{{ ruleset.type }}</Tag>
          </template>
          <div class="flex flex-col h-full">
            <div class="flex items-center justify-between">
              {{ t('rulesets.rulesetCount') }} : {{ ruleset.count }}
              <Button
                icon="preview"
                size="small"
                type="text"
                @click="handlePreview(getRulesetHubIndex(ruleset), ruleset, RulesetFormat.Source)"
              />
            </div>
            <!-- <div v-tips="ruleset.description" class="flex-1 line-clamp-2">
              {{ ruleset.description || t('rulesets.noDesc') }}
            </div> -->
            <div class="flex items-center justify-end">
              <template
                v-if="
                  isAlreadyAdded(ruleset.type + '_' + ruleset.name + '.' + RulesetFormat.Source)
                "
              >
                <Button type="text" size="small">
                  {{ t('ruleset.format.source') }} {{ t('common.added') }}
                </Button>
              </template>
              <template v-else>
                <Button
                  type="link"
                  size="small"
                  @click="handleAddRuleset(ruleset, RulesetFormat.Source)"
                >
                  {{ t('common.add') }} {{ t('ruleset.format.source') }}
                </Button>
              </template>
              <template
                v-if="
                  isAlreadyAdded(ruleset.type + '_' + ruleset.name + '.' + RulesetFormat.Binary)
                "
              >
                <Button type="text" size="small">
                  {{ t('ruleset.format.binary') }} {{ t('common.added') }}
                </Button>
              </template>
              <template v-else>
                <Button
                  type="link"
                  size="small"
                  @click="handleAddRuleset(ruleset, RulesetFormat.Binary)"
                >
                  {{ t('common.add') }} {{ t('ruleset.format.binary') }}
                </Button>
              </template>
            </div>
          </div>
        </Card>
      </div>
    </div>
  </div>
  <PreviewModal />
</template>
