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

test('late RAG window response cannot replace current metrics; failed refresh retains them', async ({ page }) => {
  await page.addInitScript(() => localStorage.setItem('token', 'fixture'))
  const metrics = (total: number) => ({ success_rate: 0.5, avg_latency_ms: 30, p95_latency_ms: 40, total_runs: total, avg_retrieved_docs: 3, avg_top_score: 0.8, trends: [] })
  await page.route('**/api/**', route => route.fulfill({ json: { data: { list: [], recent: [], total: 0 } } }))
  let release: (() => void) | undefined
  let fail = false
  await page.route('**/api/rageval/v1/dashboard**', async route => {
    const old = new URL(route.request().url()).searchParams.get('window') === '24h'
    if (old) await new Promise<void>(resolve => { release = resolve })
    await route.fulfill({ status: fail ? 503 : 200, json: { data: metrics(old ? 111 : 222) } })
  })
  await page.goto('/rag-eval')
  await expect.poll(() => Boolean(release)).toBe(true)
  await page.getByRole('button', { name: '7d', exact: true }).click()
  await expect(page.getByText('共 222 次请求')).toBeVisible()
  release!()
  await expect(page.getByText('共 111 次请求')).toHaveCount(0)
  fail = true
  await page.getByRole('button', { name: '刷新', exact: true }).click()
  await expect(page.getByTestId('rag-dashboard-state')).toContainText('保留上次有效数据')
  await expect(page.getByText('共 222 次请求')).toBeVisible()
})
