<script setup lang="ts">
import { useI18n, I18nT } from 'vue-i18n'

import { ClipboardSetText } from '@/bridge'
import { DraggableOptions, ViewOptions } from '@/constant/app'
import { View } from '@/enums/app'
import {
  useProfilesStore,
  useAppSettingsStore,
  useAppConfigStore,
  useKernelApiStore,
  useSubscribesStore,
  useAppStore,
  isResourceConflict,
  isResourceNotFound,
} from '@/stores'
import {
  debounce,
  deepClone,
  generateConfigViaRpc,
  message,
  sampleID,
  alert,
  confirmDelete,
} from '@/utils'

import CodeViewer from '@/components/CodeViewer/index.vue'
import { useModal } from '@/components/Modal'

import type { Menu } from '@/types/app'
import type { SortableEvent } from 'vue-draggable-plus'

import ProfileForm from './components/ProfileForm.vue'
import QRSShareDialog from './components/QRSShareDialog.vue'

const { t } = useI18n()
const [Modal, modalApi] = useModal({})
const appStore = useAppStore()
const profilesStore = useProfilesStore()
const subscribesStore = useSubscribesStore()
const appSettingsStore = useAppSettingsStore()
const appConfigStore = useAppConfigStore()
const kernelApiStore = useKernelApiStore()

const handleMutationError = async (error: unknown) => {
  if (isResourceConflict(error) || isResourceNotFound(error)) {
    await profilesStore.setupProfiles().catch(() => undefined)
    message.error(t('common.operationConflict'))
    return
  }
  message.error((error as any)?.message || error)
}

const menuList: Menu[] = [
  'profile.step.name',
  'profile.step.general',
  'profile.step.inbounds',
  'profile.step.outbounds',
  'profile.step.route',
  'profile.step.dns',
  'profile.step.mixin-script',
].map((v, i) => {
  return {
    label: v,
    handler: (id: string) => {
      const p = profilesStore.getProfileById(id)
      p && handleShowProfileForm(p.id, i)
    },
  }
})

const secondaryMenusList: Menu[] = [
  {
    label: 'profiles.start',
    handler: async (id: string) => {
      appConfigStore.config.profile = id
      try {
        await appConfigStore.saveNow()
        if (!kernelApiStore.running) await kernelApiStore.startCore()
      } catch (error: any) {
        message.error(error)
        console.error(error)
      }
    },
  },
  {
    label: 'profiles.copy',
    handler: async (id: string) => {
      const p = deepClone(profilesStore.getProfileById(id)!)
      p.id = sampleID()
      p.name = p.name + '(Copy)'
      await profilesStore.addProfile(p)
      message.success('common.success')
    },
  },
  {
    label: 'profiles.copytoClipboard',
    handler: async (id: string) => {
      try {
        const config = await generateConfigViaRpc(id)
        const str = JSON.stringify(config, null, 2)
        const ok = await ClipboardSetText(str)
        if (!ok) throw 'ClipboardSetText Error'
        message.success('common.success')
      } catch (error: any) {
        message.error(error.message || error)
      }
    },
  },
  {
    label: 'profiles.generateAndView',
    handler: async (id: string) => {
      const p = profilesStore.getProfileById(id)!
      try {
        const config = await generateConfigViaRpc(id)
        modalApi.setProps({
          title: p.name,
          height: '90',
          width: '60',
          submit: false,
          cancelText: 'common.close',
          maskClosable: true,
          bodyScrollable: false,
        })
        modalApi.setContent(CodeViewer, {
          modelValue: JSON.stringify(config, null, 2),
          editable: false,
          lang: 'json',
          class: 'h-full min-h-0',
        })
        modalApi.open()
      } catch (error: any) {
        message.error(error.message || error)
      }
    },
  },
  {
    label: 'profiles.qrs.share',
    handler: (id: string) => {
      const profile = profilesStore.getProfileById(id)
      if (!profile) return

      modalApi.setProps({
        title: 'profiles.qrs.title',
        footer: false,
        maxHeight: '95',
        maxWidth: '95',
        minWidth: '0',
        maskClosable: true,
        bodyScrollable: true,
        toolbar: { maximize: false },
      })
      modalApi
        .setContent(QRSShareDialog, {
          profileId: profile.id,
          profileName: profile.name,
          onClose: () => modalApi.close(),
        })
        .open()
    },
  },
]

const generateMenus = (profile: IProfile) => {
  const moreMenus: Menu[] = secondaryMenusList.map((v) => ({
    ...v,
    handler: () => v.handler?.(profile.id),
  }))
  const builtInMenus: Menu[] = [
    ...menuList.map((v) => ({ ...v, handler: () => v.handler?.(profile.id) })),
    {
      label: '',
      separator: true,
    },
    {
      label: 'common.more',
      children: moreMenus,
    },
  ]

  return builtInMenus
}

const handleShowProfileForm = (id?: string, step = 0) => {
  modalApi.setProps({ minWidth: '70' })
  modalApi.setContent(ProfileForm, { id, step }).open()
}

const handleDeleteProfile = async (p: IProfile) => {
  const { profile } = appConfigStore.config
  if (profile === p.id && kernelApiStore.running) {
    message.warn('profiles.shouldStop')
    return
  }
  if (!(await confirmDelete())) return

  try {
    await profilesStore.deleteProfile(p.id)
  } catch (error: any) {
    console.error('deleteProfile: ', error)
    await handleMutationError(error)
  }
}

const handleUseProfile = async (p: IProfile) => {
  if (appConfigStore.config.profile === p.id) return

  appConfigStore.config.profile = p.id
  try {
    await appConfigStore.saveNow()
  } catch (error: any) {
    console.error('saveAppConfig: ', error)
    message.error(error.message || error)
  }
}

const isCreatedBySubscription = (id: string) => {
  return !!subscribesStore.getSubscribeById(id)
}

const showAuto = () => alert('Tips', 'profile.auto')

const saveProfileOrder = debounce(
  (
    ids: string[],
    revision: ReturnType<typeof profilesStore.getProfilesOrderRevision>,
    fallbackIDs: string[],
  ) => profilesStore.saveProfilesOrder(ids, revision, fallbackIDs),
  1000,
)

let sortRevision = profilesStore.getProfilesOrderRevision()
let sortStartIDs: string[] = []
const onSortStart = () => {
  sortRevision = profilesStore.getProfilesOrderRevision()
  sortStartIDs = profilesStore.profiles.map((profile) => profile.id)
}

const onSortUpdate = (event: SortableEvent) => {
  const oldIndex = event.oldDraggableIndex ?? event.oldIndex
  const newIndex = event.newDraggableIndex ?? event.newIndex
  if (oldIndex == null || newIndex == null || oldIndex === newIndex) return

  const next = [...profilesStore.profiles]
  const [item] = next.splice(oldIndex, 1)
  if (!item) return
  next.splice(newIndex, 0, item)
  profilesStore.profiles.splice(0, profilesStore.profiles.length, ...next)
  saveProfileOrder(
    next.map((profile) => profile.id),
    sortRevision,
    sortStartIDs,
  ).catch((error: any) => {
    void handleMutationError(error)
  })
}
</script>

<template>
  <div v-if="profilesStore.profiles.length === 0" class="grid-list-empty">
    <Empty>
      <template #description>
        <I18nT keypath="profiles.empty" tag="div" scope="global" class="flex items-center mt-12">
          <template #action>
            <Button type="link" @click="handleShowProfileForm()">{{ t('common.add') }}</Button>
          </template>
        </I18nT>
        <div class="flex items-center">
          <CustomAction :actions="appStore.customActions.profiles_header" />
        </div>
      </template>
    </Empty>
  </div>

  <div v-else class="grid-list-header">
    <Radio v-model="appSettingsStore.app.profilesView" :options="ViewOptions" class="mr-auto" />
    <CustomAction :actions="appStore.customActions.profiles_header" />
    <Button type="primary" icon="add" @click="handleShowProfileForm()">
      {{ t('common.add') }}
    </Button>
  </div>

  <div
    v-draggable="[
      profilesStore.profiles,
      { ...DraggableOptions, onStart: onSortStart, customUpdate: onSortUpdate },
    ]"
    :class="'grid-list-' + appSettingsStore.app.profilesView"
  >
    <Card
      v-for="p in profilesStore.profiles"
      :key="p.id"
      v-menu="generateMenus(p)"
      :title="p.name"
      :selected="appConfigStore.config.profile === p.id"
      class="grid-list-item"
      @dblclick="handleUseProfile(p)"
    >
      <template #title-prefix>
        <Tag
          v-if="isCreatedBySubscription(p.id)"
          color="primary"
          size="small"
          style="margin-left: 0"
          @click="showAuto"
        >
          {{ t('common.auto') }}
        </Tag>
      </template>

      <template v-if="appSettingsStore.app.profilesView === View.Grid" #extra>
        <Dropdown>
          <Button type="link" size="small" icon="more" />
          <template #overlay>
            <div class="flex flex-col gap-4 min-w-64 p-4">
              <Button type="text" @click="handleUseProfile(p)">
                {{ t('common.use') }}
              </Button>
              <Button type="text" @click="handleShowProfileForm(p.id)">
                {{ t('common.edit') }}
              </Button>
              <Button type="text" @click="handleDeleteProfile(p)">
                {{ t('common.delete') }}
              </Button>
            </div>
          </template>
        </Dropdown>
      </template>

      <template v-else #extra>
        <Button type="text" size="small" @click="handleUseProfile(p)">
          {{ t('common.use') }}
        </Button>
        <Button type="text" size="small" @click="handleShowProfileForm(p.id)">
          {{ t('common.edit') }}
        </Button>
        <Button type="text" size="small" @click="handleDeleteProfile(p)">
          {{ t('common.delete') }}
        </Button>
      </template>
      <div>
        {{ t('profiles.inbounds') }}
        :
        {{ p.inbounds.length }}
        /
        {{ t('profiles.outbounds') }}
        :
        {{ p.outbounds.length }}
      </div>
      <div>
        {{ t('kernel.route.tab.rule_set') }}
        :
        {{ p.route.rule_set.length }}
        /
        {{ t('kernel.route.tab.rules') }}
        :
        {{ p.route.rules.length }}
      </div>
      <div>
        {{ t('profiles.dnsServers') }}
        :
        {{ p.dns.servers.length }}
        /
        {{ t('profiles.dnsRules') }}
        :
        {{ p.dns.rules.length }}
      </div>
    </Card>
  </div>

  <Modal />
</template>
