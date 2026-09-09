import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: [
      { find: /^lowlight$/, replacement: decodeURIComponent(new URL('./src/components/markdown/lowlight-core.ts', import.meta.url).pathname) },
      { find: '@', replacement: '/src' },
    ],
  },
  server: {
    port: 5173,
    proxy: {
      '/api': {
        target: 'http://localhost:8001',
        changeOrigin: true,
        secure: false,
        ws: true,
      },
    },
  },
})
