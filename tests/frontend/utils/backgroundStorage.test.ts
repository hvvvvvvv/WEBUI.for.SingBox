import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import {
  deleteBackgroundImage,
  readBackgroundImage,
  writeBackgroundImage,
} from '@/utils/backgroundImage'

describe('IndexedDB background transactions', () => {
  const oldImage = new Blob(['old'], { type: 'image/webp' })
  let stored: unknown
  let requests: any[]
  let transactions: any[]
  let db: any

  beforeEach(() => {
    stored = oldImage
    requests = []
    transactions = []
    db = {
      close: vi.fn(),
      transaction: vi.fn((_name: string, mode: string) => {
        const tx: any = { mode, request: { result: undefined }, commit: () => {} }
        tx.objectStore = () => ({
          get: () => {
            tx.request.result = stored
            return tx.request
          },
          put: (value: Blob) => {
            tx.commit = () => {
              stored = value
            }
            tx.request.result = 'image'
            return tx.request
          },
          delete: () => {
            tx.commit = () => {
              stored = undefined
            }
            return tx.request
          },
        })
        transactions.push(tx)
        return tx
      }),
    }
    vi.stubGlobal('indexedDB', {
      open: vi.fn(() => {
        const request: any = { result: db }
        requests.push(request)
        queueMicrotask(() => request.onsuccess())
        return request
      }),
    })
  })
  afterEach(() => vi.unstubAllGlobals())
  const finish = async () => {
    await Promise.resolve()
    await Promise.resolve()
    const tx = transactions.at(-1)!
    tx.commit()
    tx.oncomplete()
  }

  it('stores only the Blob, reads it through a new connection and removes it', async () => {
    const image = new Blob(['safe pixels'], { type: 'image/webp' })
    const save = writeBackgroundImage(image)
    await finish()
    await save
    expect(stored).toBe(image)
    const read = readBackgroundImage()
    await finish()
    expect(await read).toBe(image)
    const remove = deleteBackgroundImage()
    await finish()
    await remove
    expect(stored).toBeUndefined()
    expect(transactions.map((tx) => tx.mode)).toEqual(['readwrite', 'readonly', 'readwrite'])
    expect(db.close).toHaveBeenCalledTimes(3)
    expect(indexedDB.open).toHaveBeenCalledWith('webui-browser-appearance', 1)
  })

  it('waits for transaction commit and rejects an abort without replacing saved data', async () => {
    let resolved = false
    const saving = writeBackgroundImage(new Blob(['new'])).then(() => {
      resolved = true
    })
    await Promise.resolve()
    await Promise.resolve()
    expect(resolved).toBe(false)
    const tx = transactions[0]
    tx.error = new Error('Quota exceeded')
    tx.onabort()
    await expect(saving).rejects.toThrow('Quota exceeded')
    expect(stored).toBe(oldImage)
    expect(db.close).toHaveBeenCalledTimes(1)
  })

  it('rejects stored non-image values instead of treating them as a URL', async () => {
    stored = 'https://example.com/background.svg'
    const read = readBackgroundImage()
    await finish()
    await expect(read).rejects.toThrow('invalidImage')
  })

  it('closes an existing connection when another tab requests a version change', async () => {
    const read = readBackgroundImage()
    await finish()
    await read
    db.onversionchange()
    expect(db.close).toHaveBeenCalledTimes(2)
  })
})
