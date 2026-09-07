export const parseCoreLogDays = (input: number | string): number | undefined => {
  if (typeof input === 'string' && !/^\d+$/.test(input.trim())) return undefined
  const value = Number(input)
  return Number.isInteger(value) && value >= 0 && value <= 2147483647 ? value : undefined
}
