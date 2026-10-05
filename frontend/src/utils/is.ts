import { Cron } from 'croner'

export const isValidJson = (str: string) => {
  try {
    return !!JSON.parse(str)
  } catch {
    return false
  }
}

export const isValidCron = (pattern: string) => {
  try {
    new Cron(pattern, { paused: true })
    return { ok: true, reason: null }
  } catch (error: any) {
    return { ok: false, reason: error.message || error }
  }
}
