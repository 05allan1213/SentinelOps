import { expect, test, type Page } from '@playwright/test'

const envelope = (data: unknown) => JSON.stringify({ message: 'OK', data })

const runRow = (overrides: Record<string, unknown> = {}) => ({
  run_id: 'run-1',
  session_id: 'sess-1',
  workflow_key: 'wf',
  runtime_mode: 'durable_v1',
  status: 'succeeded',
  current_phase: 'completed',
  agent: 'planner',
  attempt: 1,
  worker_id: 'worker-1',
  lease_state: 'active',
  lease_generation: 3,
  heartbeat_at: '2026-09-10T00:00:05Z',
  recovery_mode: '',
  park_reason: '',
  runtime_version: 'runtime-1',
  runtime_compatibility_hash: 'hash-1',
  query_hash: 'query-1',
  budget: { model_calls: 2, tool_calls: 1, elapsed_ms: 1000, exhausted: false, availability: 'available', data_quality: 'complete' },
  usage_quality: 'complete',
  trace_quality: 'complete',
  started_at: '2026-09-10T00:00:00Z',
  finished_at: null,
  duration_ms: 1234,
  availability: 'available',
  data_quality: 'complete',
  ...overrides,
})

const listRes = (items: Record<string, unknown>[], extra: Record<string, unknown> = {}) => envelope({
  items,
  page: { page: 1, page_size: 20, total: items.length, has_next: false },
  availability: 'available',
  data_quality: 'complete',
  ...extra,
})

async function installAuth(page: Page) {
  await page.addInitScript(() => localStorage.setItem('token', 'controlled-runtime-token'))
}

for (const width of [1280, 1440]) {
  test(`runs navigation, url filters, canonical badges and legacy labelling at ${width}px`, async ({ page }) => {
    await installAuth(page)
    const requests: string[] = []
    await page.route('**/api/runtime/v1/runs**', route => {
      requests.push(route.request().url())
      return route.fulfill({
        contentType: 'application/json',
        body: listRes([
          runRow(),
          runRow({ run_id: 'run-parked', status: 'parked', current_phase: 'unknown', agent: 'executor', attempt: 2, recovery_mode: 'manual' }),
          runRow({ run_id: 'run-legacy', runtime_mode: 'legacy_v1', status: 'succeeded' }),
        ]),
      })
    })
    await page.setViewportSize({ width, height: 900 })
    await page.goto('/runtime/runs')

    await expect(page.getByRole('heading', { name: 'Agent Runtime' })).toBeVisible({ timeout: 15000 })
    await expect(page.getByRole('link', { name: 'Runs' })).toBeVisible()
    await expect(page.getByRole('link', { name: 'Capabilities' })).toBeVisible()
    await expect(page.getByRole('link', { name: 'Safety' })).toBeVisible()
    await expect(page.getByRole('link', { name: 'Worker Health' })).toBeVisible()
    await expect(page.getByTestId('runtime-run-row')).toHaveCount(3)

    // Durable default: no include_legacy filter is sent.
    expect(requests[0]).toContain('/api/runtime/v1/runs')
    expect(requests[0]).not.toContain('include_legacy')

    // Only `succeeded` renders as success; parked stays attention and legacy rows are labelled read-only.
    await expect(page.locator('[data-testid="runtime-status-badge"][data-tone="success"]')).toHaveCount(2)
    await expect(page.locator('[data-testid="runtime-status-badge"][data-status="parked"]')).toHaveAttribute('data-tone', 'attention')
    await expect(page.getByTestId('runtime-legacy-chip')).toHaveCount(1)

    // URL filter synchronisation: status select then a session filter.
    await page.getByRole('button', { name: '全部状态' }).click()
    await page.getByRole('button', { name: '已搁置' }).click()
    await expect.poll(() => new URL(page.url()).searchParams.get('status')).toBe('parked')
    await expect.poll(() => requests.some(url => url.includes('status=parked'))).toBe(true)

    await page.getByLabel('Session ID').fill('sess-42')
    await expect.poll(() => new URL(page.url()).searchParams.get('session_id')).toBe('sess-42')
    await expect.poll(() => requests.some(url => url.includes('session_id=sess-42'))).toBe(true)
    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(width)
  })

  test(`distinct empty, partial, unavailable and failed states at ${width}px`, async ({ page }) => {
    await installAuth(page)
    let mode: 'empty' | 'partial' | 'unavailable' | 'ok' | 'fail' = 'empty'
    await page.route('**/api/runtime/v1/runs**', route => {
      if (mode === 'empty') return route.fulfill({ contentType: 'application/json', body: listRes([]) })
      if (mode === 'partial') return route.fulfill({ contentType: 'application/json', body: listRes([runRow({ run_id: 'run-partial', status: 'running', current_phase: 'executing' })], { availability: 'partial', data_quality: 'reconstructed', reason_code: 'attempt_projection_rebuilt' }) })
      if (mode === 'unavailable') return route.fulfill({ contentType: 'application/json', body: envelope({ items: [], page: { page: 1, page_size: 20, total: 0, has_next: false }, availability: 'unavailable', data_quality: 'unknown', reason_code: 'not_observed', not_run: true }) })
      if (mode === 'fail') return route.fulfill({ status: 500, contentType: 'application/json', body: JSON.stringify({ message: 'runtime unavailable' }) })
      return route.fulfill({ contentType: 'application/json', body: listRes([runRow({ run_id: 'run-ok' }), runRow({ run_id: 'run-ok-2', status: 'running', current_phase: 'executing' })]) })
    })
    await page.setViewportSize({ width, height: 900 })
    await page.goto('/runtime/runs')

    await expect(page.getByText('暂无数据')).toBeVisible()
    await expect(page.getByTestId('runtime-status-badge')).toHaveCount(0)

    mode = 'partial'
    await page.getByRole('button', { name: '刷新' }).click()
    await expect(page.getByTestId('runtime-runs-partial')).toBeVisible()
    await expect(page.getByTestId('runtime-run-row')).toHaveCount(1)

    mode = 'unavailable'
    await page.getByRole('button', { name: '刷新' }).click()
    await expect(page.getByTestId('runtime-runs-unavailable')).toBeVisible()
    await expect(page.getByTestId('runtime-runs-unavailable')).toContainText('not_observed')

    // Failed refetch keeps the previously proven rows visible.
    mode = 'ok'
    await page.getByRole('button', { name: '刷新' }).click()
    await expect(page.getByTestId('runtime-run-row')).toHaveCount(2)
    mode = 'fail'
    await page.getByRole('button', { name: '刷新' }).click()
    await expect(page.getByTestId('runtime-runs-error')).toBeVisible()
    await expect(page.getByRole('button', { name: '重试' })).toBeVisible()
    await expect(page.getByTestId('runtime-run-row')).toHaveCount(2)
  })
}

test('row selection navigates to the frozen detail route without claiming success', async ({ page }) => {
  await installAuth(page)
  await page.route('**/api/runtime/v1/runs**', route => route.fulfill({
    contentType: 'application/json',
    body: listRes([runRow({ run_id: 'run-open', status: 'reconciling', current_phase: 'reconciling' })]),
  }))
  await page.setViewportSize({ width: 1280, height: 900 })
  await page.goto('/runtime/runs')
  await expect(page.locator('[data-testid="runtime-status-badge"][data-status="reconciling"]')).toHaveAttribute('data-tone', 'attention')
  await page.getByTestId('runtime-run-row').click()
  await expect.poll(() => new URL(page.url()).pathname).toBe('/runtime/runs/run-open')
})

test('local time filters send zoned RFC3339 values accepted by the API contract', async ({ page }) => {
  await installAuth(page)
  const fromValues: string[] = []
  await page.route('**/api/runtime/v1/runs**', route => {
    const from = new URL(route.request().url()).searchParams.get('from')
    if (from) fromValues.push(from)
    const valid = !from || /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:\d{2})$/.test(from)
    return route.fulfill({ status: valid ? 200 : 400, contentType: 'application/json', body: valid ? listRes([runRow()]) : JSON.stringify({ message: 'from must be RFC3339' }) })
  })
  await page.goto('/runtime/runs')
  await page.getByLabel('开始时间').fill('2026-09-11T10:30')
  await expect.poll(() => fromValues.length).toBeGreaterThan(0)
  const expected = await page.evaluate(() => new Date('2026-09-11T10:30').toISOString())
  expect(fromValues.at(-1)).toBe(expected)
  await expect(page.getByTestId('runtime-runs-error')).toHaveCount(0)
  await page.reload()
  await expect(page.getByLabel('开始时间')).toHaveValue('2026-09-11T10:30')
})
