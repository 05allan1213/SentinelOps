import { expect, test, type Page } from '@playwright/test'
import { envelope, meta, runDetailRes, timelineRes, runtimeEvent } from './fixtures/runtime-detail'

async function installAuth(page: Page) {
  await page.addInitScript(() => localStorage.setItem('token', 'controlled-runtime-token'))
}

const detailPath = '**/api/runtime/v1/runs/run-1'
const timelinePath = '**/api/runtime/v1/runs/run-1/timeline**'

/** Attempts/checkpoints/events sub-resources are E-05/E-02 surfaces; the detail spec only needs them mocked. */
async function installSubresources(page: Page) {
  for (const runId of ['run-1', 'run-2']) {
    for (const resource of ['attempts', 'checkpoints', 'timeline']) {
      await page.route(`**/api/runtime/v1/runs/${runId}/${resource}**`, route => route.fulfill({
        contentType: 'application/json',
        body: envelope({ items: [], page: { page: 1, page_size: 50, total: 0, has_next: false }, ...meta() }),
      }))
    }
    await page.route(`**/api/runtime/v1/runs/${runId}/events**`, route => route.fulfill({ contentType: 'text/event-stream', body: 'data: [DONE]\n\n' }))
    for (const resource of ['effects', 'evidence', 'context', 'traces']) {
      await page.route(`**/api/runtime/v1/runs/${runId}/${resource}**`, route => route.fulfill({
        contentType: 'application/json',
        body: envelope({ items: [], item: { identity: {}, history_count: 0, gate_keys: [] }, page: { page: 1, page_size: 50, total: 0, has_next: false }, ...meta() }),
      }))
    }
  }
}

for (const width of [1280, 1440]) {
  test(`run detail overview, tabs and full timeline at ${width}px`, async ({ page }) => {
    await installAuth(page)
    await installSubresources(page)
    await page.route(timelinePath, route => route.fulfill({
      contentType: 'application/json',
      body: envelope(timelineRes([
        runtimeEvent(1, 'run.created'),
        runtimeEvent(2, 'agent.plan', { operation_id: 'op-2', attributes: { to_status: 'running' } }),
        runtimeEvent(3, 'run.parked', { attributes: { to_status: 'parked' } }),
      ])),
    }))
    await page.route(detailPath, route => route.fulfill({ contentType: 'application/json', body: envelope(runDetailRes()) }))
    await page.setViewportSize({ width, height: 900 })
    const errors: string[] = []
    page.on('pageerror', error => errors.push(error.message))

    await page.goto('/runtime/runs/run-1')

    // Server facts only; parked never renders as success.
    await expect(page.locator('[data-testid="runtime-status-badge"][data-status="parked"]').first()).toHaveAttribute('data-tone', 'attention')
    await expect(page.getByTestId('runtime-overview-worker')).toHaveText('worker-7')
    await expect(page.getByTestId('runtime-overview-generation')).toHaveText('9')
    await expect(page.getByTestId('runtime-overview-run-match')).toHaveText('匹配')
    await expect(page.getByTestId('runtime-overview-restore')).toHaveText('不允许')
    await expect(page.getByTestId('runtime-overview-shadow')).toContainText('开启')
    await expect(page.getByTestId('runtime-overview-model-calls')).toHaveText('4')
    await expect(page.getByTestId('runtime-overview-recovery-actions')).toHaveText('resume, cancel')
    await expect(page.getByTestId('runtime-quality-reason').first()).toContainText('checkpoint_mismatch')

    // Tabs are URL state and the timeline keeps every canonical event in seq order.
    await expect(page.getByTestId('runtime-detail-tab')).toHaveCount(7)
    await page.getByTestId('runtime-detail-tab').filter({ hasText: '时间线' }).click()
    await expect.poll(() => new URL(page.url()).searchParams.get('tab')).toBe('timeline')
    const rows = page.getByTestId('runtime-timeline-row')
    await expect(rows).toHaveCount(3)
    await expect(rows.nth(0)).toHaveAttribute('data-seq', '1')
    await expect(rows.nth(2)).toHaveAttribute('data-event-type', 'run.parked')

    // Payload is collapsed by default and expands on demand.
    await expect(page.getByTestId('runtime-timeline-detail')).toHaveCount(0)
    await rows.nth(1).getByRole('button').click()
    await expect(page.getByTestId('runtime-timeline-detail')).toHaveCount(1)
    await expect(page.getByTestId('runtime-timeline-attributes')).toContainText('to_status')

    await expect(page.getByTestId('runtime-tab-placeholder')).toHaveCount(0)
    await page.getByTestId('runtime-detail-tab').filter({ hasText: 'Attempts' }).click()
    // E-05 replaced the Attempts placeholder with the real panel; the untouched tabs keep theirs.
    await expect(page.getByTestId('runtime-attempts-panel')).toBeVisible()
    // E-06 replaced the remaining placeholders; every tab now renders a real panel.
    await page.getByTestId('runtime-detail-tab').filter({ hasText: 'Trace' }).click()
    await expect(page.getByTestId('runtime-trace-panel')).toBeVisible()
    await expect(page.getByTestId('runtime-tab-placeholder')).toHaveCount(0)

    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(width)
    expect(errors).toEqual([])
  })

  test(`run detail partial, unavailable and failed states at ${width}px`, async ({ page }) => {
    await installAuth(page)
    await installSubresources(page)
    await page.route(timelinePath, route => route.fulfill({ contentType: 'application/json', body: envelope(timelineRes([])) }))
    let mode: 'partial' | 'unavailable' | 'fail' = 'partial'
    await page.route(detailPath, route => {
      if (mode === 'partial') {
        return route.fulfill({
          contentType: 'application/json',
          body: envelope(runDetailRes({ availability: 'partial', data_quality: 'reconstructed', reason_code: 'attempt_projection_rebuilt', not_run: true })),
        })
      }
      if (mode === 'unavailable') {
        return route.fulfill({
          contentType: 'application/json',
          body: envelope({ availability: 'unavailable', data_quality: 'unknown', reason_code: 'not_observed', not_run: true }),
        })
      }
      return route.fulfill({ status: 500, contentType: 'application/json', body: JSON.stringify({ message: 'runtime detail failed' }) })
    })
    await page.setViewportSize({ width, height: 900 })
    await page.goto('/runtime/runs/run-1')

    await expect(page.getByTestId('runtime-quality-availability').first()).toHaveAttribute('data-availability', 'partial')
    await expect(page.getByTestId('runtime-quality-data').first()).toHaveAttribute('data-quality', 'reconstructed')
    await expect(page.getByTestId('runtime-quality-not-run').first()).toBeVisible()

    mode = 'unavailable'
    await page.getByRole('button', { name: '刷新' }).click()
    await expect(page.getByTestId('runtime-detail-unavailable')).toContainText('not_observed')

    mode = 'fail'
    await page.getByRole('button', { name: '刷新' }).click()
    await expect(page.getByTestId('runtime-detail-error')).toBeVisible()
    await expect(page.getByRole('button', { name: '重试' })).toBeVisible()
  })
}

test('tail starts only for a non-terminal run and never claims success', async ({ page }) => {
  await installAuth(page)
  await page.addInitScript(() => {
    const real = window.fetch.bind(window)
    const calls: string[] = []
    ;(window as unknown as { __runtimeTailCalls: string[] }).__runtimeTailCalls = calls
    window.fetch = async (input, init) => {
      const url = String(input)
      if (url.includes('/api/runtime/v1/runs/') && url.includes('/events')) {
        calls.push(url)
        return new Response(new ReadableStream<Uint8Array>({ start() {} }), { headers: { 'Content-Type': 'text/event-stream' } })
      }
      return real(input, init)
    }
  })
  let status = 'running'
  await page.route(timelinePath, route => route.fulfill({ contentType: 'application/json', body: envelope(timelineRes([])) }))
  await page.route(detailPath, route => route.fulfill({
    contentType: 'application/json',
    body: envelope(runDetailRes({ summary: { status, current_phase: status === 'running' ? 'executing' : 'completed' } })),
  }))
  await page.setViewportSize({ width: 1280, height: 900 })

  await page.goto('/runtime/runs/run-1')
  await expect(page.getByTestId('runtime-tail-state')).toHaveAttribute('data-connected', 'true')
  await expect.poll(() => page.evaluate(() => (window as unknown as { __runtimeTailCalls: string[] }).__runtimeTailCalls.length)).toBe(1)
  expect(await page.evaluate(() => (window as unknown as { __runtimeTailCalls: string[] }).__runtimeTailCalls[0])).toContain('after_seq=0')

  // Terminal server status does not open a reader; the UI still renders the server's terminal fact.
  status = 'succeeded'
  await page.goto('/runtime/runs/run-1')
  await expect(page.locator('[data-testid="runtime-status-badge"][data-status="succeeded"]').first()).toHaveAttribute('data-tone', 'success')
  await expect(page.getByTestId('runtime-tail-state')).toHaveAttribute('data-connected', 'false')
  await page.waitForTimeout(300)
  // The reload resets the per-page counter; a terminal Run opens no reader at all.
  expect(await page.evaluate(() => (window as unknown as { __runtimeTailCalls: string[] }).__runtimeTailCalls.length)).toBe(0)
})

test('run identity, tail and operation state do not leak between runs on client-side navigation', async ({ page }) => {
  await installAuth(page)
  await page.addInitScript(() => {
    const real = window.fetch.bind(window)
    const calls: string[] = []
    ;(window as unknown as { __runtimeTailCalls: string[] }).__runtimeTailCalls = calls
    window.fetch = async (input, init) => {
      const url = String(input)
      if (url.includes('/api/runtime/v1/runs/') && url.includes('/events')) {
        calls.push(url)
        return new Response(new ReadableStream<Uint8Array>({ start() {} }), { headers: { 'Content-Type': 'text/event-stream' } })
      }
      return real(input, init)
    }
  })
  await installSubresources(page)
  await page.route('**/api/runtime/v1/runs/run-1', route => route.fulfill({
    contentType: 'application/json',
    body: envelope(runDetailRes({ summary: { run_id: 'run-1', status: 'succeeded', current_phase: 'completed', worker_id: 'worker-1' } })),
  }))
  await page.route('**/api/runtime/v1/runs/run-2', route => route.fulfill({
    contentType: 'application/json',
    body: envelope(runDetailRes({ summary: { run_id: 'run-2', status: 'running', current_phase: 'executing', worker_id: 'worker-2' } })),
  }))
  await page.setViewportSize({ width: 1280, height: 900 })

  await page.goto('/runtime/runs/run-1')
  await expect(page.getByRole('heading', { name: 'Run run-1' })).toBeVisible()
  await expect(page.getByTestId('runtime-overview-worker')).toHaveText('worker-1')
  await expect(page.getByTestId('runtime-tail-state')).toHaveAttribute('data-connected', 'false')

  // Client-side param change (React Router history navigation) must remount Run-scoped state.
  await page.evaluate(() => {
    window.history.pushState({}, '', '/runtime/runs/run-2')
    window.dispatchEvent(new PopStateEvent('popstate'))
  })

  await expect(page.getByRole('heading', { name: 'Run run-2' })).toBeVisible()
  await expect(page.getByTestId('runtime-overview-worker')).toHaveText('worker-2')
  await expect(page.getByTestId('runtime-tail-state')).toHaveAttribute('data-connected', 'true')
  await expect.poll(() => page.evaluate(() => (window as unknown as { __runtimeTailCalls: string[] }).__runtimeTailCalls.some(url => url.includes('/runs/run-2/events')))).toBe(true)
  expect(await page.evaluate(() => (window as unknown as { __runtimeTailCalls: string[] }).__runtimeTailCalls.some(url => url.includes('/runs/run-1/events')))).toBe(false)
  // The previous Run's facts are not reused as placeholder data.
  await expect(page.getByTestId('runtime-run-overview')).not.toContainText('worker-1')
})
