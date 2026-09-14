import { defineConfig, devices } from '@playwright/test'

// Live integration sweep config: real Vite dev server, real Go API, real MySQL/Redis/Milvus
// and a real provider. No route interception. Lives outside `testDir: ./tests` and outside
// the default `outputDir` (test-results/) so the mocked suites never clean it.
export default defineConfig({
  testDir: '.',
  testMatch: '**/*.live.ts',
  timeout: 180_000,
  fullyParallel: false,
  workers: 1,
  reporter: 'line',
  outputDir: 'artifacts',
  use: {
    baseURL: process.env.SENTINELOPS_E2E_BASE_URL ?? 'http://127.0.0.1:5173',
    trace: 'retain-on-failure',
    actionTimeout: 20_000,
  },
  projects: [
    { name: 'chromium-1280', use: { ...devices['Desktop Chrome'], viewport: { width: 1280, height: 1000 } } },
  ],
})
