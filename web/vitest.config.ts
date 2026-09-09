import { fileURLToPath, URL } from 'node:url'
import { defineConfig } from 'vitest/config'

export default defineConfig({
  resolve: {
    alias: [
      { find: /^lowlight$/, replacement: fileURLToPath(new URL('./src/components/markdown/lowlight-core.ts', import.meta.url)) },
      { find: '@', replacement: fileURLToPath(new URL('./src', import.meta.url)) },
    ],
  },
  test: {
    environment: 'jsdom',
    server: { deps: { inline: ['rehype-highlight'] } },
    globals: true,
    include: ['tests/unit/**/*.test.{ts,tsx}'],
    setupFiles: ['./tests/unit/setup.ts'],
  },
})
