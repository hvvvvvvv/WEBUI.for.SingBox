import { ColorOptions, ThemeOptions } from '@/constant/app'
import { ModeOptions } from '@/constant/kernel'
import useI18n from '@/lang'
import {
  useAppSettingsStore,
  useAppStore,
  useKernelApiStore,
  useRulesetsStore,
  useSubscribesStore,
} from '@/stores'
import { handleChangeMode, message } from '@/utils'

type Command = {
  label: string
  cmd: string
  desc?: string
  handler?: () => Promise<any> | any
  children?: Command[]
}

const processCommands = (commands: Command[], parentLabel = '', parentCmd = '') => {
  const { t } = useI18n.global

  const result: Command[] = []

  commands.forEach((item) => {
    const label = parentLabel ? `${t(parentLabel)}: ${t(item.label)}` : t(item.label)
    const cmd = parentCmd ? `${parentCmd}: ${item.cmd}` : item.cmd

    if (item.children) {
      result.push(...processCommands(item.children, label, cmd))
    } else {
      result.push({ label, cmd, handler: item.handler })
    }
  })

  return result
}

export const getCommands = () => {
  const kernelStore = useKernelApiStore()
  const appSettings = useAppSettingsStore()
  const appStore = useAppStore()
  const subscriptionsStore = useSubscribesStore()
  const rulesetsStore = useRulesetsStore()

  const rawCommands: Command[] = [
    {
      label: 'commands.kernel',
      cmd: 'Core',
      children: [
        {
          label: 'commands.startKernel',
          cmd: 'Start Core',
          handler: kernelStore.startCore,
        },
        {
          label: 'commands.stopKernel',
          cmd: 'Stop Core',
          handler: kernelStore.stopCore,
        },
        {
          label: 'commands.restartKernel',
          cmd: 'Restart Core',
          handler: kernelStore.restartCore,
        },
        {
          label: 'commands.enableTunMode',
          cmd: 'Enable Tun',
          handler: () => kernelStore.updateConfig('tun', { enable: true }),
        },
        {
          label: 'commands.disableTunMode',
          cmd: 'Disable Tun',
          handler: () => kernelStore.updateConfig('tun', { enable: false }),
        },
        {
          label: 'kernel.allow-lan',
          cmd: 'Allow Lan',
          handler: () => kernelStore.updateConfig('allow-lan', true),
        },
        {
          label: 'kernel.disallow-lan',
          cmd: 'Disallow Lan',
          handler: () => kernelStore.updateConfig('allow-lan', false),
        },
        {
          label: 'kernel.mode',
          cmd: 'Core Mode',
          children: ModeOptions.map((mode) => ({
            label: mode.label,
            cmd: mode.value,
            handler: () => handleChangeMode(mode.value),
          })),
        },
      ],
    },
    {
      label: 'APP',
      cmd: 'APP',
      children: [
        {
          label: 'settings.lang.name',
          cmd: 'Language',
          children: [
            ...appStore.locales.map((v) => ({
              label: v.label,
              cmd: v.value,
              handler: () => (appSettings.app.lang = v.value),
            })),
          ],
        },
        {
          label: 'settings.theme.name',
          cmd: 'Theme',
          children: ThemeOptions.map((theme) => ({
            label: theme.label,
            cmd: theme.value,
            handler: () => (appSettings.app.theme = theme.value),
          })),
        },
        {
          label: 'settings.color.name',
          cmd: 'Color',
          children: ColorOptions.map((color) => ({
            label: color.label,
            cmd: color.value,
            handler: () => (appSettings.app.color = color.value),
          })),
        },
        {
          label: 'router.about',
          cmd: 'About APP',
          handler: () => (appStore.showAbout = true),
        },
      ],
    },
    {
      label: 'router.subscriptions',
      cmd: 'Subscriptions',
      children: [
        {
          label: 'common.updateAll',
          cmd: 'Update Subscriptions',
          handler: subscriptionsStore.updateSubscribes,
        },
      ],
    },
    {
      label: 'router.rulesets',
      cmd: 'Rulesets',
      children: [
        {
          label: 'common.updateAll',
          cmd: 'Update Rulesets',
          handler: rulesetsStore.updateRulesets,
        },
      ],
    },
  ]

  return processCommands(rawCommands)
}
