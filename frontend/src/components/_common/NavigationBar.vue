<script setup lang="ts">
import { useI18n } from 'vue-i18n'

import routes from '@/router/routes'

const { t } = useI18n()
</script>

<template>
  <div class="flex items-center justify-center">
    <div v-for="r in routes" :key="r.path">
      <RouterLink v-slot="{ navigate, isActive }" :to="r.path" custom>
        <Button :type="isActive ? 'link' : 'text'" :icon="r.meta && r.meta.icon" @click="navigate">
          {{ (r.meta && t(r.meta.name)) || r.name }}
        </Button>
      </RouterLink>
    </div>
  </div>
</template>
