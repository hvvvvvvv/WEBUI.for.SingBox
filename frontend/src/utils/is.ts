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
    const fields = pattern.trim().split(/\s+/)
    if (fields.length !== 5 && fields.length !== 6) {
      throw new Error('Cron must have 5 or 6 fields (seconds are optional)')
    }
    const numeric = fields.map((field, index) => {
      const names = index === fields.length - 1
        ? ['SUN', 'MON', 'TUE', 'WED', 'THU', 'FRI', 'SAT']
        : index === fields.length - 2
          ? ['JAN', 'FEB', 'MAR', 'APR', 'MAY', 'JUN', 'JUL', 'AUG', 'SEP', 'OCT', 'NOV', 'DEC']
          : []
      return field.replace(/[a-z]+/gi, (name) => {
        const value = names.indexOf(name.toUpperCase())
        if (value < 0) throw new Error('Unsupported cron extension: ' + name)
        return String(value + (index === fields.length - 2 ? 1 : 0))
      })
    })
    if (numeric.some((field) => !/^(?:\d+(?:-\d+)?|[?*])(?:\/\d+)?(?:,(?:\d+(?:-\d+)?|[?*])(?:\/\d+)?)*$/.test(field))) {
      throw new Error('Cron supports numbers, names, *, ?, ranges, lists and steps')
    }
    new Cron(pattern, { paused: true })
    return { ok: true, reason: null }
  } catch (error: any) {
    return { ok: false, reason: error.message || error }
  }
}
