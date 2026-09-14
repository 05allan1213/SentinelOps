import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

export default defineConfig({
  plugins: [react(), tailwindcss()],
  build: {
    rollupOptions: {
      output: {
        // 图表库按「渲染引擎 / 图表实现」拆成两个惰性 vendor chunk：两者都只被
        // 图表路由引用，拆分不改变加载时机，只让单个 chunk 体积回到阈值内。
        manualChunks(id: string) {
          if (id.includes('node_modules/echarts/lib/chart/') || id.includes('node_modules/echarts/charts')) {
            return 'vendor-echarts-charts'
          }
          if (id.includes('node_modules/echarts') || id.includes('node_modules/zrender')) {
            return 'vendor-echarts-core'
          }
          return undefined
        },
      },
    },
  },
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
