export const deepClone = <T>(json: T): T => JSON.parse(JSON.stringify(json))

export const omitArray = <T, K extends keyof T>(arr: T[], fields: K[]): Omit<T, K>[] => {
  return arr.map((obj) => {
    const item: Partial<T> = deepClone(obj)
    fields.forEach((key) => {
      delete item[key]
    })
    return item as Omit<T, K>
  })
}
export const debounce = (fn: (...args: any) => any, wait: number) => {
  let timer: null | number = null
  const _debuonce = function (...args: any) {
    return new Promise((resolve, reject) => {
      timer && clearTimeout(timer)
      timer = setTimeout(async () => {
        try {
          resolve(await fn(...args))
        } catch (error) {
          reject(error)
        }
      }, wait)
    })
  }
  _debuonce.cancel = function () {
    timer && clearTimeout(timer)
    timer = null
  }
  return _debuonce
}

export const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms))

export const sampleID = () => 'ID_' + Math.random().toString(36).substring(2, 10)

export const getValue = <T = unknown>(obj: unknown, expr: string): T | undefined => {
  return expr.split('.').reduce<unknown>((value, key) => {
    if (value && typeof value === 'object') {
      return (value as Record<string, unknown>)[key]
    }
    return undefined
  }, obj) as T
}

const regexCache = new Map<string, RegExp>()

export const buildSmartRegExp = (pattern: string, flags = '') => {
  const key = pattern + '::' + flags
  if (regexCache.has(key)) return regexCache.get(key)!

  let r
  try {
    r = new RegExp(pattern, flags)
  } catch {
    const escaped = pattern.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
    r = new RegExp(escaped, flags)
  }

  regexCache.set(key, r)
  return r
}
