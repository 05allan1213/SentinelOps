import { expect, test, type Page } from '@playwright/test'

const approval = {
  id: 'approval-1',
  run_id: 'run-1',
  tool_name: 'block_ip',
  tool_revision: 'v2',
  risk_level: 'high',
  proposal: { target: '10.0.0.8', params: { reason: 'credential abuse' } },
  proposal_hash: 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
  requested_by: 'planner',
  status: 'pending',
  version: 3,
  published_at: '2026-08-25T09:00:00Z',
  expires_at: '2026-08-25T10:00:00Z',
}

const unknownEffect = {
  id: 'effect-1',
  run_id: 'run-1',
  tool_name: 'webhook_out',
  effect_step: 'notify',
  effect_type: 'external',
  status: 'unknown',
  version: 2,
  reconciliation_attempts: 1,
}

async function installFixture(page: Page, role = 'admin') {
  await page.addInitScript(({ currentRole }) => {
    localStorage.setItem('token', 'phase37-ui-token')
    localStorage.setItem('auth-storage', JSON.stringify({ state: { token: 'phase37-ui-token', userID: 'user-1', role: currentRole, username: 'phase37' }, version: 0 }))
  }, { currentRole: role })

  await page.route('**/api/**', async route => {
    await route.fulfill({ json: { code: 0, message: 'OK', data: {} } })
  })
  await page.route('**/api/ops/v1/approvals*', async route => {
    if (route.request().method() === 'GET') {
      await route.fulfill({ json: { code: 0, message: 'OK', data: { items: [approval, { ...approval, id: 'preparing', status: 'preparing' }] } } })
      return
    }
    await route.fulfill({ json: { code: 0, message: 'OK', data: { item: { ...approval, status: 'approved' } } } })
  })
  await page.route('**/api/ops/v1/effects/unknown*', async route => {
    await route.fulfill({ json: { code: 0, message: 'OK', data: { items: [unknownEffect] } } })
  })
  await page.route('**/api/ops/v1/runs*', async route => {
    await route.fulfill({ json: { code: 0, message: 'OK', data: { items: [{ id: 'run-1', event_id: 'evt-1', event_title: 'Suspicious login', status: 'parked', duration_ms: 0, started_at: '2026-08-25T09:00:00Z' }] } } })
  })
  await page.route('**/api/ops/v1/stats', async route => {
    await route.fulfill({ json: { code: 0, message: 'OK', data: { total_runs: 1, success_runs: 0, failed_runs: 0 } } })
  })
}

test('pending approval exposes redacted facts, hides preparing, and requires a reason', async ({ page }) => {
  await installFixture(page)
  await page.goto('/ops')

  await expect(page.getByText('block_ip', { exact: true })).toBeVisible()
  await expect(page.getByText('10.0.0.8', { exact: true })).toBeVisible()
  await expect(page.getByText(approval.proposal_hash, { exact: true })).toBeVisible()
  await expect(page.getByText('preparing', { exact: true })).toHaveCount(0)

  await page.getByRole('button', { name: '批准' }).click()
  await expect(page.getByRole('button', { name: '确认批准' })).toBeDisabled()
  await page.getByLabel('审批理由').fill('已核实并授权')
  await expect(page.getByRole('button', { name: '确认批准' })).toBeEnabled()
})

test('viewer has no decision entry and parked run is not shown as success', async ({ page }) => {
  await installFixture(page, 'viewer')
  await page.goto('/ops')

  await expect(page.getByRole('button', { name: '批准' })).toHaveCount(0)
  await expect(page.getByRole('button', { name: '拒绝' })).toHaveCount(0)
  await expect(page.getByText('已暂停：外部 Effect 结果未知', { exact: true })).toBeVisible()
  await expect(page.getByTestId('runs-panel').getByText('成功', { exact: true })).toHaveCount(0)
})

test('admin unknown resolution submits a decision only', async ({ page }) => {
  await installFixture(page)
  const requests: string[] = []
  await page.on('request', request => {
    if (request.method() === 'POST') requests.push(request.url())
  })
  await page.goto('/ops')

  await expect(page.getByText('webhook_out', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: '仍然未知' }).click()
  await page.getByLabel('对账证据').fill('供应商没有查询接口')
  await page.getByRole('button', { name: '提交对账决定' }).click()
  await expect.poll(() => requests.some(url => url.includes('/effects/effect-1/resolve'))).toBe(true)
  expect(requests.some(url => url.includes('/runs/direct'))).toBe(false)
})

test('409 already-decided is stable and double click sends one decision', async ({ page }) => {
  await installFixture(page)
  let postCount = 0
  await page.route('**/api/ops/v1/approvals/approval-1/approve', async route => {
    postCount += 1
    await route.fulfill({ status: 409, json: { code: 409, message: 'approval already decided', data: null } })
  })
  await page.goto('/ops')
  await page.getByRole('button', { name: '批准' }).click()
  await page.getByLabel('审批理由').fill('已核实')
  await page.getByRole('button', { name: '确认批准' }).dblclick()
  await expect(page.getByText('审批已被其他人处理，未执行重复决定', { exact: true })).toBeVisible()
  expect(postCount).toBe(1)
})

test('preview surfaces never claim an effect was applied', async ({ page }) => {
  await installFixture(page)
  await page.route('**/api/event/v1/list**', route => route.fulfill({ json: { data: { total: 1, events: [] } } }))
  await page.goto('/events/analysis')
  await expect(page.getByText('Proposal Preview', { exact: true }).first()).toBeVisible()
  await expect(page.getByText('规则已应用', { exact: true })).toHaveCount(0)
})

test('failed polling does not render an all-clear success state', async ({ page }) => {
  await installFixture(page)
  await page.route('**/api/ops/v1/approvals*', async route => {
    await route.fulfill({ status: 503, json: { code: 503, message: 'temporarily unavailable', data: null } })
  })
  await page.goto('/ops')
  await expect(page.getByText('待办状态暂不可用，未确认成功或已完成。', { exact: true })).toBeVisible()
  await expect(page.getByText('全部完成', { exact: true })).toHaveCount(0)
})
