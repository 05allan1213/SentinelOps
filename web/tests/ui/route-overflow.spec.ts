import { expect, test } from '@playwright/test'

// Batch 9 final responsive pass: every main route keeps document-level
// horizontal overflow at zero on the three desktop reference widths. Local
// table/code scrolling is allowed inside the page, document scroll is not.
const routes = [
  '/dashboard',
  '/subscriptions',
  '/events',
  '/events/analysis',
  '/reports',
  '/chat',
  '/knowledge',
  '/term-mapping',
  '/traces',
  '/rag-eval',
  '/ingest',
  '/ops',
  '/runtime/runs',
  '/runtime/capabilities',
  '/runtime/worker-health',
  '/settings',
]

for (const width of [1280, 1440, 1920]) {
  for (const route of routes) {
    test(`${route} has no document overflow at ${width}`, async ({ page }) => {
      await page.setViewportSize({ width, height: 1000 })
      await page.addInitScript(() => localStorage.setItem('token', 'fixture'))
      await page.route('**/api/**', route_ =>
        route_.fulfill({ json: { code: 0, message: 'OK', data: { total: 0, list: [], items: [] } } }))
      await page.goto(route)
      await page.waitForLoadState('networkidle')
      await expect(page.locator('main')).toBeVisible()
      const geometry = await page.evaluate(() => {
        const root = document.documentElement
        const body = document.body
        return {
          rootOverflow: root.scrollWidth - root.clientWidth,
          bodyOverflow: body.scrollWidth - body.clientWidth,
          windowOverflow: root.scrollWidth - window.innerWidth,
        }
      })
      expect(geometry.rootOverflow).toBeLessThanOrEqual(0)
      expect(geometry.bodyOverflow).toBeLessThanOrEqual(0)
      expect(geometry.windowOverflow).toBeLessThanOrEqual(0)
    })
  }
}
