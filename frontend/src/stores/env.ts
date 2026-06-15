import { defineStore } from 'pinia'
import { ref } from 'vue'

import { GetEnv } from '@/bridge'
import { useKernelApiStore } from '@/stores'
import { SetSystemProxy, GetSystemProxy } from '@/utils'
import { OS } from '@/enums/app'
import type { AppEnv } from '@/types/app'

export const useEnvStore = defineStore('env', () => {
  const kernelApiStore = useKernelApiStore()

  const env = ref<AppEnv>({
    appName: '',
    appVersion: '',
    basePath: '',
    appPath: '',
    os: '' as OS,
    arch: '',
    libc: '',
    isPrivileged: false,
  })

  const systemProxy = ref(false)

  const setupEnv = async () => {
    const _env = await GetEnv()
    let appPath = `${_env.basePath}/${_env.appName}`
    if (_env.os === OS.Windows) {
      appPath = appPath.replaceAll('/', '\\')
    } else if (_env.os === OS.Darwin) {
      appPath = appPath.replace(`/Contents/MacOS/${_env.appName}`, '')
    }
    env.value = { ..._env, appPath }
  }

  const updateSystemProxyStatus = async () => {
    const kernelApiStore = useKernelApiStore()
    const proxyServer = await GetSystemProxy()

    if (!proxyServer) {
      systemProxy.value = false
    } else {
      const { port, 'mixed-port': mixedPort, 'socks-port': socksPort } = kernelApiStore.config
      const proxyServerList = [
        `http://127.0.0.1:${port}`,
        `http://127.0.0.1:${mixedPort}`,

        `socks5://127.0.0.1:${mixedPort}`,
        `socks5://127.0.0.1:${socksPort}`,

        `socks=127.0.0.1:${mixedPort}`,
        `socks=127.0.0.1:${socksPort}`,
      ]
      systemProxy.value = proxyServerList.includes(proxyServer)
    }

    return systemProxy.value
  }

  const setSystemProxy = async () => {
    let proxyPort = kernelApiStore.getProxyPort()

    if (!proxyPort) {
      await kernelApiStore.updateConfig('inbound', undefined)
    }

    proxyPort = kernelApiStore.getProxyPort()

    if (!proxyPort) throw 'home.overview.needPort'

    await SetSystemProxy(true, '127.0.0.1:' + proxyPort.port, proxyPort.proxyType)

    systemProxy.value = true
  }

  const clearSystemProxy = async () => {
    await SetSystemProxy(false, '')
    systemProxy.value = false
  }

  const switchSystemProxy = async (enable: boolean) => {
    if (enable) await setSystemProxy()
    else await clearSystemProxy()
  }

  return {
    env,
    setupEnv,
    systemProxy,
    setSystemProxy,
    clearSystemProxy,
    switchSystemProxy,
    updateSystemProxyStatus,
  }
})
