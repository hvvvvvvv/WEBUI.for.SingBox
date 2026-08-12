export const concatBytes = (...parts: Uint8Array[]): Uint8Array => {
  const result = new Uint8Array(parts.reduce((size, part) => size + part.length, 0))
  let offset = 0
  for (const part of parts) {
    result.set(part, offset)
    offset += part.length
  }
  return result
}

export const encodeUvarint = (value: number): Uint8Array => {
  if (!Number.isSafeInteger(value) || value < 0) {
    throw new RangeError('uvarint value must be a non-negative safe integer')
  }

  const bytes: number[] = []
  let remaining = BigInt(value)
  while (remaining >= 0x80n) {
    bytes.push(Number(remaining & 0x7fn) | 0x80)
    remaining >>= 7n
  }
  bytes.push(Number(remaining))
  return Uint8Array.from(bytes)
}

export const encodeString = (value: string): Uint8Array => {
  const bytes = new TextEncoder().encode(value)
  return concatBytes(encodeUvarint(bytes.length), bytes)
}

export const encodeInt32BE = (value: number): Uint8Array => {
  if (!Number.isInteger(value) || value < -0x80000000 || value > 0x7fffffff) {
    throw new RangeError('int32 value is out of range')
  }
  const result = new Uint8Array(4)
  new DataView(result.buffer).setInt32(0, value, false)
  return result
}

export const encodeInt64BE = (value: bigint): Uint8Array => {
  if (value < -(1n << 63n) || value > (1n << 63n) - 1n) {
    throw new RangeError('int64 value is out of range')
  }
  const result = new Uint8Array(8)
  new DataView(result.buffer).setBigInt64(0, value, false)
  return result
}

export const encodeUint32BE = (value: number): Uint8Array => {
  if (!Number.isInteger(value) || value < 0 || value > 0xffffffff) {
    throw new RangeError('uint32 value is out of range')
  }
  const result = new Uint8Array(4)
  new DataView(result.buffer).setUint32(0, value, false)
  return result
}

export const encodeLatin1 = (value: string): Uint8Array => {
  const bytes: number[] = []
  for (const character of value) {
    const codePoint = character.codePointAt(0)!
    bytes.push(codePoint <= 0xff ? codePoint : 0x3f)
  }
  return Uint8Array.from(bytes)
}

const BASE64_ALPHABET = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/'

export const encodeBase64 = (data: Uint8Array): string => {
  let result = ''
  for (let offset = 0; offset < data.length; offset += 3) {
    const first = data[offset]!
    const hasSecond = offset + 1 < data.length
    const hasThird = offset + 2 < data.length
    const second = hasSecond ? data[offset + 1]! : 0
    const third = hasThird ? data[offset + 2]! : 0
    const value = (first << 16) | (second << 8) | third

    result += BASE64_ALPHABET[(value >>> 18) & 0x3f]
    result += BASE64_ALPHABET[(value >>> 12) & 0x3f]
    result += hasSecond ? BASE64_ALPHABET[(value >>> 6) & 0x3f] : '='
    result += hasThird ? BASE64_ALPHABET[value & 0x3f] : '='
  }
  return result
}

const CRC32_TABLE = new Uint32Array(256)
for (let value = 0; value < CRC32_TABLE.length; value++) {
  let crc = value
  for (let bit = 0; bit < 8; bit++) {
    crc = (crc & 1) !== 0 ? 0xedb88320 ^ (crc >>> 1) : crc >>> 1
  }
  CRC32_TABLE[value] = crc >>> 0
}

export const crc32 = (data: Uint8Array): number => {
  let crc = 0xffffffff
  for (const value of data) {
    crc = CRC32_TABLE[(crc ^ value) & 0xff]! ^ (crc >>> 8)
  }
  return (crc ^ 0xffffffff) >>> 0
}
