import { createRpcClient } from '@/bridge'
import { KernelConfigService } from '../../gen/kernel/v1/kernel_config_service_pb'
import { ProfileService } from '../../gen/profile/v1/profile_service_pb'
import { DnsServer } from '@/enums/kernel'
import { iProfileToProto } from './profileRpc'

export const generateDnsServerURL = (dnsServer: IDNSServer) => {
  const { type, server_port, path, server, interface: _interface } = dnsServer
  let address = ''
  if (type == DnsServer.Https) {
    address = `https://${server}${server_port ? ':' + server_port : ''}${path ? path : ''}`
  } else if (type == DnsServer.H3) {
    address = `h3://${server}${server_port ? ':' + server_port : ''}${path ? path : ''}`
  } else if (type == DnsServer.Dhcp) {
    address = `dhcp://${_interface}`
  } else if (type == DnsServer.FakeIP) {
    address =
      'fake-ip://' +
      (dnsServer.inet4_range ? dnsServer.inet4_range : '') +
      (dnsServer.inet6_range ? (dnsServer.inet4_range ? ',' : '') + dnsServer.inet6_range : '')
  } else if (type === DnsServer.Hosts) {
    address = 'hosts'
  } else if (type === DnsServer.Local) {
    address = 'local'
  } else {
    address = `${type}://${server}${server_port ? ':' + server_port : ''}`
  }
  return address
}

export const generateConfigViaRpcByProfile = async (profile: IProfile): Promise<Recordable> => {
  const profileServiceClient = createRpcClient(KernelConfigService)

  const result = await profileServiceClient.generateConfig({
    profile: iProfileToProto(profile),
    options: {
      enableMixinProcessing: true,
      enableScriptProcessing: true,
    },
  })

  if (!result.config) throw new Error('Empty config response from server')
  return result.config as Recordable
}

export const generateConfigViaRpc = async (profileId: string): Promise<Recordable> => {
  const profileMgmtClient = createRpcClient(ProfileService)
  const { profile } = await profileMgmtClient.getProfile({ id: profileId })
  if (!profile) throw new Error(`Profile "${profileId}" not found`)
  return generateConfigViaRpcByProfile(profile as any)
}
