// @vitest-environment jsdom
import { createApp, h, nextTick, reactive, type App } from 'vue'
import { createMemoryHistory, createRouter } from 'vue-router'
import { afterEach, describe, expect, it, vi } from 'vitest'

const preferences = vi.hoisted(() => ({ app: null as any }))
vi.mock('@/stores/appSettings', () => ({ useAppSettingsStore: () => preferences }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (text: string) => text }) }))
vi.mock('@/router/routes', () => ({
  default: [
    { path: '/', name: 'Overview', meta: { name: 'Overview' }, component: { render: () => null } },
    {
      path: '/plugins',
      name: 'Plugins',
      meta: { name: 'Plugins' },
      component: { render: () => null },
    },
    {
      path: '/settings',
      name: 'Settings',
      meta: { name: 'Settings' },
      component: { render: () => null },
    },
  ],
}))
import routes from '@/router/routes'
import NavigationBar from '@/components/_common/NavigationBar.vue'

describe('page visibility navigation', () => {
  let app: App
  let host: HTMLDivElement
  afterEach(() => {
    app.unmount()
    host.remove()
  })
  it('keeps Settings reachable and leaves a hidden current route even with no selected pages', async () => {
    preferences.app = reactive({ pages: ['Overview', 'Plugins'] })
    const router = createRouter({ history: createMemoryHistory(), routes })
    await router.push('/plugins')
    host = document.createElement('div')
    document.body.append(host)
    app = createApp(NavigationBar)
    app.component('Button', {
      setup:
        (_, { slots }) =>
        () =>
          h('button', slots.default?.()),
    })
    app.use(router)
    app.mount(host)
    await nextTick()
    expect(host.textContent).toContain('Plugins')
    preferences.app.pages = []
    await vi.waitFor(() => expect(router.currentRoute.value.name).toBe('Settings'))
    expect(host.querySelectorAll('button')).toHaveLength(1)
    expect(host.textContent).toBe('Settings')
    await router.push('/plugins')
    await vi.waitFor(() => expect(router.currentRoute.value.name).toBe('Settings'))
    preferences.app.pages = ['Plugins']
    await nextTick()
    host.querySelector('button')!.click()
    await vi.waitFor(() => expect(router.currentRoute.value.name).toBe('Plugins'))
    expect(host.textContent).toContain('Settings')
  })
})
