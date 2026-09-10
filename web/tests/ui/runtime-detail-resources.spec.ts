import { expect, test, type Page } from '@playwright/test'
import { envelope, meta, runDetailRes, timelineRes } from './fixtures/runtime-detail'

const detailPath = '**/api/runtime/v1/runs/run-1'
const timelinePath = '**/api/runtime/v1/runs/run-1/timeline**'
const effectsPath = '**/api/runtime/v1/runs/run-1/effects**'
const evidencePath = '**/api/runtime/v1/runs/run-1/evidence**'
const contextPath = '**/api/runtime/v1/runs/run-1/context**'
const tracesPath = '**/api/runtime/v1/runs/run-1/traces**'
const resolvePath = '**/ops/v1/effects/**'

async function installAuth(page: Page, role: string) {
  await page.addInitScript((value: string) => {
    localStorage.setItem('token', 'controlled-runtime-token')
    localStorage.setItem('auth-storage', JSON.stringify({ state: { token: 'controlled-runtime-token', userID: 'u-1', role: value, username: 'tester' }, version: 0 }))
  }, role)
}

const effect = (overrides: Record<string, unknown> = {}) => ({
  id: 'effect-1',
  run_id: 'run-1',
  effect_role: 'primary',
  effect_step: 'notify-owner',
  parent_effect_id: '',
  idempotency_key_digest: 'idem-digest',
  proposal_hash: 'proposal-hash',
  tool_name: 'send_notification',
  tool_revision: 'rev-3',
  tool_schema_hash: 'schema-hash',
  target_hash: 'target-hash',
  effect_type: 'external_write',
  status: 'unknown',
  version: 2,
  external_reference: '',
  lease_generation: 9,
  attempt: 2,
  reconciliation_attempts: 1,
  resolution: '',
  resolved_by: '',
  created_at: '2026-09-10T00:00:00Z',
  updated_at: null,
  history: [
    { seq: 11, event_type: 'effect.proposed', status: 'pending', actor_id: 'planner', reason: 'plan step', created_at: '2026-09-10T00:00:01Z' },
    { seq: 12, event_type: 'effect.unknown', status: 'unknown', actor_id: 'worker-7', reason: 'no ack', created_at: '2026-09-10T00:00:02Z' },
  ],
  ...meta(),
  ...overrides,
})

const evidence = (overrides: Record<string, unknown> = {}) => ({
  evidence_id: 'ev-1',
  source_type: 'rag_document',
  source_id: 'doc-1',
  base_id: 'base-1',
  document_id: 'doc-1',
  chunk_id: 'chunk-3',
  source_version: 'v2',
  content_hash: 'content-hash',
  access_scope: 'scope-a',
  indexed_version: 'idx-7',
  retrieved_at: '2026-09-10T00:00:00Z',
  vector_score: 0.82,
  rerank_score: 0.71,
  answer_references: ['answer-1'],
  quote_available: true,
  ...meta(),
  ...overrides,
})

const trace = (overrides: Record<string, unknown> = {}) => ({
  trace_id: 'trace-1',
  attempt: 2,
  status: 'completed',
  trace_quality: 'complete',
  duration_ms: 1200,
  input_tokens: 100,
  output_tokens: 50,
  cost_cny: 0.12,
  node_count: 5,
  raw_available: true,
  detail_url: '/traces/trace-1',
  ...meta(),
  ...overrides,
})

const contextItem = (overrides: Record<string, unknown> = {}) => ({
  identity: { user_id: 'u-1', username: 'operator', role: 'admin', scope: 'global' },
  session_revision_used: 4,
  session_revision_committed: 3,
  summary_hash: 'summary-hash',
  history_count: 2,
  budget_limits_hash: 'budget-hash',
  deadline_at: '2026-09-10T01:00:00Z',
  runtime_version: 'runtime-1',
  runtime_compatibility_hash: 'run-hash',
  policy_hash: 'policy-hash',
  config_hash: 'config-hash',
  gate_keys: ['l1_write'],
  ...meta(),
  ...overrides,
})

async function installBaseRoutes(page: Page, payloads: Record<string, unknown> = {}) {
  await page.route(timelinePath, route => route.fulfill({ contentType: 'application/json', body: envelope(timelineRes([])) }))
  await page.route(detailPath, route => route.fulfill({ contentType: 'application/json', body: envelope(runDetailRes()) }))
  await page.route(effectsPath, route => route.fulfill({
    contentType: 'application/json',
    body: envelope(payloads.effects ?? {
      items: [effect(), effect({ id: 'effect-2', effect_role: 'derived', effect_step: 'update-ticket', parent_effect_id: 'effect-1', status: 'succeeded', history: [] })],
      page: { page: 1, page_size: 50, total: 2, has_next: false },
      ...meta(),
    }),
  }))
  await page.route(evidencePath, route => route.fulfill({
    contentType: 'application/json',
    body: envelope(payloads.evidenceList ?? { items: [evidence()], page: { page: 1, page_size: 20, total: 1, has_next: false }, ...meta() }),
  }))
  await page.route(tracesPath, route => route.fulfill({
    contentType: 'application/json',
    body: envelope(payloads.traces ?? { items: [trace()], page: { page: 1, page_size: 50, total: 1, has_next: false }, ...meta() }),
  }))
  await page.route(contextPath, route => {
    const url = route.request().url()
    if (url.includes('include=history')) {
      return route.fulfill({
        contentType: 'application/json',
        body: envelope({ item: contextItem({ history: [{ role: 'user', content: 'inspect the incident' }, { role: 'assistant', content: 'plan retained' }], history_truncated: false, redaction_applied: true }), ...meta() }),
      })
    }
    return route.fulfill({ contentType: 'application/json', body: envelope({ item: contextItem(), ...meta() }) })
  })
}

for (const width of [1280, 1440]) {
  test(`effects, evidence, context and trace tabs at ${width}px`, async ({ page }) => {
    await installAuth(page, 'admin')
    await installBaseRoutes(page)
    const requests: string[] = []
    page.on('request', request => requests.push(request.url()))
    let resolveCalls = 0
    await page.route(resolvePath, route => {
      resolveCalls += 1
      return route.fulfill({ contentType: 'application/json', body: JSON.stringify({ message: 'OK', data: {} }) })
    })
    await page.setViewportSize({ width, height: 1000 })
    await page.goto('/runtime/runs/run-1?tab=effects')

    await expect(page.getByTestId('runtime-effect-row')).toHaveCount(2)
    await expect(page.locator('[data-testid="runtime-effect-row"][data-status="unknown"]')).toContainText('unknown')
    await expect(page.getByTestId('runtime-effect-history').first()).toContainText('effect.unknown')
    await expect(page.getByTestId('runtime-effects-panel')).toContainText('idem-digest')

    // Controlled reconciliation: submits to the canonical ops path; the row stays unknown until the server refreshes.
    await page.getByRole('button', { name: /受控对账/ }).click()
    await page.getByLabel('对账理由').fill('provider ack received out of band')
    await page.getByRole('button', { name: '提交对账' }).click()
    await expect.poll(() => resolveCalls).toBe(1)
    await expect(page.getByTestId('runtime-effect-notice')).toBeVisible()
    await expect(page.locator('[data-testid="runtime-effect-row"][data-status="unknown"]')).toHaveCount(1)

    // Evidence: metadata by default, quote only after explicit expansion.
    await page.getByTestId('runtime-detail-tab').filter({ hasText: 'Evidence' }).click()
    await expect(page.getByTestId('runtime-evidence-row')).toHaveCount(1)
    await expect(page.getByTestId('runtime-evidence-quote')).toHaveCount(0)
    await expect(page.getByTestId('runtime-evidence-panel')).toContainText('content-hash')
    await page.getByRole('button', { name: /展开引用/ }).click()
    await expect.poll(() => requests.some(url => url.includes('/evidence/ev-1') && url.includes('include=quote'))).toBe(true)
    await expect(page.getByTestId('runtime-evidence-quote')).toBeVisible()

    // Context: metadata first, history only through the explicit include query.
    await page.getByTestId('runtime-detail-tab').filter({ hasText: 'Context' }).click()
    await expect(page.getByTestId('runtime-context-panel')).toContainText('summary-hash')
    await expect(page.getByTestId('runtime-context-history')).toHaveCount(0)
    await page.getByRole('button', { name: /展开历史/ }).click()
    await expect.poll(() => requests.some(url => url.includes('/context') && url.includes('include=history'))).toBe(true)
    await expect(page.getByTestId('runtime-context-history-item')).toHaveCount(2)

    // Trace: raw link only when available.
    await page.getByTestId('runtime-detail-tab').filter({ hasText: 'Trace' }).click()
    await expect(page.getByTestId('runtime-trace-row')).toHaveCount(1)
    await expect(page.getByTestId('runtime-trace-link')).toHaveAttribute('href', '/traces/trace-1')
    await expect(page.getByTestId('runtime-trace-unavailable')).toHaveCount(0)

    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(width)
  })
}

test('non-admin sees no controlled reconciliation and unavailable raw trace is labelled', async ({ page }) => {
  await installAuth(page, 'viewer')
  await installBaseRoutes(page, {
    evidenceList: { items: [evidence({ evidence_id: 'ev-2', quote_available: false })], page: { page: 1, page_size: 20, total: 1, has_next: false }, ...meta() },
    traces: { items: [trace({ raw_available: false, detail_url: '', reason_code: 'trace_nodes_missing', trace_quality: 'partial', ...meta({ availability: 'partial', data_quality: 'partial' }) })], page: { page: 1, page_size: 50, total: 1, has_next: false }, ...meta() },
  })
  await page.setViewportSize({ width: 1280, height: 1000 })

  await page.goto('/runtime/runs/run-1?tab=effects')
  await expect(page.getByTestId('runtime-effects-panel')).toBeVisible({ timeout: 15000 })
  await expect(page.getByTestId('runtime-effect-row')).toHaveCount(2)
  await expect(page.getByTestId('runtime-effect-reconcile-readonly')).toContainText('仅 admin')
  await expect(page.getByTestId('runtime-effect-reconcile')).toHaveCount(0)

  await page.getByTestId('runtime-detail-tab').filter({ hasText: 'Evidence' }).click()
  await expect(page.getByRole('button', { name: /展开引用/ })).toHaveCount(0)
  await expect(page.getByTestId('runtime-evidence-row')).toContainText('否')

  await page.getByTestId('runtime-detail-tab').filter({ hasText: 'Trace' }).click()
  await expect(page.getByTestId('runtime-trace-unavailable')).toContainText('trace_nodes_missing')
  await expect(page.getByTestId('runtime-trace-link')).toHaveCount(0)
})

test('panel-level unavailable and partial states stay distinct', async ({ page }) => {
  await installAuth(page, 'admin')
  await installBaseRoutes(page, {
    effects: { items: [], page: { page: 1, page_size: 50, total: 0, has_next: false }, availability: 'unavailable', data_quality: 'unknown', reason_code: 'not_observed', not_run: true },
    traces: { items: [trace({ trace_quality: 'partial' })], page: { page: 1, page_size: 50, total: 1, has_next: false }, availability: 'partial', data_quality: 'partial', reason_code: 'trace_nodes_missing' },
  })
  await page.setViewportSize({ width: 1280, height: 1000 })
  await page.goto('/runtime/runs/run-1?tab=effects')
  await expect(page.getByTestId('runtime-effects-empty')).toContainText('不可用')
  await expect(page.getByTestId('runtime-quality-reason').first()).toContainText('not_observed')
  await expect(page.getByTestId('runtime-quality-not-run').first()).toBeVisible()

  await page.getByTestId('runtime-detail-tab').filter({ hasText: 'Trace' }).click()
  await expect(page.getByTestId('runtime-trace-row')).toHaveCount(1)
  await expect(page.getByTestId('runtime-trace-panel').getByTestId('runtime-quality-availability').first()).toHaveAttribute('data-availability', 'partial')
})
