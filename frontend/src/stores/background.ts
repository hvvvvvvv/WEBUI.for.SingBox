import { defineStore } from 'pinia'
import { nextTick, onScopeDispose, ref } from 'vue'

import {
  BackgroundImageError,
  deleteBackgroundImage,
  prepareBackgroundImage,
  readBackgroundImage,
  writeBackgroundImage,
} from '@/utils/backgroundImage'

export const useBackgroundStore = defineStore('browser-background', () => {
  const url = ref('')
  const busy = ref(false)
  const error = ref('')
  let initialized = false
  let revision = 0
  let loading: Promise<void> | undefined
  const ownedURLs = new Set<string>()
  const createURL = (image: Blob) => {
    const result = URL.createObjectURL(image)
    ownedURLs.add(result)
    return result
  }
  const revokeURL = (value: string) => {
    if (ownedURLs.delete(value)) URL.revokeObjectURL(value)
  }

  const replaceURL = async (next: string) => {
    const previous = url.value
    url.value = next
    await nextTick()
    revokeURL(previous)
  }

  const setup = () => {
    if (loading) return loading
    if (initialized) return Promise.resolve()
    const current = ++revision
    busy.value = true
    loading = (async () => {
      try {
        const image = await readBackgroundImage()
        if (current === revision && image) await replaceURL(createURL(image))
      } catch {
        if (current === revision) error.value = 'settings.background.loadFailed'
      } finally {
        if (current === revision) {
          busy.value = false
          initialized = true
          loading = undefined
        }
      }
    })()
    return loading
  }

  const upload = async (file: Blob) => {
    if (busy.value) return false
    const current = ++revision
    busy.value = true
    error.value = ''
    let nextURL = ''
    try {
      const image = await prepareBackgroundImage(file)
      if (current !== revision) return false
      nextURL = createURL(image)
      await writeBackgroundImage(image)
      if (current !== revision) return false
      await replaceURL(nextURL)
      if (current !== revision) return false
      nextURL = ''
      initialized = true
      return true
    } catch (value) {
      if (current === revision)
        error.value =
          value instanceof BackgroundImageError ? value.message : 'settings.background.saveFailed'
      return false
    } finally {
      revokeURL(nextURL)
      if (current === revision) busy.value = false
    }
  }

  const remove = async () => {
    if (busy.value) return false
    const current = ++revision
    busy.value = true
    error.value = ''
    try {
      await deleteBackgroundImage()
      if (current !== revision) return false
      await replaceURL('')
      if (current !== revision) return false
      initialized = true
      return true
    } catch {
      if (current === revision) error.value = 'settings.background.removeFailed'
      return false
    } finally {
      if (current === revision) busy.value = false
    }
  }

  const dispose = () => {
    revision++
    for (const value of ownedURLs) revokeURL(value)
    url.value = ''
    busy.value = false
    initialized = false
    loading = undefined
    error.value = ''
  }
  onScopeDispose(dispose)
  return { url, busy, error, setup, upload, remove, dispose }
})
