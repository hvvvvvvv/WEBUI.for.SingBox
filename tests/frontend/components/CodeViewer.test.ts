// @vitest-environment jsdom
import { EditorSelection } from '@codemirror/state'
import { EditorView } from '@codemirror/view'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createApp, defineComponent, h, nextTick, ref, type App } from 'vue'

const formatter = vi.hoisted(() => vi.fn())
vi.mock('prettier/standalone', () => ({ formatWithCursor: formatter }))
vi.mock('@/stores', () => ({ useAppSettingsStore: () => ({ themeMode: 'light' }) }))
vi.mock('@/lang', async () => {
  const { ref } = await import('vue')
  return { default: { global: { t: (text: string) => text, locale: ref('en') } } }
})
vi.mock('@/utils', async () => {
  const { debounce } = await import('@/utils/others')
  return { debounce, message: { error: vi.fn() } }
})
vi.mock('@/components/Modal/index.vue', () => ({ IS_IN_MODAL: Symbol('modal') }))

import CodeViewer from '@/components/CodeViewer/index.vue'

describe('CodeViewer asynchronous formatting', () => {
  let app: App
  let host: HTMLDivElement
  let view: EditorView
  let component: { format: () => Promise<void>; getContent: () => string }

  beforeEach(() => {
    vi.useFakeTimers()
    formatter.mockReset()
    host = document.createElement('div')
    document.body.append(host)
  })

  afterEach(() => {
    app.unmount()
    host.remove()
    vi.useRealTimers()
  })

  const mountEditor = async (editable = true) => {
    const editor = ref()
    app = createApp({
      render: () =>
        h(CodeViewer, { ref: editor, editable, lang: 'javascript', modelValue: 'let a=1' }),
    })
    app.component(
      'Button',
      defineComponent(() => () => null),
    )
    app.mount(host)
    await nextTick()
    await vi.advanceTimersByTimeAsync(0)
    component = editor.value
    view = EditorView.findFromDOM(host.querySelector('.cm-editor')!)!
    expect(view).toBeDefined()
  }

  it.each(['document', 'selection'])(
    'does not apply an old format result after the %s changes',
    async (change) => {
      await mountEditor()
      const pending = Promise.withResolvers<{ formatted: string; cursorOffset: number }>()
      formatter.mockReturnValue(pending.promise)
      const formatting = component.format()
      await Promise.resolve()
      expect(formatter).toHaveBeenCalledTimes(1)

      if (change === 'document') view.dispatch({ changes: { from: 6, to: 7, insert: '2' } })
      else view.dispatch({ selection: { anchor: 4 } })
      const expectedContent = component.getContent()
      const expectedSelection = view.state.selection
      pending.resolve({ formatted: 'let a = 1\n', cursorOffset: 0 })
      await formatting

      expect(component.getContent()).toBe(expectedContent)
      expect(view.state.selection.eq(expectedSelection)).toBe(true)
    },
  )

  it('preserves multiple selection ranges, their direction, and the main cursor', async () => {
    await mountEditor()
    view.dispatch({
      selection: EditorSelection.create(
        [EditorSelection.range(3, 0), EditorSelection.range(4, 7)],
        1,
      ),
    })
    formatter.mockImplementation(async (content, { cursorOffset }) => ({
      formatted: ' ' + content,
      cursorOffset: cursorOffset + 1,
    }))
    await component.format()

    expect(component.getContent()).toBe(' let a=1')
    expect(view.state.selection.mainIndex).toBe(1)
    expect(view.state.selection.ranges.map(({ anchor, head }) => ({ anchor, head }))).toEqual([
      { anchor: 4, head: 1 },
      { anchor: 5, head: 8 },
    ])
  })

  it('keeps a read-only viewer unchanged when format is requested', async () => {
    await mountEditor(false)
    await component.format()
    expect(formatter).not.toHaveBeenCalled()
    expect(component.getContent()).toBe('let a=1')
  })
})
