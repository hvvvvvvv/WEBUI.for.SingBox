import { describe, expect, it } from 'vitest'
import { parseCoreLogDays } from '../coreLogDays'

describe('core log retention input', () => {
  it.each([0, 1, 7, 2147483647, '0', '7', ' 14 '])('accepts %s', (value) => {
    expect(parseCoreLogDays(value)).toBe(Number(value))
  })
  it.each([-1, 1.5, 2147483648, NaN, Infinity, '', ' ', '-1', '1.5', 'invalid'])('rejects %s', (value) => {
    expect(parseCoreLogDays(value)).toBeUndefined()
  })
})
