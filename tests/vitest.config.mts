import { fileURLToPath, URL } from 'node:url'
import vue from '../frontend/node_modules/@vitejs/plugin-vue/dist/index.mjs'

export default {
  root: fileURLToPath(new URL('../frontend', import.meta.url)),
  plugins: [vue()],
  resolve: {
    dedupe: [
      'vue',
      'vue-i18n',
      'vue-router',
      'pinia',
      '@bufbuild/protobuf',
      '@connectrpc/connect',
      '@codemirror/state',
      '@codemirror/view',
      'prettier',
      'qrcode',
    ],
    alias: [
      { find: '@', replacement: fileURLToPath(new URL('../frontend/src', import.meta.url)) },
      { find: '@gen', replacement: fileURLToPath(new URL('../frontend/gen', import.meta.url)) },
      {
        find: /^vue$/,
        replacement: fileURLToPath(
          new URL('../frontend/node_modules/vue/dist/vue.esm-bundler.js', import.meta.url),
        ),
      },
    ],
  },
  test: {
    dir: fileURLToPath(new URL('./frontend', import.meta.url)),
    include: ['**/*.test.ts'],
  },
}
