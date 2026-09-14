import { expect, test, type Page } from '@playwright/test'
import { CAPABILITIES, deferred, installAuth, installFallback, queryOf, slicePage } from './fixtures/runtime-pagination'

const capabilitiesPath = '**/api/runtime/v1/capabilities**'
const paginationNav = 'main nav[aria-label="Capabilities 分页"]'

async function installCapabilitiesRoute(page: Page, requests: URL[], gateForPage?: { page: number; promise: Promise<void> }) {
  await page.route(capabilitiesPath, async route => {
    const url = new URL(route.request().url())
    requests.push(url)
    if (gateForPage && queryOf(url).page === String(gateForPage.page)) await gateForPage.promise
    return route.fulfill({
      json: { data: { ...slicePage(CAPABILITIES, url.toString()), availability: 'available', data_quality: 'complete' } },
    })
  })
}

test('capabilities pagination keeps URL, request params, rows and truth states in sync', async ({ page }) => {
  await installAuth(page)
  await installFallback(page)
  const requests: URL[] = []
  await installCapabilitiesRoute(page, requests)

  // 1. Initial URL page/page_size drive the request and the rendered page.
  await page.goto('/runtime/capabilities?page=1&page_size=2')
  const rows = page.getByTestId('runtime-capability-row')
  await expect(rows).toHaveCount(2)
  await expect(rows.nth(0)).toContainText('capability-a')
  await expect(rows.nth(1)).toContainText('capability-b')
  expect(queryOf(requests[0])).toMatchObject({ page: '1', page_size: '2' })

  // 2. The pagination summary is the backend PageMeta.total (5), not the visible row count (2).
  const nav = page.locator(paginationNav)
  await expect(nav).toContainText('共 5 条')
  await expect(nav).toContainText('1 / 3')

  // 3. Next page: URL, HTTP page param and rows move together.
  await page.getByRole('button', { name: '下一页' }).click()
  await expect.poll(() => new URL(page.url()).searchParams.get('page')).toBe('2')
  await expect(rows.nth(0)).toContainText('capability-c')
  await expect(rows.nth(1)).toContainText('capability-d')
  await expect.poll(() => queryOf(requests.at(-1)!).page).toBe('2')
  expect(queryOf(requests.at(-1)!)).toMatchObject({ page: '2', page_size: '2' })

  // 9. Refresh keeps the current pagination state and re-requests the same page.
  const beforeRefresh = requests.length
  await page.getByRole('button', { name: '刷新' }).click()
  await expect.poll(() => requests.length).toBeGreaterThan(beforeRefresh)
  const refreshQuery = queryOf(requests.at(-1)!)
  expect(refreshQuery).toMatchObject({ page: '2', page_size: '2' })
  expect(new URL(page.url()).searchParams.get('page')).toBe('2')
  expect(new URL(page.url()).searchParams.get('page_size')).toBe('2')
  await expect(rows.nth(0)).toContainText('capability-c')

  // Last page, then previous page restores page 2 rows.
  await page.getByRole('button', { name: '下一页' }).click()
  await expect.poll(() => new URL(page.url()).searchParams.get('page')).toBe('3')
  await expect(rows).toHaveCount(1)
  await expect(rows.nth(0)).toContainText('capability-e')
  await page.getByRole('button', { name: '上一页' }).click()
  await expect.poll(() => new URL(page.url()).searchParams.get('page')).toBe('2')
  await expect(rows).toHaveCount(2)
  await expect(rows.nth(0)).toContainText('capability-c')

  // 5. Page size change resets page to 1 and issues the matching request.
  await page.getByLabel('每页条数').selectOption('5')
  await expect.poll(() => {
    const params = new URL(page.url()).searchParams
    return [params.get('page'), params.get('page_size')]
  }).toEqual(['1', '5'])
  await expect(rows).toHaveCount(5)
  await expect.poll(() => queryOf(requests.at(-1)!)).toMatchObject({ page: '1', page_size: '5' })
  await expect(nav).toContainText('共 5 条')
  await expect(nav).toContainText('1 / 1')

  // 7/8. configured and observed stay separate; not_observed never reads as a loaded success.
  await expect(rows.nth(0).getByTestId('runtime-capability-configured')).toHaveAttribute('data-state', 'enabled')
  await expect(rows.nth(0).getByTestId('runtime-capability-observed')).toHaveAttribute('data-state', 'loaded')
  const notObserved = rows.nth(1).getByTestId('runtime-capability-observed')
  await expect(notObserved).toHaveAttribute('data-state', 'not_observed')
  await expect(notObserved).toHaveText('not_observed')
  expect(await notObserved.getAttribute('class')).not.toContain('emerald')
  await expect(rows.nth(2).getByTestId('runtime-capability-configured')).toHaveAttribute('data-state', 'disabled')
})

test('capabilities keepPreviousData keeps page 1 rows and feedback while page 2 is pending', async ({ page }) => {
  await installAuth(page)
  await installFallback(page)
  const requests: URL[] = []
  const gate = deferred()
  await installCapabilitiesRoute(page, requests, { page: 2, promise: gate.promise })

  await page.goto('/runtime/capabilities?page=1&page_size=2')
  const rows = page.getByTestId('runtime-capability-row')
  await expect(rows).toHaveCount(2)
  await expect(rows.nth(0)).toContainText('capability-a')

  const pendingRequest = page.waitForRequest(request => new URL(request.url()).searchParams.get('page') === '2')
  await page.getByRole('button', { name: '下一页' }).click()
  await pendingRequest

  // Pending page 2: previous rows stay, feedback is explicit, nothing degrades to empty/loading/error.
  await expect(rows).toHaveCount(2)
  await expect(rows.nth(0)).toContainText('capability-a')
  await expect(rows.nth(1)).toContainText('capability-b')
  const refreshing = page.locator('section[data-state-kind="loading"]')
  await expect(refreshing).toHaveCount(1)
  await expect(refreshing).toContainText('正在加载第 2 页')
  await expect(refreshing).toContainText('第 1 页数据')
  await expect(page.getByTestId('runtime-capabilities-empty')).toHaveCount(0)
  await expect(page.locator('section[data-state-kind="error"]')).toHaveCount(0)
  await expect(page.getByText('加载中…')).toHaveCount(0)
  const nav = page.locator(paginationNav)
  await expect(nav).toHaveAttribute('aria-busy', 'true')
  await expect(page.getByRole('button', { name: '下一页' })).toBeDisabled()
  await expect(nav).toContainText('共 5 条')

  gate.release()

  // After the delayed response: rows are replaced and the refreshing feedback disappears.
  await expect(rows.nth(0)).toContainText('capability-c')
  await expect(rows.nth(1)).toContainText('capability-d')
  await expect(page.locator('section[data-state-kind="loading"]')).toHaveCount(0)
  await expect(nav).not.toHaveAttribute('aria-busy', 'true')
  await expect.poll(() => new URL(page.url()).searchParams.get('page')).toBe('2')
})

test('capabilities out-of-range page is honest: no loop, no unavailable misjudgement', async ({ page }) => {
  await installAuth(page)
  await installFallback(page)
  const requests: URL[] = []
  await installCapabilitiesRoute(page, requests)

  await page.goto('/runtime/capabilities?page=99&page_size=2')
  const outOfRange = page.getByText('请求页超出范围')
  await expect(outOfRange).toBeVisible()
  await expect(page.getByTestId('runtime-capability-row')).toHaveCount(0)

  // items=[] with a valid resource envelope is an empty page, not an unavailable resource.
  const empty = page.getByTestId('runtime-capabilities-empty')
  await expect(empty).toBeVisible()
  await expect(empty).toContainText('暂无 Capability 记录')
  await expect(empty).not.toContainText('不可用')
  await expect(page.getByTestId('runtime-quality-availability')).toHaveAttribute('data-availability', 'available')

  // The backend page meta is still authoritative: total 5 while this page has no rows.
  await expect(page.locator(paginationNav)).toContainText('共 5 条')

  // No automatic re-request loop: after the network is quiet the page has issued exactly one request.
  await page.waitForLoadState('networkidle')
  expect(requests.length).toBe(1)
  expect(queryOf(requests[0])).toMatchObject({ page: '99', page_size: '2' })

  // Current product convention: the URL stays on the requested page and correction is an explicit action.
  const params = new URL(page.url()).searchParams
  expect([params.get('page'), params.get('page_size')]).toEqual(['99', '2'])

  await page.getByRole('button', { name: '返回第一页' }).click()
  await expect.poll(() => new URL(page.url()).searchParams.get('page')).toBe('1')
  await expect(page.getByTestId('runtime-capability-row')).toHaveCount(2)
  await expect(page.getByText('请求页超出范围')).toHaveCount(0)
  await page.waitForLoadState('networkidle')
  expect(requests.length).toBe(2)
})

test('capabilities surface exposes navigation only, never a business mutation control', async ({ page }) => {
  await installAuth(page)
  await installFallback(page)
  const requests: URL[] = []
  await installCapabilitiesRoute(page, requests)

  await page.goto('/runtime/capabilities?page=1&page_size=2')
  await expect(page.getByTestId('runtime-capability-row')).toHaveCount(2)

  for (const name of ['Edit', 'Enable', 'Disable', 'Delete', 'Save', 'Create', 'Switch', '编辑', '启用', '停用', '删除', '保存', '创建']) {
    await expect(page.getByRole('button', { name, exact: false })).toHaveCount(0)
  }
  await expect(page.locator('main [role="switch"], main input[type="checkbox"], main textarea')).toHaveCount(0)

  const controls = await page.locator('main button, main select').evaluateAll(elements =>
    elements.map(element => element.getAttribute('aria-label') ?? element.textContent?.trim() ?? ''))
  const allowed = /^(刷新|上一页|下一页|确定|跳转页码|每页条数)$/
  expect(controls.length).toBeGreaterThan(0)
  for (const control of controls) expect(control, `unexpected control: ${control}`).toMatch(allowed)
})
