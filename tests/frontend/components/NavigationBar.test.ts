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
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
  })
  it.each([390, 1280])(
    'reveals the current navigation entry only on mobile (%ipx)',
    async (width) => {
      vi.stubGlobal('innerWidth', width)
      const scrollPage = vi.spyOn(window, 'scrollTo').mockImplementation(() => undefined)
      vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(function () {
        if (this.matches('.app-navigation')) return new DOMRect(20, 90, 200, 44)
        if (this.tagName === 'BUTTON') {
          const offsets: Record<string, number> = { Overview: 0, Plugins: 250, Settings: 520 }
          const offset = offsets[this.textContent || ''] ?? 0
          const scroll = this.closest('nav')?.scrollLeft ?? 0
          return new DOMRect(20 + offset - scroll, 90, 100, 44)
        }
        return new DOMRect()
      })
      preferences.app = reactive({ pages: ['Overview', 'Plugins'] })
      const router = createRouter({ history: createMemoryHistory(), routes })
      await router.push('/settings')
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
      const navigation = host.querySelector('nav')!
      expect(navigation.scrollLeft).toBe(width <= 700 ? 420 : 0)
      navigation.scrollTop = 17
      await router.push('/')
      await nextTick()
      expect(navigation.scrollLeft).toBe(0)
      await router.push('/plugins')
      await nextTick()
      expect(navigation.scrollLeft).toBe(width <= 700 ? 150 : 0)
      expect(navigation.scrollTop).toBe(17)
      expect(scrollPage).not.toHaveBeenCalled()
    },
  )
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
