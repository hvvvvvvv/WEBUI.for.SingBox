/** Kotlin stdlib XorWowRandom, used by kotlin.random.Random(seed: Long). */
export class KotlinRandom {
  private x: number
  private y: number
  private z = 0
  private w = 0
  private v: number
  private addend: number

  constructor(seed: bigint) {
    const seed1 = Number(BigInt.asIntN(32, seed))
    const seed2 = Number(BigInt.asIntN(32, seed >> 32n))
    this.x = seed1
    this.y = seed2
    this.v = ~seed1
    this.addend = (seed1 << 10) ^ (seed2 >>> 4)

    if ((this.x | this.y | this.z | this.w | this.v) === 0) {
      throw new Error('Initial XorWow state must contain a non-zero value')
    }
    for (let index = 0; index < 64; index++) this.nextInt()
  }

  nextInt(): number {
    let t = this.x
    t ^= t >>> 2
    this.x = this.y
    this.y = this.z
    this.z = this.w
    const previousV = this.v
    this.w = previousV
    t = t ^ (t << 1) ^ previousV ^ (previousV << 4)
    this.v = t | 0
    this.addend = (this.addend + 362437) | 0
    return (t + this.addend) | 0
  }

  nextBits(bitCount: number): number {
    return (this.nextInt() >>> (32 - bitCount)) & (-bitCount >> 31)
  }

  nextDouble(): number {
    const high = this.nextBits(26)
    const low = this.nextBits(27)
    return (high * 134217728 + low) / 9007199254740992
  }

  nextIntBelow(until: number): number {
    if (!Number.isInteger(until) || until <= 0 || until > 0x7fffffff) {
      throw new RangeError('Random bound must be a positive int32')
    }

    if ((until & -until) === until) {
      return this.nextBits(31 - Math.clz32(until))
    }

    while (true) {
      const bits = this.nextInt() >>> 1
      const value = bits % until
      if (((bits - value + (until - 1)) | 0) >= 0) return value
    }
  }
}

export const sampleSolitonDegree = (totalBlocks: number, random: KotlinRandom): number => {
  if (totalBlocks <= 0) return 1

  const probability = random.nextDouble()
  let cumulative = 1 / totalBlocks
  if (probability < cumulative) return 1

  for (let degree = 2; degree <= totalBlocks; degree++) {
    cumulative += 1 / Math.imul(degree, degree - 1)
    if (probability < cumulative) return degree
  }
  return totalBlocks
}

export const selectKotlinIndices = (
  totalBlocks: number,
  degree: number,
  random: KotlinRandom,
): number[] => {
  const indices = Array.from({ length: totalBlocks }, (_, index) => index)
  for (let index = indices.length - 1; index > 0; index--) {
    const swapIndex = random.nextIntBelow(index + 1)
    const value = indices[index]!
    indices[index] = indices[swapIndex]!
    indices[swapIndex] = value
  }
  return indices.slice(0, Math.min(degree, totalBlocks))
}
