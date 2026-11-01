<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'

import rawRoutes from '@/router/routes'
import { useAppSettingsStore } from '@/stores/appSettings'

const { t } = useI18n()
const settings = useAppSettingsStore()
const route = useRoute()
const router = useRouter()
const navigation = ref<HTMLElement>()
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

const revealCurrentPage = () => {
  const container = navigation.value
  if (!container || window.innerWidth > 700) return
  const current = container.querySelector<HTMLElement>('[aria-current="page"]')
  if (!current) return
  const viewport = container.getBoundingClientRect()
  const button = current.getBoundingClientRect()
  if (button.left < viewport.left || button.width > viewport.width) {
    container.scrollLeft += button.left - viewport.left
  } else if (button.right > viewport.right) {
    container.scrollLeft += button.right - viewport.right
  }
}
onMounted(revealCurrentPage)
watch([() => route.name, routes], revealCurrentPage, { flush: 'post' })
</script>

<template>
  <nav ref="navigation" class="app-navigation flex flex-wrap items-center justify-center">
    <div v-for="r in routes" :key="r.path">
      <RouterLink v-slot="{ navigate, isActive }" :to="r.path" custom>
        <Button
          :type="isActive ? 'link' : 'text'"
          :icon="r.meta && r.meta.icon"
          :aria-current="isActive ? 'page' : undefined"
          @click="navigate"
        >
          {{ (r.meta && t(r.meta.name)) || r.name }}
        </Button>
      </RouterLink>
    </div>
  </nav>
</template>

<style scoped>
@media (max-width: 700px) {
  .app-navigation {
    flex-wrap: nowrap;
    justify-content: flex-start;
    flex-shrink: 0;
    width: 100%;
    gap: 4px;
    overflow-x: auto;
    overscroll-behavior-x: contain;
    scrollbar-width: thin;
    scrollbar-color: var(--scrollbar-thumb-bg) var(--scrollbar-track-bg);
  }
  .app-navigation > div {
    flex: 0 0 auto;
  }
  .app-navigation :deep(.gui-button) {
    min-height: 44px;
    box-sizing: border-box;
  }
}
</style>
