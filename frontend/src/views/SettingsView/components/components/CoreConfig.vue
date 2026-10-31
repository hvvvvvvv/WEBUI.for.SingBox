<script lang="ts" setup>
import { h, inject, onMounted, onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'

import { DefaultCoreConfig } from '@/constant/kernel'
import { createRpcClient } from '@/bridge'
import { Branch } from '@/enums/app'
import { useAppConfigStore, useKernelApiStore } from '@/stores'
import { deepClone, message } from '@/utils'
import { previewCoreRuntimeValue, type CoreRuntimePaths } from '@/utils/coreRuntimePreview'
import { KernelRuntimeService } from '../../../../../gen/kernel/v1/kernel_runtime_service_pb'

import Button from '@/components/Button/index.vue'
import Card from '@/components/Card/index.vue'

interface Props {
  isAlpha: boolean
}

const props = defineProps<Props>()

const tabs = [
  { tab: 'settings.kernel.config.env', key: 'env' },
  { tab: 'settings.kernel.config.args', key: 'args' },
]

const activeKey = ref('env')
const handleCancel = inject('cancel') as any
const handleSubmit = inject('submit') as any

const { t } = useI18n()
const appConfig = useAppConfigStore()
const kernelApiStore = useKernelApiStore()

const model = ref(deepClone(props.isAlpha ? appConfig.config.alpha : appConfig.config.main))
const runtimePaths = ref<CoreRuntimePaths>()
const pathsLoading = ref(true)
const pathsRequest = new AbortController()
onMounted(async () => {
  try {
    const paths = await createRpcClient(KernelRuntimeService).getRuntimePaths(
      {},
      {
        signal: pathsRequest.signal,
        timeoutMs: 5_000,
      },
    )
    if (!pathsRequest.signal.aborted && paths.coreBasePath) runtimePaths.value = paths
  } catch {
    // Editing remains available when the server cannot supply an accurate preview.
  } finally {
    pathsLoading.value = false
  }
})
onUnmounted(() => pathsRequest.abort())

const handleSave = async () => {
  // Saving or recovering from an error can replace the store's config object.
  const source = props.isAlpha ? appConfig.config.alpha : appConfig.config.main
  Object.assign(source, model.value)
  try {
    await appConfig.saveNow()
    const editingCurrentBranch = props.isAlpha
      ? appConfig.config.branch === Branch.Alpha
      : appConfig.config.branch === Branch.Main
    if (editingCurrentBranch && kernelApiStore.running) {
      await kernelApiStore.restartCore()
    }
    handleSubmit()
  } catch (error: any) {
    message.error(error.message || error)
  }
}

const modalSlots = {
  action: () =>
    h(
      Button,
      {
        type: 'link',
        class: 'mr-auto',
        onClick: () => {
          const { env, args } = DefaultCoreConfig()
          model.value.env = env
          model.value.args = args
          message.success('common.success')
        },
      },
      () => t('common.reset'),
    ),
  cancel: () =>
    h(
      Button,
      {
        onClick: handleCancel,
      },
      () => t('common.cancel'),
    ),
  submit: () =>
    h(
      Button,
      {
        type: 'primary',
        onClick: handleSave,
      },
      () => t('common.save'),
    ),
}

defineExpose({ modalSlots })
</script>

<template>
  <div>
    <p v-if="pathsLoading" role="status" class="mb-8 text-12">{{ t('common.loading') }}</p>
    <p v-else-if="!runtimePaths" role="status" class="mb-8 text-12">
      {{ t('settings.kernel.config.previewUnavailable') }}
    </p>
    <Tabs v-model:active-key="activeKey" :items="tabs">
      <template #env>
        <Card
          v-if="runtimePaths && Object.keys(model.env).length"
          :title="t('settings.kernel.config.preview')"
          class="mb-8"
        >
          <dl class="core-runtime-preview">
            <template v-for="(value, key) in model.env" :key="key">
              <dt>{{ key }}</dt>
              <dd>
                <code>{{ previewCoreRuntimeValue(value, runtimePaths) }}</code>
              </dd>
            </template>
          </dl>
        </Card>
        <KeyValueEditor v-model="model.env" />
      </template>

      <template #args>
        <Card
          v-if="runtimePaths && model.args.length"
          :title="t('settings.kernel.config.preview')"
          class="mb-8"
        >
          <ol class="core-runtime-preview" data-runtime-args-preview>
            <li v-for="(arg, index) in model.args" :key="index">
              <code>{{ previewCoreRuntimeValue(arg, runtimePaths) }}</code>
            </li>
          </ol>
        </Card>
        <InputList v-model="model.args" />
      </template>
    </Tabs>
  </div>
</template>

<style scoped>
.core-runtime-preview {
  margin: 8px 0;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  font-size: 12px;
}
.core-runtime-preview dt {
  font-weight: bold;
}
.core-runtime-preview dd {
  margin: 0 0 8px;
}
.core-runtime-preview li {
  margin: 4px 0;
}
</style>
