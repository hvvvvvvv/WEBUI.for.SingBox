<script setup lang="ts">
import logo from '@/assets/logo'
import { useAppStore } from '@/stores'
import { APP_TITLE } from '@/utils/env'

const appStore = useAppStore()
</script>

<template>
  <div class="h-full flex flex-col items-center justify-center">
    <img :src="logo" class="w-128" draggable="false" />
    <div class="text-24 font-bold mt-16 mb-24">{{ APP_TITLE }}</div>
    <div class="py-16">{{ appStore.currentVersion }}</div>
    <div class="rotation">
      <slot></slot>
    </div>
  </div>
</template>
