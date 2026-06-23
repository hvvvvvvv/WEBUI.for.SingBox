import { readdirSync, readFileSync, rmSync } from 'node:fs'
import { resolve } from 'node:path'
import { fileURLToPath, URL } from 'node:url'

import vue from '@vitejs/plugin-vue'
import { defineConfig, type Plugin } from 'vite'

const escapeInlineScript = (code: string) =>
  code.replace(/<\/script/gi, '<\\/script').replace(/<!--/g, '<\\!--')

const escapeInlineStyle = (code: string | Uint8Array) =>
  String(code).replace(/<\/style/gi, '<\\/style')

const singleHtmlPlugin = (): Plugin => {
  let outDir = ''

  return {
    name: 'single-html-bundle',
    apply: 'build',
    enforce: 'post',
    configResolved(config) {
      outDir = resolve(config.root, config.build.outDir)
    },
    generateBundle(_, bundle) {
      const htmlFile = Object.keys(bundle).find((fileName) => fileName.endsWith('.html'))
      if (!htmlFile) return

      const htmlAsset = bundle[htmlFile]
      if (htmlAsset.type !== 'asset' || typeof htmlAsset.source !== 'string') return

      let html = htmlAsset.source

      for (const [fileName, asset] of Object.entries(bundle)) {
        if (asset.type !== 'asset' || !fileName.endsWith('.css')) continue
        html = html.replace(
          new RegExp(`<link[^>]+href=["']\\.?/?${fileName.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}["'][^>]*>`, 'g'),
          () => `<style>${escapeInlineStyle(asset.source)}</style>`,
        )
        delete bundle[fileName]
      }

      for (const [fileName, chunk] of Object.entries(bundle)) {
        if (chunk.type !== 'chunk' || !fileName.endsWith('.js')) continue
        html = html.replace(
          new RegExp(`<script([^>]*)src=["']\\.?/?${fileName.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}["']([^>]*)></script>`, 'g'),
          (_match, before, after) => `<script${before}${after}>${escapeInlineScript(chunk.code)}</script>`,
        )
        delete bundle[fileName]
      }

      html = html.replace(/<link[^>]+rel=["']modulepreload["'][^>]*>\s*/g, '')

      const faviconFile = Object.keys(bundle).find((fileName) => fileName.endsWith('favicon.ico'))
      const faviconAsset = faviconFile ? bundle[faviconFile] : undefined
      const faviconSource =
        faviconAsset?.type === 'asset'
          ? faviconAsset.source
          : readFileSync(fileURLToPath(new URL('./public/favicon.ico', import.meta.url)))
      const faviconBase64 = Buffer.from(faviconSource).toString('base64')
      html = html.replace(
        /<link\s+rel=["']icon["']\s+href=["'][^"']+["']\s*\/?>/,
        `<link rel="icon" href="data:image/x-icon;base64,${faviconBase64}" />`,
      )
      if (faviconFile) {
        delete bundle[faviconFile]
      }

      htmlAsset.source = html
    },
    closeBundle() {
      for (const fileName of readdirSync(outDir)) {
        if (fileName !== 'index.html') {
          rmSync(resolve(outDir, fileName), { recursive: true, force: true })
        }
      }
    },
  }
}

// https://vitejs.dev/config/
export default defineConfig({
  base: './',
  plugins: [vue(), singleHtmlPlugin()],
  resolve: {
    extensions: ['.ts', '.js'],
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
      vue: 'vue/dist/vue.esm-bundler.js',
    },
  },
  server: {
    proxy: {
      '/api': {
        target: 'http://localhost:9090',
        changeOrigin: true,
      },
      '/ws': {
        target: 'ws://localhost:9090',
        ws: true,
      },
    },
  },
  build: {
    cssCodeSplit: false,
    chunkSizeWarningLimit: 4096, // 4MB
    rolldownOptions: {
      output: {
        codeSplitting: false,
      },
    },
  },
})
