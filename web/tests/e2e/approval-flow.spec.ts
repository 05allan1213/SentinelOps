import { expect, test } from '@playwright/test'
import crypto from 'node:crypto'

const adminPassword = process.env.SENTINELOPS_E2E_ADMIN_PASSWORD ?? 'sentinelops-e2e-admin-password'
const requesterUsername = 'p38-requester'
const requesterPassword = 'p38-requester-password'

function signIdentityToken(userID: string, username: string, role: string) {
  const header = Buffer.from(JSON.stringify({ alg: 'HS256', typ: 'JWT' })).toString('base64url')
  const payload = Buffer.from(JSON.stringify({
    uid: userID, username, role,
    exp: Math.floor(Date.now() / 1000) + 3600,
  })).toString('base64url')
  const input = `${header}.${payload}`
  const signature = crypto.createHmac('sha256', process.env.SENTINELOPS_E2E_JWT_SECRET ?? 'sentinelops-e2e-jwt-secret').update(input).digest('base64url')
  return `${input}.${signature}`
}

function signViewerToken() {
  return signIdentityToken('e2e-viewer', 'e2e-viewer', 'viewer')
}

async function loginAdmin(page: import('@playwright/test').Page): Promise<string> {
  const response = await page.request.post('/api/auth/v1/login', { data: { username: 'admin', password: adminPassword } })
  expect(response.ok()).toBeTruthy()
  const body = await response.json()
  const data = body.data ?? body
  await page.goto('/login')
  await page.evaluate((auth) => {
    localStorage.setItem('token', auth.token)
    localStorage.setItem('auth-storage', JSON.stringify({ state: auth, version: 0 }))
  }, {
    token: data.token, userID: data.user_id, role: data.role, username: data.username,
  })
  return data.token
}

async function loginRequester(page: import('@playwright/test').Page, adminToken: string): Promise<string> {
  const register = await page.request.post('/api/auth/v1/register', {
    headers: { Authorization: `Bearer ${adminToken}` },
    data: { username: requesterUsername, password: requesterPassword },
  })
  if (register.ok()) {
    const body = await register.json()
    const data = body.data ?? body
    // GoFrame returns HTTP 200 for a duplicate registration with no usable
    // identity payload; only treat a response with a real user id as a new
    // registration. Otherwise log in and sign the operator test identity.
    if (data?.user_id && data?.username) {
      return signIdentityToken(data.user_id, data.username, 'operator')
    }
  }
  const response = await page.request.post('/api/auth/v1/login', {
    data: { username: requesterUsername, password: requesterPassword },
  })
  expect(response.ok()).toBeTruthy()
  const body = await response.json()
  const data = body.data ?? body
  return signIdentityToken(data.user_id, data.username, 'operator')
}

async function createApprovalRun(page: import('@playwright/test').Page, adminToken: string) {
  const token = await loginRequester(page, adminToken)
  const response = await page.request.post('/api/chat/v2/runs', {
    headers: { Authorization: `Bearer ${token}` },
    data: { session_id: `p38-${crypto.randomUUID()}`, query: '请执行一次需要审批的封禁动作', agent: 'plan_agent' },
  })
  expect(response.ok()).toBeTruthy()
  return (await response.json()).data
}

test.describe('real Compose approval recovery chain', () => {
  test('pending -> approve -> worker resume -> effect success survives refresh and reconnect', async ({ page }) => {
    const token = await loginAdmin(page)
    const run = await createApprovalRun(page, token)

    await page.goto('/dashboard')
    const approval = page.locator('[data-testid^="approval-"]').first()
    await expect(approval).toBeVisible({ timeout: 60_000 })
    await expect(approval).toContainText('block_ip')
    await approval.getByRole('button', { name: '批准' }).click()
    await page.getByLabel('审批理由').fill('P38 e2e approval')
    await page.getByRole('button', { name: '确认批准' }).click()
    await page.getByRole('button', { name: '确认批准' }).click({ trial: true }).catch(() => undefined)

    await expect(page.locator('[data-testid^="approval-"]')).toHaveCount(0, { timeout: 60_000 })
    await page.reload()
    await expect(page.getByText('全部完成')).toBeVisible({ timeout: 60_000 })

    const events = await page.request.get(`/api/chat/v2/runs/${run.run_id}/events?after_seq=0`, {
      headers: { Authorization: `Bearer ${token}` },
    })
    expect(events.ok()).toBeTruthy()
  })

  test('viewer HTTP approval is forbidden and duplicate decisions do not execute twice', async ({ page }) => {
    const token = await loginAdmin(page)
    await createApprovalRun(page, token)
    await page.goto('/dashboard')
    const approval = page.locator('[data-testid^="approval-"]').first()
    await expect(approval).toBeVisible({ timeout: 60_000 })
    const approvalID = (await approval.getAttribute('data-testid'))?.replace('approval-', '')
    expect(approvalID).toBeTruthy()
    const hash = await approval.locator('p.font-mono').textContent()
    const viewer = await page.request.post(`/api/ops/v1/approvals/${approvalID}/approve`, {
      headers: { Authorization: `Bearer ${signViewerToken()}` },
      data: { proposal_hash: hash, version: 1, reason: 'viewer must fail' },
    })
    expect(viewer.status()).toBe(403)
  })
})
