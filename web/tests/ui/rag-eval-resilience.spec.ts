import { expect, test, type Locator, type Page } from '@playwright/test'

const formatterDashboard = {
  success_rate: 0.875,
  avg_latency_ms: 2832,
  p95_latency_ms: 4200,
  total_runs: 1200,
  avg_retrieved_docs: 3,
  avg_top_score: 0.8,
  success_rate_status: 'good',
  latency_status: 'good',
  trends: [
    { timestamp: '2026-09-14T10:00:00Z', success_rate: 0.875, avg_latency_ms: 2832 },
    { timestamp: '2026-09-14T11:00:00Z', success_rate: 0, avg_latency_ms: 0 },
    { timestamp: '2026-09-14T12:00:00Z', success_rate: 0.5, avg_latency_ms: 30 },
  ],
}

const zeroDashboard = {
  ...formatterDashboard,
  success_rate: 0,
  avg_latency_ms: 0,
  p95_latency_ms: 0,
  total_runs: 0,
  trends: [{ timestamp: '2026-09-14T10:00:00Z', success_rate: 0, avg_latency_ms: 0 }],
}

const zeroTrace = {
  trace_id: 'trace-zero',
  trace_name: 'zero latency trace',
  session_id: 'session-zero',
  status: 'success',
  duration_ms: 0,
  start_time: '2026-09-14 10:00:00',
  feedback_vote: 0,
}

async function installRagFixture(page: Page, dashboard: unknown, traces: unknown[] = []) {
  await page.addInitScript(() => {
    localStorage.setItem('token', 'fixture')
    localStorage.setItem('app-storage', JSON.stringify({ state: { theme: 'light' }, version: 1 }))
  })
  await page.route('**/api/**', route => route.fulfill({ json: { code: 0, message: 'OK', data: { list: [], recent: [], total: 0 } } }))
  await page.route('**/api/rageval/v1/dashboard**', route => route.fulfill({ json: { code: 0, message: 'OK', data: dashboard } }))
  await page.route('**/api/rageval/v1/traces**', route => route.fulfill({ json: { code: 0, message: 'OK', data: { list: traces, total: traces.length } } }))
}

async function hoverTrendPoint(page: Page, timestamp: string) {
  const chart = page.locator('.echarts-for-react').first()
  const label = chart.locator('svg').getByText(timestamp, { exact: true })
  await expect(label).toBeVisible()
  const labelBox = await label.boundingBox()
  const chartBox = await chart.boundingBox()
  expect(labelBox).not.toBeNull()
  expect(chartBox).not.toBeNull()
  const pointX = labelBox!.x + labelBox!.width / 2
  const pointY = chartBox!.y + chartBox!.height / 2
  // ECharts only re-targets the axis pointer on a continuous move; enter the plot, then step to the point.
  await page.mouse.move(chartBox!.x + chartBox!.width / 2, pointY)
  await page.mouse.move(pointX, pointY, { steps: 8 })
  const tooltip = chart.locator('div[style*="white-space: nowrap"]')
  await expect(tooltip).toBeVisible()
  return tooltip
}

async function expectWithinViewport(page: Page, tooltip: Locator) {
  const box = await tooltip.boundingBox()
  const viewport = page.viewportSize()
  expect(box).not.toBeNull()
  expect(viewport).not.toBeNull()
  expect(box!.x).toBeGreaterThanOrEqual(0)
  expect(box!.x + box!.width).toBeLessThanOrEqual(viewport!.width)
  expect(box!.y).toBeGreaterThanOrEqual(0)
  expect(box!.y + box!.height).toBeLessThanOrEqual(viewport!.height)
}

test('formats latency tooltip as duration and success rate tooltip as percentage', async ({ page }) => {
  await installRagFixture(page, formatterDashboard)
  await page.goto('/rag-eval')

  // KPI values use the same duration/percentage semantics as the chart tooltip.
  await expect(page.getByText('87.5%', { exact: true })).toBeVisible()
  await expect(page.getByText('2.8s', { exact: true })).toBeVisible()
  await expect(page.getByText('4.2s', { exact: true })).toBeVisible()

  const first = await hoverTrendPoint(page, '2026-09-14T10:00:00Z')
  await expect(first).toHaveText(/2026-09-14T10:00:00Z\s*成功率\s*87\.5%\s*平均延迟\s*2\.8s/)
  await expectWithinViewport(page, first)
  await expect(first).not.toContainText('2832%')
  await expect(first).not.toContainText('283200.0%')
  await expect(first).not.toContainText('2.8s%')

  const zero = await hoverTrendPoint(page, '2026-09-14T11:00:00Z')
  await expect(zero).toHaveText(/2026-09-14T11:00:00Z\s*成功率\s*0\.0%\s*平均延迟\s*0ms/)
  await expectWithinViewport(page, zero)
  await expect(zero).not.toContainText('—')

  const thirty = await hoverTrendPoint(page, '2026-09-14T12:00:00Z')
  await expect(thirty).toHaveText(/2026-09-14T12:00:00Z\s*成功率\s*50\.0%\s*平均延迟\s*30ms/)
  await expectWithinViewport(page, thirty)
})

test('preserves zero metrics across KPI and trace duration callers', async ({ page }) => {
  await installRagFixture(page, zeroDashboard, [zeroTrace])
  await page.goto('/rag-eval')

  const successCard = page.getByText('成功率', { exact: true }).first().locator('..')
  const latencyCard = page.getByText('平均延迟', { exact: true }).first().locator('..')
  await expect(successCard).toContainText('0.0%')
  await expect(successCard).toContainText('共 0 次请求')
  await expect(latencyCard).toContainText('0ms')
  await expect(latencyCard).not.toContainText('—')
  await expect(page.locator('table').getByText('0ms', { exact: true })).toBeVisible()
})

test('renders zero trace and node durations as 0ms in the detail modal', async ({ page }) => {
  const detail = {
    trace_id: 'trace-zero',
    trace_name: 'zero latency trace',
    session_id: 'session-zero',
    query_text: 'zero duration query',
    status: 'success',
    duration_ms: 0,
    total_input_tokens: 0,
    total_output_tokens: 0,
    estimated_cost_usd: 0,
    start_time: '2026-09-14 10:00:00',
    feedback_vote: 0,
    nodes: [
      {
        node_id: 'node-zero',
        depth: 0,
        node_type: 'RETRIEVER',
        node_name: 'zero retriever',
        status: 'success',
        duration_ms: 0,
        doc_count: 0,
      },
    ],
  }
  await installRagFixture(page, zeroDashboard, [zeroTrace])
  await page.route('**/api/rageval/v1/traces/detail**', route => route.fulfill({ json: { code: 0, message: 'OK', data: detail } }))
  await page.goto('/rag-eval')

  await page.locator('table').getByTitle('查看链路详情').click()
  const modal = page.locator('div.fixed.inset-0.z-50')
  await expect(modal.getByText('链路详情', { exact: true })).toBeVisible()
  await expect(modal.getByText('总耗时').locator('..')).toContainText('0ms')
  await expect(modal.getByText('zero retriever', { exact: true }).locator('..')).toContainText('0ms')
})

test('marks unavailable metrics in a partial dashboard payload without turning null into zero', async ({ page }) => {
  await installRagFixture(page, { ...formatterDashboard, avg_latency_ms: null, p95_latency_ms: 0 })
  await page.goto('/rag-eval')

  await expect(page.getByTestId('rag-dashboard-state')).toContainText('指标部分可用')
  const latencyCard = page.getByText('平均延迟', { exact: true }).first().locator('..')
  const p95Card = page.getByText('P95 延迟', { exact: true }).locator('..')
  await expect(latencyCard).toContainText('—')
  await expect(latencyCard).not.toContainText('0ms')
  await expect(p95Card).toContainText('0ms')
})

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
