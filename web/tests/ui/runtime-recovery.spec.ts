import { expect, test, type Page } from '@playwright/test'
import { envelope, meta, runDetailRes } from './fixtures/runtime-detail'

const detailPath = '**/api/runtime/v1/runs/run-1'
const timelinePath = '**/api/runtime/v1/runs/run-1/timeline**'
const attemptsPath = '**/api/runtime/v1/runs/run-1/attempts**'
const checkpointsPath = '**/api/runtime/v1/runs/run-1/checkpoints**'
const recoveryPath = '**/api/runtime/v1/runs/run-1/recovery'
const operationPath = '**/api/runtime/v1/operations/**'
const eventsPath = '**/api/runtime/v1/runs/run-1/events**'

async function installAuth(page: Page, role: string) {
  await page.addInitScript((value: string) => {
    localStorage.setItem('token', 'controlled-runtime-token')
    localStorage.setItem('auth-storage', JSON.stringify({ state: { token: 'controlled-runtime-token', userID: 'u-1', role: value, username: 'tester' }, version: 0 }))
  }, role)
}

const attempt = (overrides: Record<string, unknown> = {}) => ({
  attempt_id: 'attempt-2',
  run_id: 'run-1',
  attempt: 2,
  mode: 'resume',
  status: 'parked',
  current_phase: 'unknown',
  worker_id: 'worker-7',
  lease_generation: 9,
  runtime_version: 'runtime-1',
  run_compatibility_hash: 'run-hash',
  checkpoint_compatibility_hash: 'checkpoint-hash',
  executing_worker_fingerprint: 'worker-fingerprint',
  trace_id: 'trace-2',
  retry_count: 1,
  failover_count: 0,
  usage_quality: 'partial',
  trace_quality: 'reconstructed',
  started_at: '2026-09-10T00:00:00Z',
  finished_at: null,
  ...meta(),
  ...overrides,
})

const checkpoint = (overrides: Record<string, unknown> = {}) => ({
  checkpoint_id: 'ckpt-1',
  checkpoint_key: 'run-1/2',
  payload_sha256: 'abc123',
  runtime_version: 'runtime-1',
  runtime_compatibility_hash: 'checkpoint-hash',
  lease_generation: 9,
  state: 'valid',
  committed_at: '2026-09-10T00:00:03Z',
  expires_at: null,
  created_at: '2026-09-10T00:00:03Z',
  checkpoint_blob: 'OPAQUE_BYTES_SHOULD_NEVER_RENDER',
  snapshot_json: '{"secret":"never"}',
  ...meta(),
  ...overrides,
})

const operation = (overrides: Record<string, unknown> = {}) => ({
  operation_id: 'op-1',
  run_id: 'run-1',
  action: 'resume',
  status: 'accepted',
  terminal: false,
  idempotent_replay: false,
  accepted_at: '2026-09-10T00:00:10Z',
  started_at: null,
  finished_at: null,
  ...meta(),
  ...overrides,
})

async function installBaseRoutes(page: Page, runOverrides: Record<string, unknown> = {}) {
  await page.route(eventsPath, route => route.fulfill({ contentType: 'text/event-stream', body: 'data: [DONE]\n\n' }))
  await page.route(timelinePath, route => route.fulfill({ contentType: 'application/json', body: envelope({ items: [], page: { page: 1, page_size: 50, total: 0, has_next: false }, ...meta() }) }))
  await page.route(attemptsPath, route => route.fulfill({
    contentType: 'application/json',
    body: envelope({ items: [attempt()], page: { page: 1, page_size: 20, total: 1, has_next: false }, ...meta() }),
  }))
  await page.route(checkpointsPath, route => route.fulfill({
    contentType: 'application/json',
    body: envelope({ items: [checkpoint(), checkpoint({ checkpoint_id: 'ckpt-0', state: 'corrupt', reason_code: 'payload_hash_mismatch' })], page: { page: 1, page_size: 20, total: 2, has_next: false }, ...meta() }),
  }))
  await page.route(detailPath, route => route.fulfill({
    contentType: 'application/json',
    body: envelope(runDetailRes({
      summary: { status: 'retryable_failed', current_phase: 'executing', park_reason: '' },
      allowed_recovery_actions: ['resume', 'cancel'],
      ...runOverrides,
    })),
  }))
}

for (const width of [1280, 1440]) {
  test(`attempts, checkpoints and admin recovery operation at ${width}px`, async ({ page }) => {
    await installAuth(page, 'admin')
    const posts: { url: string; body: unknown }[] = []
    let operationStatus = 'accepted'
    let operationTerminal = false
    await installBaseRoutes(page)
    await page.route(recoveryPath, route => {
      posts.push({ url: route.request().url(), body: route.request().postDataJSON() })
      return route.fulfill({ status: 202, contentType: 'application/json', body: envelope({ operation: operation({ status: 'accepted', terminal: false }), ...meta() }) })
    })
    await page.route(operationPath, route => route.fulfill({
      contentType: 'application/json',
      body: envelope({ item: operation({ status: operationStatus, terminal: operationTerminal, error_code: operationTerminal ? 'worker_unavailable' : undefined }), ...meta() }),
    }))
    await page.setViewportSize({ width, height: 1000 })
    await page.goto('/runtime/runs/run-1?tab=attempts')

    // Attempt facts and opaque-byte exclusion.
    await expect(page.getByTestId('runtime-attempt-row')).toHaveCount(1, { timeout: 15000 })
    await expect(page.getByTestId('runtime-attempt-row')).toContainText('恢复')
    await expect(page.getByTestId('runtime-attempt-row')).toContainText('worker-7')
    await expect(page.getByTestId('runtime-attempt-row')).toContainText('checkpoint-hash')
    await expect(page.getByTestId('runtime-checkpoint-row')).toHaveCount(2)
    await expect(page.locator('[data-testid="runtime-checkpoint-row"][data-state="corrupt"]')).toContainText('损坏')
    await expect(page.locator('body')).not.toContainText('OPAQUE_BYTES_SHOULD_NEVER_RENDER')
    await expect(page.locator('body')).not.toContainText('snapshot_json')

    // Dialog requires reason and generation; only server-allowed actions are offered.
    await expect(page.getByTestId('runtime-recovery-button')).toHaveCount(2)
    await page.locator('[data-testid="runtime-recovery-button"][data-action="resume"]').click()
    await expect(page.getByTestId('runtime-recovery-dialog')).toHaveAttribute('data-action', 'resume')
    const submit = page.getByRole('button', { name: '提交 Resume' })
    await expect(submit).toBeDisabled()
    await page.getByLabel('期望 Generation').fill('')
    await page.getByLabel('恢复理由').fill('worker restart after node drain')
    await expect(submit).toBeDisabled()
    await page.getByLabel('期望 Generation').fill('9')
    await expect(submit).toBeEnabled()
    await submit.click()

    // 202 acknowledged with an operation id and no optimistic success.
    await expect(page.getByTestId('runtime-operation-progress')).toHaveAttribute('data-operation-id', 'op-1')
    expect(posts).toHaveLength(1)
    expect(posts[0].url).toContain('/api/runtime/v1/runs/run-1/recovery')
    expect(posts[0].body).toMatchObject({ action: 'resume', reason: 'worker restart after node drain', expected_generation: 9 })
    expect(String((posts[0].body as { idempotency_key?: string }).idempotency_key ?? '')).not.toBe('')
    await expect(page.getByTestId('runtime-operation-status')).toHaveAttribute('data-status', 'accepted')
    expect(await page.evaluate(() => sessionStorage.getItem('runtime_operation_run-1'))).toBe('op-1')

    operationStatus = 'running'
    operationTerminal = false
    await expect.poll(async () => page.getByTestId('runtime-operation-status').getAttribute('data-status'), { timeout: 10000 }).toBe('running')

    operationStatus = 'succeeded'
    operationTerminal = true
    await expect.poll(async () => page.getByTestId('runtime-operation-status').getAttribute('data-status'), { timeout: 10000 }).toBe('succeeded')
    await expect(page.getByTestId('runtime-operation-status')).toHaveAttribute('data-terminal', 'true')
    await expect(page.locator('[data-testid="runtime-status-badge"][data-status="retryable_failed"]').first()).toHaveAttribute('data-tone', 'warning')
  })

  test(`operator never receives an enabled recovery control at ${width}px`, async ({ page }) => {
    await installAuth(page, 'operator')
    await installBaseRoutes(page)
    await page.setViewportSize({ width, height: 1000 })
    await page.goto('/runtime/runs/run-1?tab=attempts')

    // operator/viewer never get an enabled Recovery control.
    await expect(page.getByTestId('runtime-recovery-button')).toHaveCount(2)
    for (const button of await page.getByTestId('runtime-recovery-button').all()) {
      await expect(button).toBeDisabled()
    }
    await expect(page.getByTestId('runtime-recovery-disabled-reason').first()).toHaveText('仅 admin 可执行')
  })

  test(`admin sees idempotent replay and a conflict is never shown as success at ${width}px`, async ({ page }) => {
    await installAuth(page, 'admin')
    await installBaseRoutes(page)
    await page.addInitScript(() => sessionStorage.setItem('runtime_operation_run-1', 'op-1'))
    await page.route(recoveryPath, route => route.fulfill({ status: 409, contentType: 'application/json', body: JSON.stringify({ message: 'RUNTIME_IDEMPOTENCY_CONFLICT', code: 409 }) }))
    await page.route(operationPath, route => route.fulfill({
      contentType: 'application/json',
      body: envelope({ item: operation({ status: 'running', idempotent_replay: true, terminal: false }), ...meta() }),
    }))
    await page.setViewportSize({ width, height: 1000 })
    await page.goto('/runtime/runs/run-1?tab=attempts')

    await expect(page.getByTestId('runtime-operation-replay')).toBeVisible()
    await expect(page.getByTestId('runtime-operation-status')).toHaveAttribute('data-status', 'running')
    await page.locator('[data-testid="runtime-recovery-button"][data-action="resume"]').click()
    await page.getByLabel('恢复理由').fill('operator escalated to admin')
    await page.getByRole('button', { name: '提交 Resume' }).click()
    await expect(page.getByTestId('runtime-recovery-error')).toContainText('RUNTIME_IDEMPOTENCY_CONFLICT')
    await expect(page.locator('[data-testid="runtime-status-badge"][data-status="retryable_failed"]').first()).toHaveAttribute('data-tone', 'warning')
  })
}

test('operation reconnects from session storage after reload and shows a terminal failure', async ({ page }) => {
  await installAuth(page, 'admin')
  await installBaseRoutes(page)
  await page.addInitScript(() => sessionStorage.setItem('runtime_operation_run-1', 'op-9'))
  await page.route(operationPath, route => route.fulfill({
    contentType: 'application/json',
    body: envelope({ item: operation({ operation_id: 'op-9', status: 'failed', terminal: true, error_code: 'worker_unavailable', correlation_seq: 42, reason: 'safe point missing' }), ...meta() }),
  }))
  await page.setViewportSize({ width: 1280, height: 1000 })
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await page.goto('/runtime/runs/run-1?tab=attempts')

  await expect(page.getByTestId('runtime-operation-progress')).toHaveAttribute('data-operation-id', 'op-9')
  await expect(page.getByTestId('runtime-operation-status')).toHaveAttribute('data-status', 'failed')
  await expect(page.getByTestId('runtime-operation-progress')).toContainText('worker_unavailable')
  await expect(page.getByTestId('runtime-operation-progress')).toContainText('42')
  // A failed operation never flips the Run to success.
  await expect(page.locator('[data-testid="runtime-status-badge"][data-status="retryable_failed"]').first()).toHaveAttribute('data-tone', 'warning')
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(1280)
})

test('operation query error exposes retry and retains last server state', async ({ page }) => {
  await installAuth(page, 'admin')
  await installBaseRoutes(page)
  await page.addInitScript(() => sessionStorage.setItem('runtime_operation_run-1', 'op-1'))
  let failing = true
  await page.route(operationPath, route => failing
    ? route.fulfill({ status: 503, json: { message: 'temporary query failure' } })
    : route.fulfill({ contentType: 'application/json', body: envelope({ item: operation({ status: 'running' }), ...meta() }) }))
  await page.goto('/runtime/runs/run-1?tab=attempts')
  await expect(page.getByTestId('runtime-operation-error')).toBeVisible()
  failing = false
  await page.getByTestId('runtime-operation-error').getByRole('button', { name: '重试' }).click()
  await expect(page.getByTestId('runtime-operation-status')).toHaveAttribute('data-status', 'running')
  failing = true
  await expect(page.getByTestId('runtime-operation-error')).toContainText('保留上次状态', { timeout: 15000 })
  await expect(page.getByTestId('runtime-operation-status')).toHaveAttribute('data-status', 'running')
})
