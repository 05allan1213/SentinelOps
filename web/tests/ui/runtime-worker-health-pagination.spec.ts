import { expect, test, type Page } from '@playwright/test'
import { WORKERS, WORKER_AGGREGATE, deferred, installAuth, installFallback, queryOf, slicePage, worker } from './fixtures/runtime-pagination'

const workerHealthPath = '**/api/runtime/v1/worker-health**'
const paginationNav = 'main nav[aria-label="Worker Health 分页"]'
const GLOBAL_AGGREGATE = { total: '4', active: '2', idle: '1', stale: '1' }

async function installWorkerHealthRoute(
  page: Page,
  requests: URL[],
  extra: Record<string, unknown> = {},
  gateForPage?: { page: number; promise: Promise<void> },
) {
  await page.route(workerHealthPath, async route => {
    const url = new URL(route.request().url())
    requests.push(url)
    if (gateForPage && queryOf(url).page === String(gateForPage.page)) await gateForPage.promise
    return route.fulfill({
      json: {
        data: {
          ...slicePage(WORKERS, url.toString()),
          aggregate: WORKER_AGGREGATE,
          availability: 'available',
          data_quality: 'complete',
          ...extra,
        },
      },
    })
  })
}

const aggregateSnapshot = async (page: Page) => ({
  total: await page.getByTestId('runtime-worker-aggregate-total').innerText(),
  active: await page.getByTestId('runtime-worker-aggregate-active').innerText(),
  idle: await page.getByTestId('runtime-worker-aggregate-idle').innerText(),
  stale: await page.getByTestId('runtime-worker-aggregate-stale').innerText(),
})

test('worker health pagination changes rows and pages while the global aggregate stays fixed', async ({ page }) => {
  await installAuth(page)
  await installFallback(page)
  const requests: URL[] = []
  await installWorkerHealthRoute(page, requests)

  await page.goto('/runtime/worker-health?page=1&page_size=2')
  const rows = page.getByTestId('runtime-worker-row')
  await expect(rows).toHaveCount(2)
  await expect(rows.nth(0)).toHaveAttribute('data-worker-id', 'worker-a')
  await expect(rows.nth(1)).toHaveAttribute('data-worker-id', 'worker-b')
  expect(queryOf(requests[0])).toMatchObject({ page: '1', page_size: '2' })
  await expect(page.locator(paginationNav)).toContainText('共 4 条')
  await expect(page.locator(paginationNav)).toContainText('1 / 2')

  // The page carries 2 rows while the summary reports the global aggregate of 4 workers.
  expect(await aggregateSnapshot(page)).toEqual(GLOBAL_AGGREGATE)

  await page.getByRole('button', { name: '下一页' }).click()
  await expect.poll(() => new URL(page.url()).searchParams.get('page')).toBe('2')
  await expect(rows.nth(0)).toHaveAttribute('data-worker-id', 'worker-c')
  await expect(rows.nth(1)).toHaveAttribute('data-worker-id', 'worker-d')
  await expect.poll(() => queryOf(requests.at(-1)!).page).toBe('2')
  expect(queryOf(requests.at(-1)!)).toMatchObject({ page: '2', page_size: '2' })

  // Same aggregate on page 2: it is never recomputed from the current page items.
  expect(await aggregateSnapshot(page)).toEqual(GLOBAL_AGGREGATE)

  await page.getByRole('button', { name: '上一页' }).click()
  await expect.poll(() => new URL(page.url()).searchParams.get('page')).toBe('1')
  await expect(rows.nth(0)).toHaveAttribute('data-worker-id', 'worker-a')
  expect(await aggregateSnapshot(page)).toEqual(GLOBAL_AGGREGATE)
})

test('worker health keepPreviousData keeps page 1 workers and the global aggregate while page 2 is pending', async ({ page }) => {
  await installAuth(page)
  await installFallback(page)
  const requests: URL[] = []
  const gate = deferred()
  await installWorkerHealthRoute(page, requests, {}, { page: 2, promise: gate.promise })

  await page.goto('/runtime/worker-health?page=1&page_size=2')
  const rows = page.getByTestId('runtime-worker-row')
  await expect(rows).toHaveCount(2)
  expect(await aggregateSnapshot(page)).toEqual(GLOBAL_AGGREGATE)

  const pendingRequest = page.waitForRequest(request => new URL(request.url()).searchParams.get('page') === '2')
  await page.getByRole('button', { name: '下一页' }).click()
  await pendingRequest

  // Pending: previous rows and the global summary stay, with explicit refreshing feedback.
  await expect(rows.nth(0)).toHaveAttribute('data-worker-id', 'worker-a')
  await expect(rows.nth(1)).toHaveAttribute('data-worker-id', 'worker-b')
  expect(await aggregateSnapshot(page)).toEqual(GLOBAL_AGGREGATE)
  const refreshing = page.locator('section[data-state-kind="loading"]')
  await expect(refreshing).toHaveCount(1)
  await expect(refreshing).toContainText('正在加载第 2 页')
  await expect(page.getByTestId('runtime-worker-empty')).toHaveCount(0)
  await expect(page.locator('section[data-state-kind="error"]')).toHaveCount(0)
  await expect(page.locator(paginationNav)).toHaveAttribute('aria-busy', 'true')
  await expect(page.getByRole('button', { name: '下一页' })).toBeDisabled()

  gate.release()

  await expect(rows.nth(0)).toHaveAttribute('data-worker-id', 'worker-c')
  await expect(rows.nth(1)).toHaveAttribute('data-worker-id', 'worker-d')
  expect(await aggregateSnapshot(page)).toEqual(GLOBAL_AGGREGATE)
  await expect(page.locator('section[data-state-kind="loading"]')).toHaveCount(0)
  await expect(page.locator(paginationNav)).not.toHaveAttribute('aria-busy', 'true')
})

test('worker health truth semantics survive pagination: stale stays stale, partial stays partial', async ({ page }) => {
  await installAuth(page)
  await installFallback(page)
  const requests: URL[] = []
  await page.route(workerHealthPath, route => {
    const url = new URL(route.request().url())
    requests.push(url)
    const items = [
      worker('worker-b', 'stale', {
        heartbeat_at: '2026-09-09T20:00:00Z',
        last_error: 'heartbeat expired',
        availability: 'partial',
        data_quality: 'partial',
        reason_code: 'not_observed',
        not_run: true,
      }),
      worker('worker-a', 'active', { availability: 'partial', data_quality: 'partial' }),
    ]
    return route.fulfill({
      json: {
        data: {
          ...slicePage(items, url.toString()),
          aggregate: { total: 2, active: 1, idle: null, stale: 1, availability: 'partial', data_quality: 'partial' },
          availability: 'partial',
          data_quality: 'partial',
          reason_code: 'heartbeat_observation_partial',
        },
      },
    })
  })

  await page.goto('/runtime/worker-health?page=1&page_size=50')
  const stale = page.locator('[data-testid="runtime-worker-status"][data-status="stale"]')
  await expect(stale).toHaveText('stale')
  const tone = await stale.getAttribute('class')
  expect(tone).toContain('amber')
  expect(tone).not.toContain('emerald')

  // Human-readable state and the raw fact agree instead of contradicting each other.
  const availability = page.getByTestId('runtime-quality-availability').first()
  await expect(availability).toHaveAttribute('data-availability', 'partial')
  await expect(availability).toHaveText('部分可用')
  await expect(page.getByTestId('runtime-quality-reason').first()).toContainText('heartbeat_observation_partial')

  // Nullable aggregate facts keep “—” instead of a fabricated zero.
  await expect(page.getByTestId('runtime-worker-aggregate-idle')).toHaveText('—')
  await expect(page.getByTestId('runtime-worker-aggregate-stale')).toHaveText('1')
  await expect(page.locator(paginationNav)).toContainText('共 2 条')
})

test('worker health unavailable observation is not rendered as an empty list', async ({ page }) => {
  await installAuth(page)
  await installFallback(page)
  await page.route(workerHealthPath, route => route.fulfill({
    json: {
      data: {
        items: [],
        page: { page: 1, page_size: 50, total: 0, has_next: false },
        aggregate: null,
        availability: 'unavailable',
        data_quality: 'unknown',
        reason_code: 'not_observed',
        not_run: true,
      },
    },
  }))

  await page.goto('/runtime/worker-health')
  const empty = page.getByTestId('runtime-worker-empty')
  await expect(empty).toBeVisible()
  await expect(empty).toContainText('不可用')
  await expect(empty).toContainText('not_observed')
  await expect(empty).not.toContainText('暂无 Worker 观测记录')
  await expect(page.getByTestId('runtime-quality-reason').first()).toContainText('not_observed')
  await expect(page.getByTestId('runtime-quality-not-run').first()).toBeVisible()
  await expect(page.getByTestId('runtime-worker-aggregate-total')).toHaveText('—')
})
