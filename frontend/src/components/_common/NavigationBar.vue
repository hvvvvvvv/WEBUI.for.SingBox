<script setup lang="ts">
import { computed, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'

import rawRoutes from '@/router/routes'
import { useAppSettingsStore } from '@/stores/appSettings'

const { t } = useI18n()
const settings = useAppSettingsStore()
const route = useRoute()
const router = useRouter()
const routes = computed(() =>
  rawRoutes.filter(
    (item) => item.name === 'Settings' || settings.app.pages.includes(String(item.name)),
  ),
)
watch(
  [() => route.name, routes],
  () => {
    if (route.name && !routes.value.some((item) => item.name === route.name)) {
      void router.replace(routes.value[0]?.path || '/settings').catch(() => undefined)
    }
  },
  { immediate: true },
)
</script>

<template>
  <div class="flex flex-wrap items-center justify-center">
    <div v-for="r in routes" :key="r.path">
      <RouterLink v-slot="{ navigate, isActive }" :to="r.path" custom>
        <Button :type="isActive ? 'link' : 'text'" :icon="r.meta && r.meta.icon" @click="navigate">
          {{ (r.meta && t(r.meta.name)) || r.name }}
        </Button>
      </RouterLink>
    </div>
  </div>
</template>
