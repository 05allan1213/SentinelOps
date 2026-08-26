import { expect, test } from '@playwright/test'
import {
  acceptUnknownWithRetry,
  createDurableRun,
  eventAttributes,
  loginAdmin,
  loginRequester,
  readSSEUntilTerminal,
  signViewerToken,
  waitForPendingApproval,
  waitForUnknownEffect,
} from './p38-helpers'

test('unknown external Effect remains parked until admin accepts the evidence, then reaches a canceled terminal Run', async ({ page }) => {
  const admin = await loginAdmin(page)
  const requester = await loginRequester(page, admin.token)
  const run = await createDurableRun(page, requester, '请执行一次 unknown 外部 Effect')
  const approval = await waitForPendingApproval(page.request, admin.token, run.run_id)

  await page.goto('/dashboard')
  const approvalCard = page.getByTestId(`approval-${approval.id}`)
  await expect(approvalCard).toBeVisible({ timeout: 90_000 })
  await approvalCard.getByRole('button', { name: '批准' }).click()
  await page.getByLabel('审批理由').fill('P38 unknown fixture approval')
  await page.getByRole('button', { name: '确认批准' }).click()
  await expect(approvalCard).toHaveCount(0, { timeout: 90_000 })

  const unknown = await waitForUnknownEffect(page.request, admin.token, run.run_id)
  await page.reload()
  const effectCard = page.getByTestId(`effect-${unknown.id}`)
  await expect(effectCard).toBeVisible({ timeout: 30_000 })
  await expect(effectCard).toContainText('结果未知')

  const viewer = await page.request.post(`/api/ops/v1/effects/${unknown.id}/accept-unknown`, {
    headers: { Authorization: `Bearer ${signViewerToken()}` },
    data: { run_id: run.run_id, reason: 'viewer must fail', evidence: { source: 'viewer' } },
  })
  expect(viewer.status()).toBe(403)

  // The reconciliation worker may briefly own the Effect. Retry only the
  // documented CAS conflict until the unknown row is available again.
  await acceptUnknownWithRetry(page.request, admin.token, unknown.id, run.run_id)

  const events = await readSSEUntilTerminal(page.request, admin.token, run.run_id)
  const types = events.map(event => event.type)
  expect(types).toContain('effect.unknown')
  expect(types).toContain('run.parked')
  expect(types.filter(type => type === 'effect.started')).toHaveLength(1)
  expect(types.filter(type => type === 'effect.succeeded')).toHaveLength(0)
  expect(types.filter(type => type === 'approval.decided')).toHaveLength(1)
  expect(types.filter(type => type === 'run.resumed')).toHaveLength(1)

  const accepted = events.find(event => event.type === 'effect.resolved' && eventAttributes(event).resolution === 'accepted_unknown')
  expect(accepted).toBeTruthy()
  const terminal = [...events].reverse().find(event => event.type === 'run.failed')
  expect(terminal).toBeTruthy()
  expect(eventAttributes(terminal!).to_status).toBe('canceled')

  await page.reload()
  // Accepted unknown is terminally removed from the admin queue; the event
  // ledger above, rather than a green UI card, is the durable truth.
  await expect(page.getByTestId(`effect-${unknown.id}`)).toHaveCount(0)
})
