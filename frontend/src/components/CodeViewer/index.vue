<script setup lang="ts">
import { indentWithTab } from '@codemirror/commands'
import { javascript } from '@codemirror/lang-javascript'
import { json, jsonParseLinter } from '@codemirror/lang-json'
import { yaml } from '@codemirror/lang-yaml'
import { linter } from '@codemirror/lint'
import { MergeView } from '@codemirror/merge'
import {
  closeSearchPanel,
  getSearchQuery,
  openSearchPanel,
  searchPanelOpen,
  setSearchQuery,
} from '@codemirror/search'
import { Compartment, EditorState } from '@codemirror/state'
import { oneDark } from '@codemirror/theme-one-dark'
import { keymap, placeholder as Placeholder } from '@codemirror/view'
import { EditorView, basicSetup } from 'codemirror'
import * as parserBabel from 'prettier/parser-babel'
import * as parserYaml from 'prettier/parser-yaml'
import estreePlugin from 'prettier/plugins/estree'
import * as prettier from 'prettier/standalone'
import { watch, onUnmounted, onMounted, useTemplateRef, inject } from 'vue'

import { Theme } from '@/enums/app'
import i18n from '@/lang'
import { useAppSettingsStore } from '@/stores'
import { debounce, message } from '@/utils'

import { IS_IN_MODAL } from '@/components/Modal/index.vue'

interface Props {
  modelValue?: string
  editable?: boolean
  lang?: 'json' | 'javascript' | 'yaml'
  mode?: 'editor' | 'diff'
  placeholder?: string
}

const emit = defineEmits(['change', 'update:modelValue'])
const props = withDefaults(defineProps<Props>(), {
  modelValue: '',
  editable: false,
  lang: 'json',
  mode: 'editor',
  placeholder: '',
})

const { promise: editorReady, resolve: markEditorReady } = Promise.withResolvers()
let internalUpdate = true

watch(
  () => props.modelValue,
  async (val) => {
    await editorReady
    const view = editorView || mergeView?.b
    if (view && val != view.state.doc.toString()) {
      internalUpdate = false
      view.dispatch({
        changes: {
          from: 0,
          to: view.state.doc.length,
          insert: val,
        },
      })
    }
  },
)

let editorView: EditorView
let mergeView: MergeView
const themeCompartment = new Compartment()
const searchLocaleCompartment = new Compartment()
const domRef = useTemplateRef('domRef')
const appSettings = useAppSettingsStore()

const getEditorViews = () =>
  editorView ? [editorView] : mergeView ? [mergeView.a, mergeView.b] : []

const readOnlyCursorTheme = EditorView.theme({
  '&.cm-focused > .cm-scroller > .cm-cursorLayer .cm-cursor': {
    display: 'none !important',
  },
  '.cm-dropCursor': {
    display: 'none !important',
  },
})

const getEditorModeExtensions = (readOnly: boolean) => [
  EditorState.readOnly.of(readOnly),
  EditorView.editable.of(!readOnly),
  ...(readOnly
    ? [EditorView.contentAttributes.of({ tabindex: '0' }), readOnlyCursorTheme]
    : []),
]

const getSearchPhrases = (): Record<string, string> => {
  const { t } = i18n.global
  return {
    Find: t('codeViewer.search.find'),
    Replace: t('codeViewer.search.replace'),
    next: t('codeViewer.search.next'),
    previous: t('codeViewer.search.previous'),
    all: t('codeViewer.search.all'),
    'match case': t('codeViewer.search.matchCase'),
    regexp: t('codeViewer.search.regexp'),
    'by word': t('codeViewer.search.byWord'),
    replace: t('codeViewer.search.replace'),
    'replace all': t('codeViewer.search.replaceAll'),
    close: t('codeViewer.search.close'),
    'current match': t('codeViewer.search.currentMatch'),
    'on line': t('codeViewer.search.onLine'),
    'replaced match on line $': t('codeViewer.search.replacedMatchOnLine'),
    'replaced $ matches': t('codeViewer.search.replacedMatches'),
    'Go to line': t('codeViewer.search.goToLine'),
    go: t('codeViewer.search.go'),
  }
}

const onChange = debounce((content: string) => {
  if (internalUpdate) {
    emit('update:modelValue', content)
    emit('change', content)
  }
  internalUpdate = true
}, 300)

const formatDoc = async (view: EditorView) => {
  if (view.state.readOnly) return
  const content = view.state.doc.toString()
  const cursor = view.state.selection.ranges[0]?.from || 0
  try {
    const parser = { javascript: 'babel', yaml: 'yaml', json: 'json' }[props.lang]
    const plugins = {
      javascript: [parserBabel, estreePlugin],
      yaml: [parserYaml],
      json: [parserBabel, estreePlugin],
    }[props.lang]
    const { formatted, cursorOffset } = await prettier.formatWithCursor(content, {
      cursorOffset: cursor,
      parser,
      plugins,
      semi: false,
      tabWidth: 2,
      singleQuote: true,
      printWidth: 160,
      trailingComma: 'none',
    })
    if (content !== formatted) {
      view.dispatch({
        changes: { from: 0, to: content.length, insert: formatted },
        selection: { anchor: cursorOffset, head: cursorOffset },
      })
    }
  } catch (error: any) {
    message.error(error.message || error)
  }
}

watch(
  () => appSettings.themeMode,
  (theme) => {
    getEditorViews().forEach((view) => {
      view.dispatch({
        effects: themeCompartment.reconfigure(
          theme === Theme.Dark ? [EditorView.theme({}, { dark: true }), oneDark] : [],
        ),
      })
    })
  },
)

watch(
  () => i18n.global.locale.value,
  () => {
    getEditorViews().forEach((view) => {
      const panelOpen = searchPanelOpen(view.state)
      const query = panelOpen ? getSearchQuery(view.state) : null
      if (panelOpen) closeSearchPanel(view)
      view.dispatch({
        effects: searchLocaleCompartment.reconfigure(
          EditorState.phrases.of(getSearchPhrases()),
        ),
      })
      if (panelOpen && query) {
        openSearchPanel(view)
        view.dispatch({ effects: setSearchQuery.of(query) })
      }
    })
  },
)

let timer: number
onMounted(() => (timer = setTimeout(() => initEditor(), inject(IS_IN_MODAL, false) ? 100 : 0)))
onUnmounted(() => {
  clearTimeout(timer)
  const view = editorView || mergeView
  view?.destroy()
})

const initEditor = () => {
  domRef.value!.innerHTML = ''

  const extensions = [
    basicSetup,
    // keymap
    ...(props.editable
      ? [
          keymap.of([
            indentWithTab,
            {
              key: 'Shift-Alt-f',
              run: function (v: EditorView) {
                if (v.state.readOnly) return false
                formatDoc(v)
                return true
              },
            },
          ]),
        ]
      : []),
    // code wrap
    EditorView.lineWrapping,
    // placeholder
    Placeholder(props.placeholder),
    // theme
    themeCompartment.of(
      appSettings.themeMode === Theme.Dark ? [EditorView.theme({}, { dark: true }), oneDark] : [],
    ),
    // search locale
    searchLocaleCompartment.of(EditorState.phrases.of(getSearchPhrases())),
    // lint
    ...(props.lang === 'json' ? [linter(jsonParseLinter())] : []),
    // lang
    ...(['javascript', 'json', 'yaml'].includes(props.lang)
      ? [{ javascript, json, yaml }[props.lang]()]
      : []),
    EditorView.updateListener.of((update) => {
      if (update.docChanged && !update.state.readOnly) {
        onChange(update.state.doc.toString())
      }
    }),
  ]

  if (props.mode === 'editor') {
    editorView = new EditorView({
      doc: props.modelValue,
      parent: domRef.value!,
      extensions: [...extensions, ...getEditorModeExtensions(!props.editable)],
    })
  } else {
    mergeView = new MergeView({
      parent: domRef.value!,
      a: {
        doc: props.modelValue,
        extensions: [...extensions, ...getEditorModeExtensions(true)],
      },
      b: {
        doc: props.modelValue,
        extensions: [...extensions, ...getEditorModeExtensions(!props.editable)],
      },
    })
  }

  markEditorReady(null)
}
</script>

<template>
  <div ref="domRef" @keydown.esc.stop @keydown.esc.prevent>
    <div class="flex justify-center">
      <Button loading type="link" />
    </div>
  </div>
</template>

<style lang="less" scoped>
:deep(.cm-editor) {
  height: 100%;
}
:deep(.cm-scroller) {
  font-family: monaco, Consolas, Menlo, Courier, monospace;
  font-size: 14px;
}
:deep(.cm-content) {
  cursor: text;
  user-select: text;
  -webkit-user-select: text;
}
:deep(.cm-focused) {
  outline: none;
}
</style>
