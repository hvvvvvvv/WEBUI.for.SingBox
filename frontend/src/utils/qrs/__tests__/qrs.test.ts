import { gunzipSync, inflateSync } from 'node:zlib'

import { describe, expect, it } from 'vitest'

import {
  appendQRSFileMeta,
  buildProfileQRS,
  calculateRequiredFrames,
  crc32,
  encodeProfileContent,
  encodeQRS,
  KotlinRandom,
  ProfileType,
  QRS_URL_PREFIX,
  sampleSolitonDegree,
  selectKotlinIndices,
  type QRSFrame,
} from '..'

class BinaryReader {
  offset = 0

  constructor(readonly data: Uint8Array) {}

  byte() {
    return this.data[this.offset++]!
  }

  uvarint() {
    let value = 0n
    let shift = 0n
    while (true) {
      const next = this.byte()
      value |= BigInt(next & 0x7f) << shift
      if ((next & 0x80) === 0) return Number(value)
      shift += 7n
    }
  }

  bytes(length: number) {
    const result = this.data.slice(this.offset, this.offset + length)
    this.offset += length
    return result
  }

  string() {
    return new TextDecoder().decode(this.bytes(this.uvarint()))
  }

  int32BE() {
    const value = new DataView(this.data.buffer, this.data.byteOffset + this.offset, 4).getInt32(
      0,
      false,
    )
    this.offset += 4
    return value
  }

  int64BE() {
    const value = new DataView(this.data.buffer, this.data.byteOffset + this.offset, 8).getBigInt64(
      0,
      false,
    )
    this.offset += 8
    return value
  }
}

interface ParsedFrame {
  indices: Set<number>
  totalBlocks: number
  compressedSize: number
  checksum: number
  data: Uint8Array
}

const parseFrame = (frame: QRSFrame): ParsedFrame => {
  const payload = Buffer.from(frame.base64, 'base64')
  const view = new DataView(payload.buffer, payload.byteOffset, payload.byteLength)
  let offset = 0
  const degree = view.getInt32(offset, true)
  offset += 4
  const indices = new Set<number>()
  for (let index = 0; index < degree; index++) {
    indices.add(view.getInt32(offset, true))
    offset += 4
  }
  const totalBlocks = view.getInt32(offset, true)
  offset += 4
  const compressedSize = view.getInt32(offset, true)
  offset += 4
  const checksum = view.getUint32(offset, true)
  offset += 4
  return {
    indices,
    totalBlocks,
    compressedSize,
    checksum,
    data: payload.subarray(offset),
  }
}

const xorInto = (target: Uint8Array, source: Uint8Array) => {
  for (let index = 0; index < target.length; index++) {
    target[index] = target[index]! ^ source[index]!
  }
}

const xorIndices = (target: Set<number>, source: Set<number>) => {
  for (const index of source) {
    if (target.has(index)) target.delete(index)
    else target.add(index)
  }
}

const recoverWrappedData = (frames: QRSFrame[]): Uint8Array => {
  const first = parseFrame(frames[0]!)
  const pivots = new Map<number, ParsedFrame>()

  for (const encodedFrame of frames) {
    const parsed = parseFrame(encodedFrame)
    const equation: ParsedFrame = {
      ...parsed,
      indices: new Set(parsed.indices),
      data: parsed.data.slice(),
    }

    while (equation.indices.size > 0) {
      const pivot = Math.min(...equation.indices)
      const existing = pivots.get(pivot)
      if (!existing) {
        pivots.set(pivot, equation)
        break
      }
      xorIndices(equation.indices, existing.indices)
      xorInto(equation.data, existing.data)
    }
    if (pivots.size === first.totalBlocks) break
  }

  expect(pivots.size).toBe(first.totalBlocks)
  const blocks: Uint8Array[] = Array(first.totalBlocks)
  for (let pivot = first.totalBlocks - 1; pivot >= 0; pivot--) {
    const equation = pivots.get(pivot)!
    const block = equation.data.slice()
    for (const index of equation.indices) {
      if (index > pivot) xorInto(block, blocks[index]!)
    }
    blocks[pivot] = block
  }

  const padded = new Uint8Array(first.totalBlocks * first.data.length)
  blocks.forEach((block, index) => padded.set(block, index * block.length))
  return Uint8Array.from(inflateSync(padded.slice(0, first.compressedSize)))
}

describe('Kotlin random compatibility', () => {
  it('matches Kotlin 2.3.20 XorWow nextInt vectors', () => {
    const vectors = [
      [-1934310868, 1409199696, -649160781, -1454478562, -1464141532],
      [600123930, -1531902544, -527218591, -1598672019, -541439398],
      [-665788237, 1221660678, -382311875, 15345335, 285846983],
    ]

    vectors.forEach((expected, seed) => {
      const random = new KotlinRandom(BigInt(seed))
      expect(Array.from({ length: expected.length }, () => random.nextInt())).toEqual(expected)
    })
  })

  it('matches SFA degree and shuffled-index vectors', () => {
    const vectors = [
      [2, [9, 0]],
      [2, [5, 4]],
      [4, [4, 2, 1, 8]],
      [2, [8, 7]],
      [1, [5]],
      [3, [0, 1, 6]],
      [2, [5, 4]],
      [8, [1, 7, 9, 6, 4, 0, 8, 2]],
      [3, [6, 8, 7]],
      [1, [2]],
      [8, [9, 6, 3, 2, 5, 7, 4, 8]],
      [2, [0, 4]],
    ] as const

    vectors.forEach(([expectedDegree, expectedIndices], seed) => {
      const random = new KotlinRandom(BigInt(seed))
      const degree = sampleSolitonDegree(10, random)
      expect(degree).toBe(expectedDegree)
      expect(selectKotlinIndices(10, degree, random)).toEqual(expectedIndices)
    })
  })
})

describe('ProfileContent encoding', () => {
  it('encodes a local profile and preserves the exact JSON text', () => {
    const config = '{\n  "emoji": "🧪",\n  "space":  true\n}\n'
    const encoded = encodeProfileContent({
      type: ProfileType.Local,
      name: '本地配置',
      config,
    })

    expect(Array.from(encoded.slice(0, 2))).toEqual([3, 1])
    const reader = new BinaryReader(gunzipSync(encoded.slice(2)))
    expect(reader.string()).toBe('本地配置')
    expect(reader.int32BE()).toBe(ProfileType.Local)
    expect(reader.string()).toBe(config)
    expect(reader.offset).toBe(reader.data.length)
  })

  it('writes iCloud and remote fields in current libbox order', () => {
    const iCloud = encodeProfileContent({
      type: ProfileType.ICloud,
      name: 'cloud',
      config: '{}',
      remotePath: 'folder/profile.json',
    })
    const iCloudReader = new BinaryReader(gunzipSync(iCloud.slice(2)))
    expect([iCloudReader.string(), iCloudReader.int32BE(), iCloudReader.string()]).toEqual([
      'cloud',
      ProfileType.ICloud,
      '{}',
    ])
    expect(iCloudReader.string()).toBe('folder/profile.json')
    expect(iCloudReader.offset).toBe(iCloudReader.data.length)

    const remote = encodeProfileContent({
      type: ProfileType.Remote,
      name: 'remote',
      config: '{ "raw": true }',
      remotePath: 'https://example.com/config.json',
      autoUpdate: true,
      autoUpdateInterval: 3600,
      lastUpdated: 1_786_523_617_123n,
    })
    const remoteReader = new BinaryReader(gunzipSync(remote.slice(2)))
    expect(remoteReader.string()).toBe('remote')
    expect(remoteReader.int32BE()).toBe(ProfileType.Remote)
    expect(remoteReader.string()).toBe('{ "raw": true }')
    expect(remoteReader.string()).toBe('https://example.com/config.json')
    expect(remoteReader.byte()).toBe(1)
    expect(remoteReader.int32BE()).toBe(3600)
    expect(remoteReader.int64BE()).toBe(1_786_523_617_123n)
    expect(remoteReader.offset).toBe(remoteReader.data.length)
  })
})

describe('QRS file metadata', () => {
  it('uses SFA escaping, ISO-8859-1 replacement, and byte lengths', () => {
    const bpf = Uint8Array.of(3, 1, 0xaa, 0xbb)
    const wrapped = appendQRSFileMeta(bpf, 'A"\\\n中😀')
    const view = new DataView(wrapped.buffer, wrapped.byteOffset, wrapped.byteLength)
    const metadataLength = view.getUint32(0, false)
    const metadata = String.fromCharCode(...wrapped.slice(4, 4 + metadataLength))
    const dataLengthOffset = 4 + metadataLength

    expect(metadata).toBe(
      '{"filename":"A\\"\\\\\\n??.bpf","contentType":"application/octet-stream"}',
    )
    expect(view.getUint32(dataLengthOffset, false)).toBe(bpf.length)
    expect(wrapped.slice(dataLengthOffset + 4)).toEqual(bpf)
  })
})

describe('QRS fountain frames', () => {
  it('uses the uncompressed wrapped size for default frame count', () => {
    const bundle = buildProfileQRS(
      {
        type: ProfileType.Local,
        name: 'frame-count',
        config: JSON.stringify({ value: 'compressible '.repeat(300) }),
      },
      { sliceSize: 100 },
    )
    const parsed = parseFrame(bundle.frames[0]!)

    expect(bundle.frameCount).toBe(calculateRequiredFrames(bundle.wrappedData.length, 100))
    expect(bundle.totalBlocks).toBe(Math.ceil(parsed.compressedSize / 100))
    expect(bundle.frameCount).toBeGreaterThan(bundle.totalBlocks)
  })

  it('honors explicit frame count and emits standard prefixed Base64 payloads', () => {
    const frames = encodeQRS(new TextEncoder().encode('fixed QRS vector'), {
      sliceSize: 8,
      frameCount: 3,
    })

    expect(frames).toHaveLength(3)
    // Generated independently with JVM Deflater and Kotlin 2.3.20 Random(seed: Long).
    expect(frames.map((frame) => frame.base64)).toEqual([
      'AgAAAAIAAAABAAAAAwAAABgAAADgQc9hwSMIVhiaSPQ=',
      'AQAAAAAAAAADAAAAGAAAAOBBz2F4nEvLrEhNUQ==',
      'AwAAAAEAAAAAAAAAAgAAAAMAAAAYAAAA4EHPYbm/Q5200gWl',
    ])
    for (const [index, frame] of frames.entries()) {
      expect(frame.frameIndex).toBe(index)
      expect(frame.content).toBe(QRS_URL_PREFIX + frame.base64)
      expect(frame.base64).toMatch(/^[A-Za-z0-9+/]+={0,2}$/)
      expect(frame.base64.length % 4).toBe(0)
    }
  })

  it('recovers zlib data from shuffled frames with duplicates', () => {
    const wrapped = new TextEncoder().encode(
      Array.from({ length: 200 }, (_, index) => `line-${index}:${index * 7919}`).join('\n'),
    )
    const initial = encodeQRS(wrapped, { sliceSize: 64, frameCount: 1 })
    const totalBlocks = initial[0]!.totalBlocks
    const frames = encodeQRS(wrapped, {
      sliceSize: 64,
      frameCount: totalBlocks * 5 + 20,
    })
    const shuffledWithDuplicates = [
      ...frames.filter((_, index) => index % 2 === 1).reverse(),
      frames[3]!,
      frames[3]!,
      ...frames.filter((_, index) => index % 2 === 0).reverse(),
    ]

    const recovered = recoverWrappedData(shuffledWithDuplicates)
    expect(recovered).toEqual(wrapped)
    expect(parseFrame(frames[0]!).checksum).toBe((crc32(wrapped) ^ totalBlocks) >>> 0)
  })
})
