import { type Directive, type DirectiveBinding } from 'vue'

import { useAppStore } from '@/stores'
import { debounce, getZoomLevel } from '@/utils'

interface TipsState {
  message: string
  overflowOnly: boolean
  show: ReturnType<typeof debounce>
}

const states = new WeakMap<HTMLElement, TipsState>()

const isOverflowing = (el: HTMLElement) =>
  el.scrollWidth > el.clientWidth || el.scrollHeight > el.clientHeight

const canShow = (el: HTMLElement, state: TipsState) =>
  !!state.message && (!state.overflowOnly || isOverflowing(el))

const updateState = (el: HTMLElement, binding: DirectiveBinding) => {
  const state = states.get(el)
  if (!state) return
  state.message = binding.value ? String(binding.value) : ''
  state.overflowOnly = !!binding.modifiers.overflow
}

export default {
  mounted(el: HTMLElement, binding: DirectiveBinding) {
    const appStore = useAppStore()

    const delay = binding.modifiers.fast ? 200 : 500
    const state: TipsState = {
      message: binding.value ? String(binding.value) : '',
      overflowOnly: !!binding.modifiers.overflow,
      show: debounce((x: number, y: number) => {
        if (el.dataset.showTips === 'true' && canShow(el, state)) {
          const zoom = getZoomLevel()
          appStore.tipsPosition = { x: x / zoom, y: y / zoom }
          appStore.tipsMessage = state.message
          appStore.tipsShow = true
        }
      }, delay),
    }
    states.set(el, state)

    el.onmouseenter = (e: MouseEvent) => {
      if (canShow(el, state)) {
        el.dataset.showTips = 'true'
        state.show(e.clientX, e.clientY)
      }
    }

    el.onmouseleave = () => {
      state.show.cancel()
      appStore.tipsShow = false
      el.dataset.showTips = 'false'
    }
  },
  updated(el: HTMLElement, binding: DirectiveBinding) {
    const appStore = useAppStore()
    updateState(el, binding)
    const state = states.get(el)
    if (!state || el.dataset.showTips !== 'true') return

    if (canShow(el, state)) {
      appStore.tipsMessage = state.message
    } else {
      state.show.cancel()
      appStore.tipsShow = false
      el.dataset.showTips = 'false'
    }
  },
  beforeUnmount(el: HTMLElement) {
    const appStore = useAppStore()
    const state = states.get(el)
    state?.show.cancel()
    if (el.dataset.showTips === 'true') appStore.tipsShow = false
    el.dataset.showTips = 'false'
    el.onmouseenter = null
    el.onmouseleave = null
    states.delete(el)
  },
} as Directive
