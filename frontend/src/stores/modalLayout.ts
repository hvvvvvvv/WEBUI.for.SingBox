import { defineStore } from 'pinia'
import { computed, onScopeDispose, ref, shallowRef } from 'vue'
import { useAppSettingsStore } from './appSettings'

interface ModalTab {
  id: string
  title: () => string
  activate: () => void
  close: () => unknown
}

export const useModalLayoutStore = defineStore('modal-layout', () => {
  const settings = useAppSettingsStore()
  const tabs = shallowRef<ModalTab[]>([])
  const activeTab = ref('')
  const hostReady = ref(false)
  const media = window.matchMedia('(min-width: 1100px)')
  const wideScreen = ref(media.matches)
  const onResize = (event: MediaQueryListEvent) => (wideScreen.value = event.matches)
  media.addEventListener('change', onResize)
  onScopeDispose(() => media.removeEventListener('change', onResize))
  const splitActive = computed(
    () =>
      settings.app.debugModalSideBySide &&
      wideScreen.value &&
      hostReady.value &&
      tabs.value.length > 0,
  )
  const register = (tab: ModalTab) => {
    if (!tabs.value.some((item) => item.id === tab.id)) tabs.value = [...tabs.value, tab]
    tab.activate()
  }
  const unregister = (id: string) => {
    const index = tabs.value.findIndex((tab) => tab.id === id)
    if (index < 0) return
    tabs.value = tabs.value.filter((tab) => tab.id !== id)
    if (activeTab.value === id) {
      const next = tabs.value[Math.min(index, tabs.value.length - 1)]
      activeTab.value = next?.id || ''
      next?.activate()
    }
  }
  return { tabs, activeTab, hostReady, wideScreen, splitActive, register, unregister }
})
