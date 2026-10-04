import { fileURLToPath, URL } from 'node:url'

export default {
  root: fileURLToPath(new URL('../frontend', import.meta.url)),
  resolve: {
    dedupe: ['vue', 'pinia', '@bufbuild/protobuf', '@connectrpc/connect'],
    alias: {
      '@': fileURLToPath(new URL('../frontend/src', import.meta.url)),
      '@gen': fileURLToPath(new URL('../frontend/gen', import.meta.url)),
      vue: fileURLToPath(
        new URL('../frontend/node_modules/vue/dist/vue.esm-bundler.js', import.meta.url),
      ),
    },
  },
  test: {
    dir: fileURLToPath(new URL('./frontend', import.meta.url)),
    include: ['**/*.test.ts'],
  },
}
