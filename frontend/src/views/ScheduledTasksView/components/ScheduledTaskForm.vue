<script setup lang="ts">
import { ref, inject, h } from 'vue'
import { useI18n } from 'vue-i18n'

import { ScheduledTaskOptions } from '@/constant/app'
import { ScheduledTasksType } from '@/enums/app'
import { useScheduledTasksStore, useSubscribesStore, useRulesetsStore } from '@/stores'
import { alert, deepClone, formatDate, isValidCron, message, sampleID } from '@/utils'

import Button from '@/components/Button/index.vue'

import type { ScheduledTask } from '@/types/app'
import { IsNotificationAvailable, RequestNotificationAuthorization } from '@/bridge'

interface Props {
  id?: string
}

const props = defineProps<Props>()

const loading = ref(false)

const task = ref<ScheduledTask>({
  id: sampleID(),
  name: '',
  type: ScheduledTasksType.UpdateAllSubscription,
  subscriptions: [],
  rulesets: [],
  script: '',
  cron: '',
  notification: false,
  disabled: false,
  lastTime: 0,
  logLimit: 20,
})

const { t } = useI18n()
const scheduledTasksStore = useScheduledTasksStore()
const subscribesStore = useSubscribesStore()
const rulesetsStore = useRulesetsStore()

const handleCancel = inject('cancel') as any
const handleSubmit = inject('submit') as any

const handleSave = async () => {
  if (task.value.type === ScheduledTasksType.RunScript) {
    message.error('run::script is not supported by the backend scheduler')
    return
  }

  const { ok, reason } = isValidCron(task.value.cron)
  if (!ok) {
    message.error(reason)
    return
  }

  switch (task.value.type) {
    case ScheduledTasksType.UpdateSubscription:
      task.value.subscriptions = task.value.subscriptions.filter((id) =>
        subscribesStore.getSubscribeById(id),
      )
      break
    case ScheduledTasksType.UpdateRuleset:
      task.value.rulesets = task.value.rulesets.filter((id) => rulesetsStore.getRulesetById(id))
      break
  }
  task.value.logLimit = task.value.logLimit && task.value.logLimit > 0 ? task.value.logLimit : 20

  loading.value = true

  try {
    if (props.id) {
      await scheduledTasksStore.editScheduledTask(props.id, task.value)
    } else {
      await scheduledTasksStore.addScheduledTask(task.value)
    }
    await handleSubmit()
  } catch (error: any) {
    console.error(error)
    message.error(error)
  }

  loading.value = false
}

const handleUse = (list: string[], id: string) => {
  const idx = list.findIndex((v) => v === id)
  if (idx !== -1) {
    list.splice(idx, 1)
  } else {
    list.push(id)
  }
}

const handleValidate = () => {
  const { ok, reason } = isValidCron(task.value.cron)
  if (!ok) {
    message.error(reason)
    return
  }
  message.success('common.success')
}

const handleViewNextRuns = async () => {
  const { ok, reason, instance } = isValidCron(task.value.cron)
  if (!ok) {
    message.error(reason)
    return
  }
  const runs = await scheduledTasksStore.nextScheduledTaskRuns(task.value.cron)
  const list = runs.map((v: number, i: number) => {
    const index = (i + 1).toString().padStart(2, '0')
    return index + ' - '.repeat(14) + formatDate(v, 'YYYY/MM/DD HH:mm:ss')
  })
  void instance
  alert('Next Run Time', list.join('\n'))
}

const onNotificationChange = async (v: boolean) => {
  if (v) {
    try {
      if (!(await IsNotificationAvailable())) {
        throw t('scheduledtasks.notificationUnavailable')
      }
      const permission = await RequestNotificationAuthorization()
      if (permission !== 'granted') {
        throw t('scheduledtasks.notificationDenied')
      }
    } catch (error: any) {
      task.value.notification = false
      message.warn(error)
    }
  }
}

if (props.id) {
  const s = scheduledTasksStore.getScheduledTaskById(props.id)
  if (s) {
    task.value = deepClone(s)
    task.value.logLimit = task.value.logLimit && task.value.logLimit > 0 ? task.value.logLimit : 20
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
        loading: loading.value,
        disabled: !task.value.name || !task.value.cron,
        onClick: handleSave,
      },
      () => t('common.save'),
    ),
}

defineExpose({ modalSlots })
</script>

<template>
  <div>
    <div class="form-item">
      {{ t('scheduledtask.name') }} *
      <div class="min-w-[75%]">
        <Input v-model="task.name" autofocus class="w-full" />
      </div>
    </div>
    <div class="form-item">
      {{ t('scheduledtask.cron') }} *
      <div class="min-w-[75%]">
        <Input v-model="task.cron" :placeholder="t('scheduledtask.cronTips')" class="w-full">
          <template #suffix>
            <Button type="primary" size="small" @click="handleValidate">Validate</Button>
            <Button type="primary" size="small" class="ml-4" @click="handleViewNextRuns">
              Next Run Time
            </Button>
          </template>
        </Input>
      </div>
    </div>
    <div class="form-item">
      <div>{{ t('scheduledtask.type') }}</div>
      <Radio v-model="task.type" :options="ScheduledTaskOptions" />
    </div>
    <div class="form-item">
      {{ t('scheduledtask.notification') }}
      <Switch v-model="task.notification" @change="onNotificationChange" />
    </div>
    <div class="form-item">
      {{ t('scheduledtask.logLimit') }}
      <div class="min-w-[75%]">
        <Input v-model="task.logLimit" type="number" :min="1" class="w-full" />
      </div>
    </div>

    <div v-if="task.type === ScheduledTasksType.UpdateSubscription">
      <Divider>{{ t('scheduledtask.subscriptions') }}</Divider>
      <Empty v-if="subscribesStore.subscribes.length === 0" />
      <div class="grid grid-cols-3 gap-8">
        <Card
          v-for="s in subscribesStore.subscribes"
          :key="s.id"
          :title="s.name"
          :selected="task.subscriptions.includes(s.id)"
          @click="handleUse(task.subscriptions, s.id)"
        >
          <div class="text-12 line-clamp-2">{{ s.type }}</div>
        </Card>
      </div>
    </div>

    <div v-else-if="task.type === ScheduledTasksType.UpdateRuleset">
      <Divider>{{ t('scheduledtask.rulesets') }}</Divider>
      <Empty v-if="rulesetsStore.rulesets.length === 0" />
      <div class="grid grid-cols-3 gap-8">
        <Card
          v-for="r in rulesetsStore.rulesets"
          :key="r.id"
          :title="r.tag"
          :selected="task.rulesets.includes(r.id)"
          @click="handleUse(task.rulesets, r.id)"
        >
          <div class="text-12 line-clamp-2">{{ r.type }}</div>
        </Card>
      </div>
    </div>

    <div v-else-if="task.type === ScheduledTasksType.RunScript">
      <Divider>{{ t('scheduledtask.script') }}</Divider>
      <CodeViewer v-model="task.script" editable lang="javascript" />
    </div>
  </div>
</template>
