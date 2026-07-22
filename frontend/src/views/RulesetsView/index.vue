<script setup lang="ts">
import { computed } from 'vue'
import { useI18n, I18nT } from 'vue-i18n'

import { DraggableOptions, ViewOptions } from '@/constant/app'
import { View } from '@/enums/app'
import { RulesetFormat } from '@/enums/kernel'
import {
  isResourceConflict,
  isResourceNotFound,
  type RuleSet,
  useRulesetsStore,
  useAppSettingsStore,
} from '@/stores'
import {
  debounce,
  deepClone,
  formatRelativeTime,
  formatDate,
  message,
  confirmDelete,
} from '@/utils'

import { useModal } from '@/components/Modal'

import type { Menu } from '@/types/app'

import RulesetForm from './components/RulesetForm.vue'
import RulesetHub from './components/RulesetHub.vue'
import RulesetView from './components/RulesetView.vue'

const sourceMenuList: Menu[] = [
  {
    label: 'rulesets.editRuleset',
    handler: (id: string) => handleEditRulesetList(id),
  },
  {
    label: 'common.clear',
    handler: (id: string) => handleClearRuleset(id),
  },
]

const binaryMenuList: Menu[] = [
  {
    label: 'common.none',
    handler: (id: string) => {
      console.log(id)
      message.info('common.none')
    },
  },
]

const { t } = useI18n()
const [Modal, modalApi] = useModal({})
const rulesetsStore = useRulesetsStore()
const appSettingsStore = useAppSettingsStore()

const handleMutationError = async (error: unknown) => {
  if (isResourceConflict(error) || isResourceNotFound(error)) {
    await rulesetsStore.setupRulesets().catch(() => undefined)
    message.error(t('common.operationConflict'))
    return
  }
  message.error((error as any)?.message || error)
}

const handleImportRuleset = async () => {
  modalApi.setProps({
    title: 'rulesets.hub',
    cancelText: 'common.close',
    height: '90',
    width: '90',
    submit: false,
    maskClosable: true,
  })
  modalApi.setContent(RulesetHub)
  modalApi.open()
}

const handleShowRulesetForm = async (id?: string, isUpdate = false) => {
  modalApi.setProps({
    title: isUpdate ? 'common.edit' : 'common.add',
    maxHeight: '90',
    minWidth: '70',
  })
  modalApi.setContent(RulesetForm, { id, isUpdate })
  modalApi.open()
}

const handleUpdateRulesets = async () => {
  try {
    await rulesetsStore.updateRulesets()
    message.success('common.success')
  } catch (error: any) {
    console.error('updateRulesets: ', error)
    message.error(error)
  }
}

const handleEditRulesetList = (id: string) => {
  modalApi.setProps({
    title: rulesetsStore.getRulesetById(id)?.tag,
    height: '90',
    width: '90',
  })
  modalApi.setContent(RulesetView, { id })
  modalApi.open()
}

const handleUpdateRuleset = async (r: RuleSet) => {
  try {
    await rulesetsStore.updateRuleset(r.id)
  } catch (error: any) {
    console.error('updateRuleset: ', error)
    message.error(error)
  }
}

const handleDeleteRuleset = async (r: RuleSet) => {
  if (!(await confirmDelete())) return

  try {
    await rulesetsStore.deleteRuleset(r.id)
  } catch (error: any) {
    console.error('deleteRuleset: ', error)
    await handleMutationError(error)
  }
}

const handleDisableRuleset = async (r: RuleSet) => {
  const revision = rulesetsStore.getRulesetRevision(r.id)
  const next = deepClone(r)
  next.disabled = !next.disabled
  try {
    await rulesetsStore.editRuleset(r.id, next, revision)
  } catch (error: any) {
    await handleMutationError(error)
  }
}

const handleClearRuleset = async (id: string) => {
  const r = rulesetsStore.getRulesetById(id)
  if (!r) return
  if (r.format != RulesetFormat.Source) return

  try {
    await rulesetsStore.clearRulesetContent(r.id)
  } catch (error: any) {
    console.error(error)
    await handleMutationError(error)
  }
}

const generateMenus = (r: RuleSet) => {
  return {
    [RulesetFormat.Source]: sourceMenuList,
    [RulesetFormat.Binary]: binaryMenuList,
  }[r.format].map((v) => ({ ...v, handler: () => v.handler?.(r.id) }))
}

const noUpdateNeeded = computed(() => rulesetsStore.rulesets.every((v) => v.disabled))

let sortRevision = rulesetsStore.getRulesetsOrderRevision()
let sortStartIDs: string[] = []
const onSortStart = () => {
  sortRevision = rulesetsStore.getRulesetsOrderRevision()
  sortStartIDs = rulesetsStore.rulesets.map((item) => item.id)
}
const saveRulesetOrder = debounce(
  (
    ids: string[],
    revision: ReturnType<typeof rulesetsStore.getRulesetsOrderRevision>,
    fallbackIDs: string[],
  ) => rulesetsStore.reorderRulesets(ids, revision, fallbackIDs),
  1000,
)
const onSortUpdate = () => {
  void saveRulesetOrder(
    rulesetsStore.rulesets.map((item) => item.id),
    sortRevision,
    sortStartIDs,
  ).catch((error: any) => void handleMutationError(error))
}
</script>

<template>
  <div v-if="rulesetsStore.rulesets.length === 0" class="grid-list-empty">
    <Empty>
      <template #description>
        <I18nT keypath="rulesets.empty" tag="div" scope="global" class="flex items-center mt-12">
          <template #action>
            <Button type="link" @click="handleShowRulesetForm()">{{ t('common.add') }}</Button>
          </template>
          <template #import>
            <Button type="link" @click="handleImportRuleset">{{ t('rulesets.hub') }}</Button>
          </template>
        </I18nT>
      </template>
    </Empty>
  </div>

  <div v-else class="grid-list-header">
    <Radio v-model="appSettingsStore.app.rulesetsView" :options="ViewOptions" class="mr-auto" />
    <Button type="link" @click="handleImportRuleset">
      {{ t('rulesets.hub') }}
    </Button>
    <Button
      :disabled="noUpdateNeeded"
      :type="noUpdateNeeded ? 'text' : 'link'"
      @click="handleUpdateRulesets"
    >
      {{ t('common.updateAll') }}
    </Button>
    <Button type="primary" icon="add" class="ml-16" @click="handleShowRulesetForm()">
      {{ t('common.add') }}
    </Button>
  </div>

  <div
    v-draggable="[
      rulesetsStore.rulesets,
      { ...DraggableOptions, onStart: onSortStart, onUpdate: onSortUpdate },
    ]"
    :class="'grid-list-' + appSettingsStore.app.rulesetsView"
  >
    <Card
      v-for="r in rulesetsStore.rulesets"
      :key="r.id"
      v-menu="generateMenus(r)"
      :title="r.tag"
      :disabled="r.disabled"
      class="grid-list-item"
    >
      <template #title-prefix>
        <Tag v-if="r.updating" color="cyan" size="small">
          {{ t('ruleset.updating') }}
        </Tag>
      </template>

      <template v-if="appSettingsStore.app.rulesetsView === View.Grid" #extra>
        <Dropdown>
          <Button type="link" size="small" icon="more" />
          <template #overlay>
            <div class="flex flex-col gap-4 min-w-64 p-4">
              <Button
                :disabled="r.disabled"
                :loading="r.updating"
                :type="r.disabled ? 'text' : 'text'"
                @click="handleUpdateRuleset(r)"
              >
                {{ t('common.update') }}
              </Button>
              <Button type="text" @click="handleDisableRuleset(r)">
                {{ r.disabled ? t('common.enable') : t('common.disable') }}
              </Button>
              <Button type="text" @click="handleShowRulesetForm(r.id, true)">
                {{ t('common.edit') }}
              </Button>
              <Button type="text" @click="handleDeleteRuleset(r)">
                {{ t('common.delete') }}
              </Button>
            </div>
          </template>
        </Dropdown>
      </template>

      <template v-else #extra>
        <Button
          :disabled="r.disabled"
          :loading="r.updating"
          :type="r.disabled ? 'text' : 'text'"
          size="small"
          @click="handleUpdateRuleset(r)"
        >
          {{ t('common.update') }}
        </Button>
        <Button type="text" size="small" @click="handleDisableRuleset(r)">
          {{ r.disabled ? t('common.enable') : t('common.disable') }}
        </Button>
        <Button type="text" size="small" @click="handleShowRulesetForm(r.id, true)">
          {{ t('common.edit') }}
        </Button>
        <Button type="text" size="small" @click="handleDeleteRuleset(r)">
          {{ t('common.delete') }}
        </Button>
      </template>

      <div v-if="r.format === RulesetFormat.Binary">
        {{ t('ruleset.format.name') }}
        :
        {{ r.format || '--' }}
      </div>

      <template v-if="appSettingsStore.app.rulesetsView === View.Grid">
        <div v-if="r.format === RulesetFormat.Source">
          {{ t('rulesets.rulesetCount') }}
          :
          {{ r.count }}
        </div>
        <div>
          {{ t('common.updateTime') }}
          :
          {{ r.updateTime ? formatRelativeTime(r.updateTime) : '--' }}
        </div>
      </template>
      <template v-else>
        <div v-if="r.format === RulesetFormat.Source">
          {{ t('rulesets.rulesetCount') }}
          :
          {{ r.count }}
        </div>
        <div>
          {{ t('common.updateTime') }}
          :
          {{ r.updateTime ? formatDate(r.updateTime, 'YYYY-MM-DD HH:mm:ss') : '--' }}
        </div>
      </template>
    </Card>
  </div>

  <Modal />
</template>
