// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createApp, defineComponent, h, nextTick, type App } from 'vue'

vi.mock('@/lang', () => ({ default: { global: { t: (text: string) => text } } }))
vi.mock('@/stores', () => ({ useAppStore: () => ({ currentVersion: 'test' }) }))
vi.mock('@/utils', async () => {
  const { sampleID, getValue } = await import('@/utils/others')
  return { APP_TITLE: 'Test', sampleID, getValue }
})
vi.mock('@/directives/menu', () => ({ default: {} }))
vi.mock('@/components/CodeViewer/index.vue', () => ({ default: { render: () => null } }))

import Confirm from '@/components/Confirm/index.vue'

describe('Confirm untrusted Markdown', () => {
  let app: App
  let host: HTMLDivElement

  beforeEach(() => {
    vi.useFakeTimers()
    host = document.createElement('div')
    document.body.append(host)
  })

  afterEach(() => {
    app.unmount()
    host.remove()
    vi.restoreAllMocks()
    vi.useRealTimers()
  })

  const mountMarkdown = async (message: string | Record<string, unknown>) => {
    app = createApp({
      render: () => h(Confirm, { title: 'Message', message, options: { type: 'markdown' } }),
    })
    app.component(
      'Button',
      defineComponent(
        (_, { slots }) =>
          () =>
            h('button', slots.default?.()),
      ),
    )
    ;(window as any).appInstance = app
    app.mount(host)
    await nextTick()
    await vi.runAllTimersAsync()
    await nextTick()
  }

  const expectSafeHTML = (root: Element) => {
    expect(
      root.querySelector('script, iframe, object, embed, form, input, style, base, svg, math'),
    ).toBeNull()
    for (const element of root.querySelectorAll('*')) {
      for (const attribute of element.attributes) {
        expect(attribute.name).not.toMatch(/^on/i)
        if (['href', 'src'].includes(attribute.name))
          expect(attribute.value).not.toMatch(/^\s*javascript:/i)
      }
    }
  }

  it('sanitizes raw HTML, Markdown links, and separately mounted table cells', async () => {
    await mountMarkdown(
      [
        '# Release notes',
        '',
        '<img src="bad" onerror="window.__xss=1"><iframe srcdoc="<script>alert(1)</script>"></iframe>',
        '<form action="https://evil.invalid"><input name="token"></form>',
        '<svg><a xlink:href="javascript:alert(1)">bad</a></svg>',
        '',
        '[unsafe](javascript:alert%281%29)',
        '',
        '| Change |',
        '| --- |',
        '| <img src="bad" onerror="window.__xss=2"> <a href="javascript:alert(1)">bad</a> **safe text** |',
      ].join('\n'),
    )

    const content = host.querySelector('.gui-confirm-content')!
    expectSafeHTML(content)
    const cell = content.querySelector('td')!
    expect(cell).not.toBeNull()
    expect(cell.querySelector('strong')?.textContent).toBe('safe text')
    expect(content.textContent).toContain('Release notes')
    expect((window as any).__xss).toBeUndefined()
  })

  it('opens safe links without an opener and blocks raw links from navigating the page', async () => {
    const open = vi.spyOn(window, 'open').mockReturnValue(null)
    await mountMarkdown(
      '<a href="https://example.com/release"><strong>release</strong></a> <a href="data:text/html,bad">blocked</a>',
    )
    const links = host.querySelectorAll('.gui-confirm-content a')
    const safeClick = new MouseEvent('click', { bubbles: true, cancelable: true })
    links[0]!.querySelector('strong')!.dispatchEvent(safeClick)
    expect(safeClick.defaultPrevented).toBe(true)
    expect(open).toHaveBeenCalledExactlyOnceWith(
      'https://example.com/release',
      '_blank',
      'noopener,noreferrer',
    )

    const blockedClick = new MouseEvent('click', { bubbles: true, cancelable: true })
    links[1]!.dispatchEvent(blockedClick)
    expect(blockedClick.defaultPrevented).toBe(true)
    expect(open).toHaveBeenCalledTimes(1)
  })

  it('also sanitizes object messages rendered in Markdown mode', async () => {
    await mountMarkdown({ note: '<img src="bad" onerror="window.__xss=3">' })
    const content = host.querySelector('.gui-confirm-content')!
    expectSafeHTML(content)
    expect(content.textContent).toContain('note')
  })
})
