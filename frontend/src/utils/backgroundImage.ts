export const BackgroundImageMaxBytes = 10 * 1024 * 1024
export const BackgroundImageMaxPixels = 24_000_000
export const BackgroundImageMaxDimension = 8192
export const BackgroundImageOutputDimension = 2048

export class BackgroundImageError extends Error {}

const invalidImage = () => new BackgroundImageError('settings.background.invalidImage')
const validDimensions = (width: number, height: number) => {
  if (
    !width ||
    !height ||
    width > BackgroundImageMaxDimension ||
    height > BackgroundImageMaxDimension ||
    width * height > BackgroundImageMaxPixels
  ) {
    throw new BackgroundImageError('settings.background.tooManyPixels')
  }
}

// Check dimensions before asking the browser to allocate a decoded bitmap.
// The browser decoder still validates the complete image before it is saved.
const imageDimensions = (data: Uint8Array, mime: string): [number, number] => {
  const view = new DataView(data.buffer, data.byteOffset, data.byteLength)
  const text = (offset: number, size: number) =>
    String.fromCharCode(...data.subarray(offset, offset + size))
  if (
    mime === 'image/png' &&
    data.length >= 33 &&
    data[0] === 137 &&
    text(1, 7) === 'PNG\r\n\x1a\n' &&
    text(12, 4) === 'IHDR'
  ) {
    return [view.getUint32(16), view.getUint32(20)]
  }
  if (mime === 'image/jpeg' && data[0] === 255 && data[1] === 216) {
    let offset = 2
    while (offset + 3 < data.length) {
      if (data[offset++] !== 255) break
      while (data[offset] === 255) offset++
      const marker = data[offset++]!
      if (marker === 218 || marker === 217 || offset + 2 > data.length) break
      const length = view.getUint16(offset)
      if (length < 2 || offset + length > data.length) break
      if (
        [192, 193, 194, 195, 197, 198, 199, 201, 202, 203, 205, 206, 207].includes(marker) &&
        length >= 8
      ) {
        return [view.getUint16(offset + 5), view.getUint16(offset + 3)]
      }
      offset += length
    }
  }
  if (
    mime === 'image/webp' &&
    data.length >= 25 &&
    text(0, 4) === 'RIFF' &&
    text(8, 4) === 'WEBP' &&
    view.getUint32(4, true) + 8 <= data.length &&
    view.getUint32(16, true) + 20 <= data.length
  ) {
    const chunk = text(12, 4)
    if (chunk === 'VP8X' && data.length >= 30) {
      const uint24 = (offset: number) =>
        data[offset]! | (data[offset + 1]! << 8) | (data[offset + 2]! << 16)
      return [uint24(24) + 1, uint24(27) + 1]
    }
    if (
      chunk === 'VP8 ' &&
      data.length >= 30 &&
      data[23] === 157 &&
      data[24] === 1 &&
      data[25] === 42
    ) {
      return [view.getUint16(26, true) & 0x3fff, view.getUint16(28, true) & 0x3fff]
    }
    if (chunk === 'VP8L' && data[20] === 47) {
      const bits = view.getUint32(21, true)
      return [(bits & 0x3fff) + 1, ((bits >>> 14) & 0x3fff) + 1]
    }
  }
  throw invalidImage()
}

export const prepareBackgroundImage = async (file: Blob): Promise<Blob> => {
  if (!['image/png', 'image/jpeg', 'image/webp'].includes(file.type)) {
    throw new BackgroundImageError('settings.background.unsupportedType')
  }
  if (!file.size || file.size > BackgroundImageMaxBytes) {
    throw new BackgroundImageError('settings.background.tooLarge')
  }
  const [width, height] = imageDimensions(new Uint8Array(await file.arrayBuffer()), file.type)
  validDimensions(width, height)

  let source: ImageBitmap | HTMLImageElement | undefined
  let decodeURL = ''
  let canvas: HTMLCanvasElement | undefined
  try {
    if (typeof createImageBitmap === 'function') {
      source = await createImageBitmap(file)
    } else {
      const image = new Image()
      decodeURL = URL.createObjectURL(file)
      image.src = decodeURL
      await image.decode()
      source = image
    }
    const decodedWidth = 'naturalWidth' in source ? source.naturalWidth : source.width
    const decodedHeight = 'naturalHeight' in source ? source.naturalHeight : source.height
    validDimensions(decodedWidth, decodedHeight)
    const scale = Math.min(
      1,
      BackgroundImageOutputDimension / Math.max(decodedWidth, decodedHeight),
    )
    canvas = document.createElement('canvas')
    canvas.width = Math.max(1, Math.round(decodedWidth * scale))
    canvas.height = Math.max(1, Math.round(decodedHeight * scale))
    const context = canvas.getContext('2d')
    if (!context) throw invalidImage()
    context.drawImage(source, 0, 0, canvas.width, canvas.height)
    // Re-encoding keeps pixels only: no original EXIF, URLs or animation metadata.
    const image = await new Promise<Blob>((resolve, reject) => {
      canvas!.toBlob((blob) => (blob ? resolve(blob) : reject(invalidImage())), 'image/webp', 0.85)
    })
    if (image.type !== 'image/webp' || !image.size || image.size > BackgroundImageMaxBytes)
      throw invalidImage()
    return image
  } catch (error) {
    if (error instanceof BackgroundImageError) throw error
    throw invalidImage()
  } finally {
    if (source && 'close' in source) source.close()
    if (decodeURL) URL.revokeObjectURL(decodeURL)
    if (canvas) canvas.width = canvas.height = 0
  }
}

const databaseName = 'webui-browser-appearance'
const objectStore = 'background'

const openDatabase = () =>
  new Promise<IDBDatabase>((resolve, reject) => {
    const request = indexedDB.open(databaseName, 1)
    let blocked = false
    request.onupgradeneeded = () => request.result.createObjectStore(objectStore)
    request.onerror = () => reject(request.error)
    request.onblocked = () => {
      blocked = true
      reject(new Error('Background storage is blocked'))
    }
    request.onsuccess = () => {
      const db = request.result
      if (blocked) {
        db.close()
        return
      }
      db.onversionchange = () => db.close()
      resolve(db)
    }
  })

const transaction = async (
  mode: IDBTransactionMode,
  action: (store: IDBObjectStore) => IDBRequest,
): Promise<unknown> => {
  const db = await openDatabase()
  try {
    return await new Promise((resolve, reject) => {
      const tx = db.transaction(objectStore, mode)
      const request = action(tx.objectStore(objectStore))
      // A successful put request is not durable until its transaction completes.
      tx.oncomplete = () => resolve(request.result)
      tx.onerror = () => reject(tx.error || request.error)
      tx.onabort = () => reject(tx.error || new Error('Background storage transaction aborted'))
    })
  } finally {
    db.close()
  }
}

export const readBackgroundImage = async (): Promise<Blob | undefined> => {
  const value = await transaction('readonly', (store) => store.get('image'))
  if (value === undefined) return undefined
  if (
    !(value instanceof Blob) ||
    value.type !== 'image/webp' ||
    !value.size ||
    value.size > BackgroundImageMaxBytes
  )
    throw invalidImage()
  return value
}
export const writeBackgroundImage = (image: Blob) =>
  transaction('readwrite', (store) => store.put(image, 'image'))
export const deleteBackgroundImage = () =>
  transaction('readwrite', (store) => store.delete('image'))
