import { expect, test } from '@playwright/test'
for (const width of [1280, 1440]) {
  test(`analysis columns stay local at ${width}`, async ({ page }) => {
    await page.setViewportSize({ width, height: 1000 })
    await page.addInitScript(() => localStorage.setItem('token', 'fixture'))
    await page.route('**/api/**', route => route.fulfill({ json: { data: { total: 1, events: [] } } }))
    await page.goto('/events/analysis')
    await expect(page.getByText('思考链路', { exact: true })).toBeVisible()
    expect(await page.locator('main').evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true)
    expect(await page.evaluate(() => document.documentElement.scrollWidth === document.documentElement.clientWidth)).toBe(true)
  })
}
