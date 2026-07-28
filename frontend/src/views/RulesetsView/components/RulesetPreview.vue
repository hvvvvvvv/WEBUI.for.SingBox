<script setup lang="ts">
import { inject, ref } from 'vue'

import { useRulesetsStore } from '@/stores'
import { message } from '@/utils'

interface Props {
  id: string
}

const props = defineProps<Props>()
const content = ref('')
const loading = ref(true)

const handleCancel = inject('cancel') as () => Promise<void>
const rulesetsStore = useRulesetsStore()

const loadContent = async () => {
  try {
    content.value = await rulesetsStore.getRulesetContent(props.id)
  } catch (error: any) {
    message.error(error.message || error)
    await handleCancel()
  } finally {
    loading.value = false
  }
}

loadContent()
</script>

<template>
  <div class="h-full">
    <div v-if="loading" class="h-full flex items-center justify-center">
      <Button loading type="text" />
    </div>
    <CodeViewer
      v-else
      :model-value="content"
      :editable="false"
      lang="json"
      class="h-full min-h-0"
    />
  </div>
</template>
