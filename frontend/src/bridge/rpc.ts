import { createClient, type Client } from '@connectrpc/connect'
import { createConnectTransport } from '@connectrpc/connect-web'
import type { DescService } from '@bufbuild/protobuf'

import { useAppSettingsStore } from '@/stores'

import { loadAuthToken } from './http'

let transport: ReturnType<typeof createConnectTransport> | undefined

const getTransport = () => {
  if (transport) return transport

  transport = createConnectTransport({
    baseUrl: '/api/rpc',
    fetch: async (input, init) => {
      const appSettings = useAppSettingsStore()

      if (appSettings.sessionInfo.cacheToken === '' && !(await loadAuthToken())) {
        location.reload()
      }

      const headers = new Headers(init?.headers)
      headers.set('Authorization', `Bearer ${appSettings.sessionInfo.cacheToken}`)

      let resp = await fetch(input, { ...init, headers })
      if (resp.status === 401) {
        appSettings.sessionInfo.cacheToken = ''
        if (!(await loadAuthToken())) {
          location.reload()
        }

        headers.set('Authorization', `Bearer ${appSettings.sessionInfo.cacheToken}`)
        resp = await fetch(input, { ...init, headers })
      }

      if (resp.status === 401) {
        location.reload()
      }

      return resp
    },
  })

  return transport
}

export const createRpcClient = <T extends DescService>(service: T): Client<T> => {
  return createClient(service, getTransport())
}
