import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { BackgroundImageMaxBytes, prepareBackgroundImage } from '@/utils/backgroundImage'

const png = (width = 4096, height = 2048) => {
  const bytes = new Uint8Array(33)
  bytes.set([137, 80, 78, 71, 13, 10, 26, 10], 0)
  bytes.set([73, 72, 68, 82], 12)
  const view = new DataView(bytes.buffer)
  view.setUint32(16, width)
  view.setUint32(20, height)
  return new Blob([bytes], { type: 'image/png' })
}
const jpeg = () =>
  new Blob(
    [
      new Uint8Array([
        255, 216, 255, 225, 0, 8, 69, 88, 73, 70, 0, 0, 255, 192, 0, 8, 8, 0, 32, 0, 64, 1,
      ]),
    ],
    { type: 'image/jpeg' },
  )
const webp = (chunk: 'VP8X' | 'VP8 ' | 'VP8L') => {
  const bytes = new Uint8Array(30)
  const view = new DataView(bytes.buffer)
  const put = (text: string, offset: number) =>
    bytes.set(
      [...text].map((char) => char.charCodeAt(0)),
      offset,
    )
  put('RIFF', 0)
  view.setUint32(4, 22, true)
  put('WEBP', 8)
  put(chunk, 12)
  view.setUint32(16, 10, true)
  if (chunk === 'VP8X') {
    bytes[24] = 63
    bytes[27] = 31
  }
  if (chunk === 'VP8 ') {
    bytes.set([157, 1, 42], 23)
    view.setUint16(26, 64, true)
    view.setUint16(28, 32, true)
  }
  if (chunk === 'VP8L') {
    bytes[20] = 47
    view.setUint32(21, 63 | (31 << 14), true)
  }
  return new Blob([bytes], { type: 'image/webp' })
}

describe('local background image validation and re-encoding', () => {
  const bitmap = { width: 4096, height: 2048, close: vi.fn() }
  const drawImage = vi.fn()
  const output = new Blob(['encoded pixels'], { type: 'image/webp' })
  const canvas = { width: 0, height: 0, getContext: vi.fn(), toBlob: vi.fn() }
  beforeEach(() => {
    vi.resetAllMocks()
    bitmap.width = 4096
    bitmap.height = 2048
    vi.stubGlobal('createImageBitmap', vi.fn().mockResolvedValue(bitmap))
    canvas.getContext.mockReturnValue({ drawImage })
    canvas.toBlob.mockImplementation((callback: BlobCallback) => callback(output))
    vi.stubGlobal('document', { createElement: vi.fn().mockReturnValue(canvas) })
  })
  afterEach(() => {
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  it('resizes to 2048 pixels, writes a new WebP blob and releases the decoded image', async () => {
    const input = png()
    expect(await prepareBackgroundImage(input)).toBe(output)
    expect(createImageBitmap).toHaveBeenCalledWith(input)
    expect(drawImage).toHaveBeenCalledWith(bitmap, 0, 0, 2048, 1024)
    expect(canvas.toBlob).toHaveBeenCalledWith(expect.any(Function), 'image/webp', 0.85)
    expect(bitmap.close).toHaveBeenCalledTimes(1)
    expect(canvas.width).toBe(0)
    expect(canvas.height).toBe(0)
  })

  it.each([
    ['JPEG with EXIF', jpeg],
    ['WebP extended', () => webp('VP8X')],
    ['WebP lossy', () => webp('VP8 ')],
    ['WebP lossless', () => webp('VP8L')],
  ] as const)(
    'decodes and re-encodes %s without retaining original metadata',
    async (_, makeImage) => {
      bitmap.width = 64
      bitmap.height = 32
      const input = makeImage()
      const result = await prepareBackgroundImage(input)
      expect(result).not.toBe(input)
      expect(await result.text()).toBe('encoded pixels')
      expect(drawImage).toHaveBeenCalledWith(bitmap, 0, 0, 64, 32)
    },
  )

  it('rejects unsupported, disguised, oversized and excessive-pixel files before decoding', async () => {
    await expect(
      prepareBackgroundImage(new Blob(['<svg/>'], { type: 'image/svg+xml' })),
    ).rejects.toThrow('unsupportedType')
    await expect(
      prepareBackgroundImage(new Blob(['<svg/>'], { type: 'image/png' })),
    ).rejects.toThrow('invalidImage')
    await expect(
      prepareBackgroundImage(
        new Blob([new Uint8Array(BackgroundImageMaxBytes + 1)], { type: 'image/png' }),
      ),
    ).rejects.toThrow('tooLarge')
    await expect(prepareBackgroundImage(png(8000, 8000))).rejects.toThrow('tooManyPixels')
    await expect(prepareBackgroundImage(png(8193, 1))).rejects.toThrow('tooManyPixels')
    expect(createImageBitmap).not.toHaveBeenCalled()
  })

  it('rejects actual decoding failures and checks decoded dimensions too', async () => {
    vi.mocked(createImageBitmap).mockRejectedValueOnce(new Error('invalid CRC'))
    await expect(prepareBackgroundImage(png())).rejects.toThrow('invalidImage')
    expect(canvas.toBlob).not.toHaveBeenCalled()
    bitmap.width = 9000
    await expect(prepareBackgroundImage(png())).rejects.toThrow('tooManyPixels')
    expect(bitmap.close).toHaveBeenCalledTimes(1)
  })

  it('rejects failed or unsupported WebP encoding and frees canvas resources', async () => {
    canvas.toBlob.mockImplementationOnce((callback: BlobCallback) => callback(null))
    await expect(prepareBackgroundImage(png())).rejects.toThrow('invalidImage')
    canvas.toBlob.mockImplementationOnce((callback: BlobCallback) =>
      callback(new Blob(['fallback'], { type: 'image/png' })),
    )
    await expect(prepareBackgroundImage(png())).rejects.toThrow('invalidImage')
    expect(bitmap.close).toHaveBeenCalledTimes(2)
    expect(canvas.width).toBe(0)
  })

  it('revokes temporary decode URLs with the image-element fallback on success and failure', async () => {
    vi.stubGlobal('createImageBitmap', undefined)
    const decode = vi.fn().mockResolvedValue(undefined)
    vi.stubGlobal(
      'Image',
      class {
        naturalWidth = 64
        naturalHeight = 32
        src = ''
        decode = decode
      },
    )
    vi.spyOn(URL, 'createObjectURL').mockReturnValue('blob:decode')
    const revoke = vi.spyOn(URL, 'revokeObjectURL').mockImplementation(() => {})
    expect(await prepareBackgroundImage(png())).toBe(output)
    expect(revoke).toHaveBeenCalledExactlyOnceWith('blob:decode')
    decode.mockRejectedValueOnce(new Error('decode failed'))
    await expect(prepareBackgroundImage(png())).rejects.toThrow('invalidImage')
    expect(revoke).toHaveBeenCalledTimes(2)
  })
})
