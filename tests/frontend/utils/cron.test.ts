import { describe, expect, it } from 'vitest'

import { isValidCron } from '@/utils/is'

describe('scheduler syntax shared with the backend', () => {
  it.each(['0 8 * * *', '0 0 8 * * MON', '0 0 8 * * 7', '0 0 8 * * 5-7', '0 0 0 29 FEB *', '* * * * * *'])(
    'accepts %s', (expression) => expect(isValidCron(expression).ok).toBe(true),
  )
  it.each(['', '0 0 * *', '0 0 0 L * *', '0 0 * * MON#2', '0 0 * * 17', '0 0 * * 7/0', '@daily'])(
    'rejects %s before saving', (expression) => expect(isValidCron(expression).ok).toBe(false),
  )
})
