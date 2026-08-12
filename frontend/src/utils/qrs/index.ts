import { deflate, gzip } from 'pako'

import {
  concatBytes,
  crc32,
  encodeBase64,
  encodeInt32BE,
  encodeInt64BE,
  encodeLatin1,
  encodeString,
  encodeUint32BE,
} from './binary'
import { KotlinRandom, sampleSolitonDegree, selectKotlinIndices } from './random'
import {
  ProfileType,
  QRS_DEFAULT_SLICE_SIZE,
  QRS_RECOVERY_FACTOR,
  QRS_URL_PREFIX,
  type ProfileContentInput,
  type QRSBundle,
  type QRSEncodeOptions,
  type QRSFrame,
} from './types'

export * from './binary'
export * from './profileConfig'
export * from './random'
export * from './types'

const MESSAGE_TYPE_PROFILE_CONTENT = 3
const PROFILE_CONTENT_VERSION = 1
const CONTENT_TYPE_BINARY = 'application/octet-stream'

const validatePositiveInt = (value: number, label: string): number => {
  if (!Number.isSafeInteger(value) || value <= 0 || value > 0x7fffffff) {
    throw new RangeError(`${label} must be a positive int32`)
  }
  return value
}

export const encodeProfileContent = (input: ProfileContentInput): Uint8Array => {
  const body: Uint8Array[] = [
    encodeString(input.name),
    encodeInt32BE(input.type),
    encodeString(input.config),
  ]

  if (input.type !== ProfileType.Local) {
    body.push(encodeString(input.remotePath))
  }
  if (input.type === ProfileType.Remote) {
    body.push(
      Uint8Array.of(input.autoUpdate ? 1 : 0),
      encodeInt32BE(input.autoUpdateInterval),
      encodeInt64BE(input.lastUpdated),
    )
  }

  return concatBytes(
    Uint8Array.of(MESSAGE_TYPE_PROFILE_CONTENT, PROFILE_CONTENT_VERSION),
    gzip(concatBytes(...body)),
  )
}

const escapeJson = (value: string): string =>
  value
    .replace(/\\/g, '\\\\')
    .replace(/"/g, '\\"')
    .replace(/\n/g, '\\n')
    .replace(/\r/g, '\\r')
    .replace(/\t/g, '\\t')

export const appendQRSFileMeta = (bpfData: Uint8Array, profileName: string): Uint8Array => {
  const metadata = `{"filename":"${escapeJson(`${profileName}.bpf`)}","contentType":"${CONTENT_TYPE_BINARY}"}`
  const metadataBytes = encodeLatin1(metadata)
  return concatBytes(
    encodeUint32BE(metadataBytes.length),
    metadataBytes,
    encodeUint32BE(bpfData.length),
    bpfData,
  )
}

export const calculateRequiredFrames = (
  dataSize: number,
  sliceSize = QRS_DEFAULT_SLICE_SIZE,
): number => {
  validatePositiveInt(sliceSize, 'sliceSize')
  if (!Number.isSafeInteger(dataSize) || dataSize < 0) {
    throw new RangeError('dataSize must be a non-negative safe integer')
  }
  const blocks = Math.ceil(dataSize / sliceSize)
  if (blocks === 0) return 1
  return Math.max(Math.floor(blocks * QRS_RECOVERY_FACTOR), blocks + 5)
}

interface EncodedBlock {
  degree: number
  indices: number[]
  totalBlocks: number
  compressedSize: number
  checksum: number
  data: Uint8Array
}

const buildPayload = (block: EncodedBlock): Uint8Array => {
  const result = new Uint8Array(16 + block.indices.length * 4 + block.data.length)
  const view = new DataView(result.buffer)
  let offset = 0
  view.setInt32(offset, block.degree, true)
  offset += 4
  for (const index of block.indices) {
    view.setInt32(offset, index, true)
    offset += 4
  }
  view.setInt32(offset, block.totalBlocks, true)
  offset += 4
  view.setInt32(offset, block.compressedSize, true)
  offset += 4
  view.setUint32(offset, block.checksum, true)
  offset += 4
  result.set(block.data, offset)
  return result
}

const xorBlocks = (blocks: Uint8Array[], indices: number[]): Uint8Array => {
  const result = blocks[indices[0]!]!.slice()
  for (let position = 1; position < indices.length; position++) {
    const source = blocks[indices[position]!]!
    for (let offset = 0; offset < result.length; offset++) {
      result[offset] = result[offset]! ^ source[offset]!
    }
  }
  return result
}

export const encodeQRS = (wrappedData: Uint8Array, options: QRSEncodeOptions = {}): QRSFrame[] => {
  const sliceSize = validatePositiveInt(options.sliceSize ?? QRS_DEFAULT_SLICE_SIZE, 'sliceSize')
  const compressedData = deflate(wrappedData)
  if (compressedData.length > 0x7fffffff) throw new RangeError('compressed data is too large')

  const totalBlocks = Math.ceil(compressedData.length / sliceSize)
  if (totalBlocks <= 0 || totalBlocks > 0x7fffffff) {
    throw new RangeError('totalBlocks is out of int32 range')
  }

  const paddedData = new Uint8Array(totalBlocks * sliceSize)
  paddedData.set(compressedData)
  const blocks = Array.from({ length: totalBlocks }, (_, index) =>
    paddedData.slice(index * sliceSize, (index + 1) * sliceSize),
  )
  const checksum = (crc32(wrappedData) ^ totalBlocks) >>> 0
  const frameCount = validatePositiveInt(
    options.frameCount ?? calculateRequiredFrames(wrappedData.length, sliceSize),
    'frameCount',
  )

  return Array.from({ length: frameCount }, (_, frameIndex): QRSFrame => {
    const random = new KotlinRandom(BigInt(frameIndex))
    const degree = sampleSolitonDegree(totalBlocks, random)
    const indices = selectKotlinIndices(totalBlocks, degree, random)
    const payload = buildPayload({
      degree,
      indices,
      totalBlocks,
      compressedSize: compressedData.length,
      checksum,
      data: xorBlocks(blocks, indices),
    })
    const base64 = encodeBase64(payload)
    return {
      base64,
      content: QRS_URL_PREFIX + base64,
      frameIndex,
      totalBlocks,
    }
  })
}

export const buildProfileQRS = (
  input: ProfileContentInput,
  options: QRSEncodeOptions = {},
): QRSBundle => {
  const bpfData = encodeProfileContent(input)
  const wrappedData = appendQRSFileMeta(bpfData, input.name)
  const frames = encodeQRS(wrappedData, options)
  return {
    bpfData,
    wrappedData,
    frames,
    frameCount: frames.length,
    totalBlocks: frames[0]!.totalBlocks,
  }
}
