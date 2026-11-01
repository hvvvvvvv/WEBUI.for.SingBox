import { createPinia, disposePinia, setActivePinia, type Pinia } from 'pinia'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

const io = vi.hoisted(() => ({ read: vi.fn(), write: vi.fn(), remove: vi.fn(), prepare: vi.fn() }))
vi.mock('@/utils/backgroundImage', () => ({
  BackgroundImageError: class extends Error {},
  readBackgroundImage: io.read,
  writeBackgroundImage: io.write,
  deleteBackgroundImage: io.remove,
  prepareBackgroundImage: io.prepare,
}))
import { BackgroundImageError } from '@/utils/backgroundImage'
import { useBackgroundStore } from '@/stores/background'

describe('browser-local background lifecycle', () => {
  let pinia: Pinia
  const oldImage = new Blob(['old'], { type: 'image/webp' })
  const newImage = new Blob(['new pixels'], { type: 'image/webp' })
  let persisted: Blob | undefined
  beforeEach(() => {
    vi.resetAllMocks()
    let sequence = 0
    vi.spyOn(URL, 'createObjectURL').mockImplementation(() => `blob:background-${++sequence}`)
    vi.spyOn(URL, 'revokeObjectURL').mockImplementation(() => {})
    persisted = oldImage
    io.read.mockImplementation(async () => persisted)
    io.write.mockImplementation(async (image: Blob) => {
      persisted = image
    })
    io.remove.mockImplementation(async () => {
      persisted = undefined
    })
    io.prepare.mockResolvedValue(newImage)
    pinia = createPinia()
    setActivePinia(pinia)
  })
  afterEach(() => {
    disposePinia(pinia)
    vi.restoreAllMocks()
  })

  it('keeps the displayed image until persistence commits and restores it in a new store', async () => {
    const store = useBackgroundStore()
    await store.setup()
    const oldURL = store.url
    const pending = Promise.withResolvers<void>()
    io.write.mockImplementationOnce(async (image: Blob) => {
      await pending.promise
      persisted = image
    })
    const saving = store.upload(new Blob(['input'], { type: 'image/png' }))
    await vi.waitFor(() => expect(io.write).toHaveBeenCalledWith(newImage))
    expect(store.url).toBe(oldURL)
    expect(URL.revokeObjectURL).not.toHaveBeenCalledWith(oldURL)
    expect(store.busy).toBe(true)
    pending.resolve()
    expect(await saving).toBe(true)
    expect(store.url).not.toBe(oldURL)
    expect(URL.revokeObjectURL).toHaveBeenCalledWith(oldURL)
    disposePinia(pinia)
    pinia = createPinia()
    setActivePinia(pinia)
    const reloaded = useBackgroundStore()
    await reloaded.setup()
    expect(URL.createObjectURL).toHaveBeenLastCalledWith(newImage)
    expect(reloaded.url).not.toBe('')
  })

  it('preserves the previous image when validation, save or deletion fails', async () => {
    const store = useBackgroundStore()
    await store.setup()
    const oldURL = store.url
    io.prepare.mockRejectedValueOnce(new BackgroundImageError('settings.background.invalidImage'))
    expect(await store.upload(oldImage)).toBe(false)
    expect(io.write).not.toHaveBeenCalled()
    expect(store.error).toBe('settings.background.invalidImage')
    io.write.mockRejectedValueOnce(new Error('quota exceeded'))
    expect(await store.upload(oldImage)).toBe(false)
    expect(store.error).toBe('settings.background.saveFailed')
    expect(store.url).toBe(oldURL)
    expect(persisted).toBe(oldImage)
    expect(URL.revokeObjectURL).toHaveBeenCalledWith('blob:background-2')
    io.remove.mockRejectedValueOnce(new Error('storage unavailable'))
    expect(await store.remove()).toBe(false)
    expect(store.error).toBe('settings.background.removeFailed')
    expect(store.url).toBe(oldURL)
    expect(URL.revokeObjectURL).not.toHaveBeenCalledWith(oldURL)
  })

  it('removes the persistent blob and releases the active URL only after success', async () => {
    const store = useBackgroundStore()
    await store.setup()
    const url = store.url
    expect(await store.remove()).toBe(true)
    expect(store.url).toBe('')
    expect(persisted).toBeUndefined()
    expect(URL.revokeObjectURL).toHaveBeenCalledExactlyOnceWith(url)
    store.dispose()
    await store.setup()
    expect(store.url).toBe('')
  })

  it('ignores a late initial read after disposal and allows a fresh load', async () => {
    const pending = Promise.withResolvers<Blob>()
    io.read.mockReturnValueOnce(pending.promise)
    const store = useBackgroundStore()
    const loading = store.setup()
    const secondLoad = store.setup()
    expect(io.read).toHaveBeenCalledTimes(1)
    store.dispose()
    pending.resolve(oldImage)
    await Promise.all([loading, secondLoad])
    expect(store.url).toBe('')
    expect(URL.createObjectURL).not.toHaveBeenCalled()
    await store.setup()
    expect(store.url).not.toBe('')
  })

  it('releases pending and active URLs on disposal without resurrecting them after a save', async () => {
    const store = useBackgroundStore()
    await store.setup()
    const pending = Promise.withResolvers<void>()
    io.write.mockReturnValueOnce(pending.promise)
    const saving = store.upload(oldImage)
    await vi.waitFor(() => expect(io.write).toHaveBeenCalled())
    expect(await store.remove()).toBe(false)
    store.dispose()
    expect(URL.revokeObjectURL).toHaveBeenCalledTimes(2)
    pending.resolve()
    expect(await saving).toBe(false)
    expect(store.url).toBe('')
    expect(URL.revokeObjectURL).toHaveBeenCalledTimes(2)
  })
})
