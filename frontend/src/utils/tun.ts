import { Inbound } from '@/enums/kernel'

const MaxIPRoute2Index = 0xffffffff

export const validateTunInbounds = (inbounds: IInbound[], platformOS: string): string => {
  const enabledTunInbounds = inbounds.filter(
    (inbound) => inbound.enable && inbound.type === Inbound.Tun && inbound.tun,
  )

  for (const inbound of enabledTunInbounds) {
    const mode: unknown = inbound.tun!.dns_mode
    if (
      mode !== undefined &&
      mode !== '' &&
      (typeof mode !== 'string' || !['disabled', 'native', 'hijack'].includes(mode))
    ) {
      return 'kernel.inbounds.tun.dns_mode_invalid'
    }
  }

  if (platformOS !== 'linux') return ''
  const autoRouteInbounds = enabledTunInbounds.filter((inbound) => inbound.tun!.auto_route)

  for (const inbound of autoRouteInbounds) {
    const tableIndex = inbound.tun!.iproute2_table_index
    if (
      tableIndex !== undefined &&
      (!Number.isInteger(tableIndex) || tableIndex < 1 || tableIndex > MaxIPRoute2Index)
    ) {
      return 'kernel.inbounds.tun.iproute2_table_index_invalid'
    }

    const ruleIndex = inbound.tun!.iproute2_rule_index
    if (
      ruleIndex !== undefined &&
      (!Number.isInteger(ruleIndex) || ruleIndex < 0 || ruleIndex > MaxIPRoute2Index)
    ) {
      return 'kernel.inbounds.tun.iproute2_rule_index_invalid'
    }
  }

  if (autoRouteInbounds.filter((inbound) => inbound.tun!.auto_redirect).length > 1) {
    return 'kernel.inbounds.tun.auto_redirect_conflict'
  }
  return ''
}
