import { expect, test } from '@playwright/test'
import {
  createDurableRun,
  eventAttributes,
  loginAdmin,
  loginRequester,
  readSSESegment,
  readSSEUntilTerminal,
  signViewerToken,
  waitForPendingApproval,
} from './p38-helpers'

test.describe('real Compose approval recovery chain', () => {
  test('pending -> approve -> worker resume -> primary/derived effects succeed and SSE replay is read-only', async ({ page }) => {
    const admin = await loginAdmin(page)
    const requester = await loginRequester(page, admin.token)
    const run = await createDurableRun(page, requester, '请执行一次需要审批的封禁动作')
    const approval = await waitForPendingApproval(page.request, admin.token, run.run_id)

    await page.goto('/dashboard')
    const card = page.getByTestId(`approval-${approval.id}`)
    await expect(card).toBeVisible({ timeout: 90_000 })
    await expect(card).toContainText('block_ip')
    await card.getByRole('button', { name: '批准' }).click()
    await page.getByLabel('审批理由').fill('P38 e2e approval')
    await page.getByRole('button', { name: '确认批准' }).click()
    await expect(card).toHaveCount(0, { timeout: 90_000 })

    const events = await readSSESegment(page.request, admin.token, run.run_id, 0)
    const types = events.map(event => event.type)
    expect(types.filter(type => type === 'approval.decided')).toHaveLength(1)
    expect(types.filter(type => type === 'run.resumed')).toHaveLength(1)
    expect(types.filter(type => type === 'effect.started')).toHaveLength(2)
    expect(types.filter(type => type === 'effect.succeeded')).toHaveLength(2)
    expect(types).toContain('run.completed')

    const succeededSteps = events
      .filter(event => event.type === 'effect.succeeded')
      .map(event => eventAttributes(event).effect_step)
    expect(succeededSteps).toEqual(expect.arrayContaining(['primary', 'nginx_reload']))
    expect(new Set(succeededSteps).size).toBe(2)

    const terminal = events.find(event => event.type === 'run.completed')
    expect(terminal).toBeTruthy()
    expect(eventAttributes(terminal!).to_status).toBe('succeeded')

    // Reconnect from the terminal boundary. It must replay only the terminal
    // event and cannot create another model call or Effect row.
    const replay = await readSSESegment(page.request, admin.token, run.run_id, terminal!.id - 1)
    expect(replay.map(event => event.id)).toEqual([terminal!.id])
    expect(replay.some(event => event.type === 'agent.tool_call' || event.type === 'effect.started')).toBe(false)

    await page.reload()
    await expect(page.getByTestId(`approval-${approval.id}`)).toHaveCount(0)
  })

  test('viewer HTTP approval is forbidden and duplicate CAS decision never adds a second event', async ({ page }) => {
    const admin = await loginAdmin(page)
    const requester = await loginRequester(page, admin.token)
    const run = await createDurableRun(page, requester, '请执行一次需要审批的封禁动作')
    const approval = await waitForPendingApproval(page.request, admin.token, run.run_id)
    const viewer = await page.request.post(`/api/ops/v1/approvals/${approval.id}/approve`, {
      headers: { Authorization: `Bearer ${signViewerToken()}` },
      data: { proposal_hash: approval.proposal_hash, version: approval.version, reason: 'viewer must fail' },
    })
    expect(viewer.status()).toBe(403)

    const decided = await page.request.post(`/api/ops/v1/approvals/${approval.id}/approve`, {
      headers: { Authorization: `Bearer ${admin.token}` },
      data: { proposal_hash: approval.proposal_hash, version: approval.version, reason: 'P38 CAS decision' },
    })
    expect(decided.ok()).toBeTruthy()
    const duplicate = await page.request.post(`/api/ops/v1/approvals/${approval.id}/approve`, {
      headers: { Authorization: `Bearer ${admin.token}` },
      data: { proposal_hash: approval.proposal_hash, version: approval.version, reason: 'duplicate must conflict' },
    })
    expect(duplicate.ok()).toBeTruthy()
    const conflictingDecision = await page.request.post(`/api/ops/v1/approvals/${approval.id}/reject`, {
      headers: { Authorization: `Bearer ${admin.token}` },
      data: { proposal_hash: approval.proposal_hash, version: approval.version, reason: 'opposite decision must conflict' },
    })
    expect(conflictingDecision.status()).toBe(409)

    const events = await readSSESegment(page.request, admin.token, run.run_id, 0)
    expect(events.filter(event => event.type === 'approval.decided')).toHaveLength(1)
  })

  test('proposal hash mismatch is rejected and an explicit reject never starts an Effect', async ({ page }) => {
    const admin = await loginAdmin(page)
    const requester = await loginRequester(page, admin.token)
    const run = await createDurableRun(page, requester, '请执行一次需要审批的封禁动作')
    const approval = await waitForPendingApproval(page.request, admin.token, run.run_id)

    const mismatched = await page.request.post(`/api/ops/v1/approvals/${approval.id}/approve`, {
      headers: { Authorization: `Bearer ${admin.token}` },
      data: {
        proposal_hash: '0000000000000000000000000000000000000000000000000000000000000000',
        version: approval.version,
        reason: 'hash mismatch must fail closed',
      },
    })
    expect(mismatched.status()).toBe(409)

    const rejected = await page.request.post(`/api/ops/v1/approvals/${approval.id}/reject`, {
      headers: { Authorization: `Bearer ${admin.token}` },
      data: { proposal_hash: approval.proposal_hash, version: approval.version, reason: 'P38 explicit rejection' },
    })
    expect(rejected.ok()).toBeTruthy()

    const events = await readSSEUntilTerminal(page.request, admin.token, run.run_id)
    const types = events.map(event => event.type)
    expect(types.filter(type => type === 'approval.decided')).toHaveLength(1)
    expect(types.filter(type => type === 'effect.started')).toHaveLength(0)
    expect(types).toContain('run.failed')
    expect(eventAttributes(events.find(event => event.type === 'approval.decided')!).decision).toBe('rejected')
  })
})
