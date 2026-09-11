import { expect, test } from '@playwright/test'
for (const value of [null, {}, '404']) {
  test(`RAG dashboard handles ${JSON.stringify(value)}`, async ({ page }) => {
    const errors: string[] = []
    page.on('pageerror', error => errors.push(error.message))
    await page.addInitScript(() => {
      localStorage.setItem('token', 'fixture')
      localStorage.setItem('app-storage', JSON.stringify({ state: { theme: 'light' }, version: 1 }))
    })
    await page.route('**/api/**', route => route.fulfill({ json: { data: { list: [], recent: [], total: 0 } } }))
    await page.route('**/api/rageval/v1/dashboard**', route => route.fulfill({ status: value === '404' ? 404 : 200, json: { data: value } }))
    await page.goto('/rag-eval')
    await expect(page.getByTestId('rag-dashboard-state')).toContainText('不可用')
    await expect(page.locator('html')).not.toHaveClass(/dark/)
    expect(errors).toEqual([])
  })
}
