import { expect, test } from '@playwright/test'
test('analysis source is explicitly legacy/demo', async ({ page }) => {
  await page.addInitScript(() => localStorage.setItem('token', 'fixture'))
  await page.route('**/api/**', route => route.fulfill({ json: { data: { total: 1, events: [] } } }))
  await page.goto('/events/analysis')
  await expect(page.getByTestId('analysis-source-label')).toContainText('legacy / demo')
  await expect(page.getByTestId('analysis-source-label')).toContainText('模拟')
  await expect(page.getByTestId('runtime-recovery-button')).toHaveCount(0)
  await expect(page.locator('a[href="/logs"]')).toHaveCount(0)
})
