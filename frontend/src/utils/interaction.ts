import { render, h, type VNode } from 'vue'

import i18n from '@/lang'
import { APP_TITLE, sampleID } from '@/utils'

import ConfirmComp from '@/components/Confirm/index.vue'
import MessageComp from '@/components/Message/index.vue'
import PickerComp from '@/components/Picker/index.vue'

import type { ConfirmOptions } from '@/components/Confirm/index.vue'
import type { MessageIcon } from '@/components/Message/index.vue'
import type { PickerItem } from '@/components/Picker/index.vue'

const FloatingContainerCssText = `
    position: fixed;
    z-index: 99999;
    top: 84px;
    left: 0;
    right: 0;
    display: flex;
    justify-content: center;
    max-height: 70%;
`

const ConfirmContainerCssText = `
    position: fixed;
    z-index: 99999;
    inset: 0;
    display: flex;
    align-items: center;
    justify-content: center;
    box-sizing: border-box;
    padding: 24px;
    background: var(--modal-mask-bg);
    backdrop-filter: blur(4px);
    -webkit-backdrop-filter: blur(4px);
`

interface MessageInstance {
  dom: HTMLDivElement
  vnode: VNode
  timer: number
}

const bindAppContext = (vnode: VNode) => {
  vnode.appContext = window.appInstance._context
}

class Message {
  public container: HTMLElement
  public instances: Record<string, MessageInstance>

  constructor() {
    const ID = APP_TITLE + '-toast'
    this.container = document.getElementById(ID) || document.createElement('div')
    this.container.id = ID
    this.container.style.cssText = `
        position: fixed;
        z-index: 999999;
        top: 80px;
        left: 50%;
        transform: translateX(-50%);
    `
    document.body.appendChild(this.container)
    this.instances = {}
  }

  private buildMessage = (icon: MessageIcon) => {
    return (content: string, duration = 3_000, onClose?: () => void) => {
      const id = sampleID()
      const dom = document.createElement('div')

      const onMouseEnter = () => clearTimeout(this.instances[id]!.timer)
      const onMouseLeave = () => (this.instances[id]!.timer = setTimeout(onDestroy, duration))

      const onDestroy = () => {
        dom.removeEventListener('mouseenter', onMouseEnter)
        dom.removeEventListener('mouseleave', onMouseLeave)
        this.destroy(id)
      }

      const initInstance = () => {
        dom.style.cssText = 'display: flex; align-items: center; justify-content: center;'

        const vnode = h(MessageComp, {
          icon,
          content,
          onClose: () => {
            onClose?.()
            onDestroy()
          },
        })
        bindAppContext(vnode)

        this.instances[id] = {
          dom,
          vnode,
          timer: setTimeout(onDestroy, duration),
        }

        dom.addEventListener('mouseenter', onMouseEnter)
        dom.addEventListener('mouseleave', onMouseLeave)

        this.container.appendChild(dom)
        render(vnode, dom)
      }

      initInstance()

      return {
        error: (content: string) => this.update(id, content, 'error'),
        success: (content: string) => this.update(id, content, 'success'),
        update: (content: string) => this.update(id, content),
        destroy: onDestroy,
      }
    }
  }

  public info = this.buildMessage('info')
  public warn = this.buildMessage('warn')
  public error = this.buildMessage('error')
  public success = this.buildMessage('success')

  public update = (id: string, content: string, icon?: MessageIcon) => {
    const instance = this.instances[id]
    if (instance) {
      icon && (instance.vnode.component!.props.icon = icon)
      content && (instance.vnode.component!.props.content = content)
    }
  }

  public destroy = (id: string) => {
    const instance = this.instances[id]
    if (instance) {
      render(null, instance.dom)
      instance.dom.remove()
      clearTimeout(instance.timer)
      delete this.instances[id]
    }
  }
}

class Picker {
  public multi = <ValueType>(
    title: string,
    options: PickerItem<ValueType>[],
  ): Promise<ValueType[]> => {
    return new Promise((resolve, reject) => {
      const { t } = i18n.global
      const dom = document.createElement('div')
      dom.style.cssText = FloatingContainerCssText
      const vnode = h(PickerComp<ValueType, 'multi'>, {
        type: 'multi',
        title,
        options,
        onConfirm: resolve,
        onCancel: () => reject(t('common.canceled')),
        onFinish: () => {
          render(null, dom)
          dom.remove()
        },
      })
      bindAppContext(vnode)
      document.body.appendChild(dom)
      render(vnode, dom)
    })
  }
}

const buildConfirm = (
  title: string,
  message: string,
  options: ConfirmOptions = { type: 'text' },
  cancel = true,
) => {
  return new Promise((resolve, reject) => {
    const { t } = i18n.global
    const dom = document.createElement('div')
    dom.style.cssText = ConfirmContainerCssText
    const vnode = h(ConfirmComp, {
      title,
      message,
      options,
      cancel,
      onConfirm: resolve,
      onCancel: () => reject(t('common.canceled')),
      onFinish: () => {
        render(null, dom)
        dom.remove()
      },
    })
    bindAppContext(vnode)
    document.body.appendChild(dom)
    render(vnode, dom)
  })
}

export const alert = (title: string, message: string) => {
  return buildConfirm(title, message, { type: 'text' }, false)
}

export const confirm = (
  title: string,
  message: string,
  options: ConfirmOptions = { type: 'text' },
) => {
  return buildConfirm(title, message, options)
}

export const confirmDelete = async () => {
  try {
    await confirm('common.warning', 'common.deleteConfirm', {
      type: 'text',
      okText: 'common.delete',
    })
    return true
  } catch {
    return false
  }
}

export const picker = new Picker()

export const message = new Message()
