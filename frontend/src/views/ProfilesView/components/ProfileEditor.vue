<script setup lang="ts">
import { ref, inject, h, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'

import { deepClone, generateConfigViaRpcByProfile, message, restoreProfile } from '@/utils'

import Button from '@/components/Button/index.vue'
import ResourceConflictNotice from '@/components/ResourceConflictNotice/index.vue'
import { isResourceConflict, isResourceNotFound, useProfilesStore } from '@/stores'

interface Props {
  profile: IProfile
}

const props = defineProps<Props>()

const loading = ref(false)
const reloading = ref(false)
const conflict = ref<'changed' | 'deleted' | null>(null)
const profileText = ref('')

const { t } = useI18n()
const profilesStore = useProfilesStore()
let sourceProfile = deepClone(props.profile)
let baseRevision = profilesStore.getProfileRevision(props.profile.id)

const handleCancel = inject('cancel') as any
const handleSubmit = inject('submit') as any

const handleSave = async () => {
  loading.value = true
  try {
    const subscriptions = sourceProfile.outbounds.reduce((p, c) => {
      c.outbounds.forEach((outbound) => {
        if (outbound.type !== 'Built-in') {
          const id = outbound.type === 'Subscription' ? outbound.id : outbound.type
          p.add(id)
        }
      })
      return p
    }, new Set<string>())
    const newProfile = restoreProfile(JSON.parse(profileText.value), sourceProfile.name, {
      profile: sourceProfile,
      subscriptionIds: [...subscriptions],
    })
    newProfile.id = sourceProfile.id
    newProfile.mixin = sourceProfile.mixin
    newProfile.script = sourceProfile.script
    await profilesStore.editProfile(sourceProfile.id, newProfile, baseRevision)
    await handleSubmit()
  } catch (error: any) {
    console.log(error)
    if (isResourceConflict(error) || isResourceNotFound(error)) {
      await profilesStore.setupProfiles().catch(() => undefined)
      conflict.value = isResourceNotFound(error) ? 'deleted' : 'changed'
    } else {
      message.error(error.message || error)
    }
  }
  loading.value = false
}

const renderProfile = async (value: IProfile) => {
  const text = await generateConfigViaRpcByProfile(value, {
    enableStableConfigCompat: false,
    enableMixinProcessing: false,
    enableScriptProcessing: false,
  })
  profileText.value = JSON.stringify(text, null, 2)
}

const loadLatest = async () => {
  reloading.value = true
  try {
    await profilesStore.setupProfiles()
    const latest = profilesStore.getProfileById(sourceProfile.id)
    if (!latest) {
      conflict.value = 'deleted'
      return
    }
    sourceProfile = deepClone(latest)
    baseRevision = profilesStore.getProfileRevision(latest.id)
    await renderProfile(sourceProfile)
    conflict.value = null
  } catch (error: any) {
    message.error(error.message || error)
  } finally {
    reloading.value = false
  }
}

onMounted(() => {
  void renderProfile(sourceProfile)
})

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
    <CodeViewer v-model="profileText" lang="json" editable class="flex-1 min-h-0" />
  </div>
</template>
