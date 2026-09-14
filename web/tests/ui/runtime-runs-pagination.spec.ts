import { expect, test, type Page } from '@playwright/test'
import { RUNS, deferred, installAuth, installFallback, queryOf, slicePage } from './fixtures/runtime-pagination'

const runsPath = '**/api/runtime/v1/runs**'
const paginationNav = 'main nav[aria-label="分页"]'
const RFC3339 = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:\d{2})$/

async function installRunsRoute(page: Page, requests: URL[], gateForPage?: { page: number; promise: Promise<void> }, items = RUNS) {
  await page.route(runsPath, async route => {
    const url = new URL(route.request().url())
    requests.push(url)
    if (gateForPage && queryOf(url).page === String(gateForPage.page)) await gateForPage.promise
    return route.fulfill({
      json: { data: { ...slicePage(items, url.toString(), 20), availability: 'available', data_quality: 'complete' } },
    })
  })
}

async function applyRunFilters(page: Page, requests: URL[]) {
  await page.getByRole('combobox', { name: '状态', exact: true }).selectOption('parked')
  await page.getByRole('textbox', { name: 'Session ID', exact: true }).fill('sess-9')
  await page.getByRole('textbox', { name: 'Agent', exact: true }).fill('planner')
  await page.getByRole('textbox', { name: 'Scope', exact: true }).fill('scope-a')
  await page.getByLabel('开始时间', { exact: true }).fill('2026-09-11T10:30')
  await page.getByLabel('结束时间', { exact: true }).fill('2026-09-12T10:30')
  await page.getByRole('combobox', { name: '排序', exact: true }).selectOption('created_at')
  await page.getByRole('combobox', { name: '方向', exact: true }).selectOption('desc')
  await page.getByRole('checkbox').click()
  await expect.poll(() => new URL(page.url()).searchParams.get('include_legacy')).toBe('true')
  await expect(page.getByRole('checkbox')).toBeChecked()
  await expect.poll(() => queryOf(requests.at(-1)!).include_legacy).toBe('true')
}

test('runs PaginationBar keeps URL, request query, rows and filters in sync', async ({ page }) => {
  await installAuth(page)
  await installFallback(page)
  const requests: URL[] = []
  await installRunsRoute(page, requests)

  await page.goto('/runtime/runs')
  const rows = page.getByTestId('runtime-run-row')
  await expect(rows).toHaveCount(20)
  await expect(rows.nth(0)).toHaveAttribute('data-run-id', 'run-01')
  expect(queryOf(requests[0])).toMatchObject({ page: '1', page_size: '20' })
  await expect(page.locator(paginationNav)).toContainText('共 45 条')
  await expect(page.locator(paginationNav)).toContainText('1 / 3')

  await applyRunFilters(page, requests)
  const fromISO = await page.evaluate(() => new Date('2026-09-11T10:30').toISOString())
  const toISO = await page.evaluate(() => new Date('2026-09-12T10:30').toISOString())

  // Next page: URL and request both move, and every filter survives the page change.
  await page.getByRole('button', { name: '下一页' }).click()
  await expect.poll(() => new URL(page.url()).searchParams.get('page')).toBe('2')
  await expect(rows).toHaveCount(20)
  await expect(rows.nth(0)).toHaveAttribute('data-run-id', 'run-21')
  await expect.poll(() => queryOf(requests.at(-1)!).page).toBe('2')
  const paged = queryOf(requests.at(-1)!)
  expect(paged).toMatchObject({
    page: '2',
    page_size: '20',
    status: 'parked',
    session_id: 'sess-9',
    agent: 'planner',
    scope: 'scope-a',
    sort: 'created_at',
    direction: 'desc',
    include_legacy: 'true',
  })
  expect(paged.from).toBe(fromISO)
  expect(paged.to).toBe(toISO)
  expect(paged.from).toMatch(RFC3339)
  const urlParams = new URL(page.url()).searchParams
  expect(Object.fromEntries(urlParams.entries())).toMatchObject({
    page: '2',
    status: 'parked',
    session_id: 'sess-9',
    agent: 'planner',
    scope: 'scope-a',
    sort: 'created_at',
    direction: 'desc',
    include_legacy: 'true',
  })

  // Previous page returns to page 1 while keeping the filters. The page-1 entry is still fresh
  // (shared staleTime is 60s), so the cache serves it without a duplicate HTTP request.
  const requestsBeforePrev = requests.length
  await page.getByRole('button', { name: '上一页' }).click()
  await expect.poll(() => new URL(page.url()).searchParams.get('page')).toBeNull()
  await expect(rows).toHaveCount(20)
  await expect(rows.nth(0)).toHaveAttribute('data-run-id', 'run-01')
  expect(requests.length).toBe(requestsBeforePrev)

  // Refresh proves the page-1 request shape still carries page=1 plus every filter.
  await page.getByRole('button', { name: '刷新' }).click()
  await expect.poll(() => requests.length).toBeGreaterThan(requestsBeforePrev)
  await expect.poll(() => queryOf(requests.at(-1)!).page).toBe('1')
  expect(queryOf(requests.at(-1)!)).toMatchObject({ status: 'parked', session_id: 'sess-9', include_legacy: 'true' })

  // Page size change resets page and issues the matching request.
  await page.getByLabel('每页条数').selectOption('50')
  await expect.poll(() => new URL(page.url()).searchParams.get('page_size')).toBe('50')
  await expect(rows).toHaveCount(45)
  await expect.poll(() => queryOf(requests.at(-1)!)).toMatchObject({ page: '1', page_size: '50', status: 'parked' })
  expect(new URL(page.url()).searchParams.get('page')).toBeNull()
})

test('runs keepPreviousData keeps rows and filters while page 2 is pending', async ({ page }) => {
  await installAuth(page)
  await installFallback(page)
  const requests: URL[] = []
  const gate = deferred()
  // 25 runs -> page 2 holds 5 rows, so the replacement is unambiguous.
  await installRunsRoute(page, requests, { page: 2, promise: gate.promise }, RUNS.slice(0, 25))

  await page.goto('/runtime/runs?status=parked&session_id=sess-9&page_size=20')
  const rows = page.getByTestId('runtime-run-row')
  await expect(rows).toHaveCount(20)
  await expect(rows.nth(0)).toHaveAttribute('data-run-id', 'run-01')
  const filtersBefore = await page.getByTestId('runtime-runs-filters').innerText()
  expect(filtersBefore).toContain('parked')
  expect(filtersBefore).toContain('sess-9')

  const pendingRequest = page.waitForRequest(request => new URL(request.url()).searchParams.get('page') === '2')
  await page.getByRole('button', { name: '下一页' }).click()
  await pendingRequest

  // Pending page 2: old rows stay, filters stay, the fetching indicator is explicit.
  await expect(rows).toHaveCount(20)
  await expect(rows.nth(0)).toHaveAttribute('data-run-id', 'run-01')
  const refreshing = page.locator('section[data-state-kind="loading"]')
  await expect(refreshing).toContainText('正在加载第 2 页')
  await expect(page.getByText('暂无数据')).toHaveCount(0)
  await expect(page.getByTestId('runtime-runs-error')).toHaveCount(0)
  await expect(page.getByTestId('runtime-runs-filters')).toHaveText(filtersBefore)
  await expect(page.locator(paginationNav)).toHaveAttribute('aria-busy', 'true')
  await expect(page.getByRole('button', { name: '下一页' })).toBeDisabled()
  expect(queryOf(requests.at(-1)!)).toMatchObject({ page: '2', status: 'parked', session_id: 'sess-9' })

  gate.release()

  await expect(rows).toHaveCount(5)
  await expect(rows.nth(0)).toHaveAttribute('data-run-id', 'run-21')
  await expect(page.locator('section[data-state-kind="loading"]')).toHaveCount(0)
  await expect(page.getByTestId('runtime-runs-filters')).toHaveText(filtersBefore)
  await expect.poll(() => new URL(page.url()).searchParams.get('page')).toBe('2')
  expect(queryOf(requests.at(-1)!)).toMatchObject({ page: '2', status: 'parked', session_id: 'sess-9' })
})
