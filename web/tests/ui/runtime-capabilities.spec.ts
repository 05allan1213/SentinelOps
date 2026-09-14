import { expect, test, type Page } from '@playwright/test'

const envelope = (data: unknown) => JSON.stringify({ message: 'OK', data })

// Read-only surfaces may expose refresh + pagination navigation, never a business mutation affordance.
const MUTATION_NAMES = ['Edit', 'Enable', 'Disable', 'Delete', 'Save', 'Create', 'Switch', '编辑', '启用', '停用', '删除', '保存', '创建']
const NAVIGATION_CONTROL = /^(刷新|上一页|下一页|确定|跳转页码|每页条数)$/

async function expectReadOnlySurface(page: Page) {
  for (const name of MUTATION_NAMES) {
    await expect(page.getByRole('button', { name, exact: false })).toHaveCount(0)
  }
  await expect(page.locator('main [role="switch"], main input[type="checkbox"], main textarea')).toHaveCount(0)
  const controls = await page.locator('main button, main select').evaluateAll(elements =>
    elements.map(element => element.getAttribute('aria-label') ?? element.textContent?.trim() ?? ''))
  expect(controls.length).toBeGreaterThan(0)
  for (const control of controls) expect(control, `unexpected runtime control: ${control}`).toMatch(NAVIGATION_CONTROL)
}

async function installAuth(page: Page) {
  await page.addInitScript(() => localStorage.setItem('token', 'controlled-runtime-token'))
}

async function installRoutes(page: Page) {
  await page.route('**/api/runtime/v1/runs**', route => route.fulfill({
    contentType: 'application/json',
    body: envelope({ items: [], page: { page: 1, page_size: 20, total: 0, has_next: false }, availability: 'available', data_quality: 'complete' }),
  }))
  await page.route('**/api/runtime/v1/capabilities**', route => route.fulfill({
    contentType: 'application/json',
    body: envelope({
      items: [
        { name: 'send_notification', source: 'local_tool', risk: 'medium', revision: 'rev-3', schema_hash: 'schema-abc', effect_type: 'external_write', required_gate: 'l1_write', required_role: 'admin', allowed_agents: ['executor'], configured_state: 'enabled', observed_worker_state: 'loaded', last_observed_at: '2026-09-10T00:00:00Z', availability: 'available', data_quality: 'complete' },
        { name: 'mcp://vendor/scan', source: 'mcp', risk: 'high', revision: 'r7', schema_hash: 'schema-def', effect_type: 'read', required_gate: 'l2_write', required_role: 'admin', allowed_agents: ['executor', 'planner'], configured_state: 'enabled', observed_worker_state: 'not_observed', availability: 'unavailable', data_quality: 'unknown', reason_code: 'not_observed', not_run: true },
      ],
      page: { page: 1, page_size: 100, total: 2, has_next: false },
      availability: 'partial',
      data_quality: 'partial',
      reason_code: 'worker_observation_missing',
    }),
  }))
  await page.route('**/api/runtime/v1/safety', route => route.fulfill({
    contentType: 'application/json',
    body: envelope({
      item: {
        static_caps: ['l1_write', 'l2_write'],
        dynamic_caps: ['l1_write'],
        current_effective: [],
        shadow_mode: true,
        policy_hash: 'policy-hash',
        catalog_revision: 'catalog-9',
        gate_audit_available: false,
        availability: 'partial',
        data_quality: 'partial',
        reason_code: 'gate_audit_unavailable',
      },
      availability: 'partial',
      data_quality: 'partial',
    }),
  }))
  await page.route('**/api/runtime/v1/worker-health**', route => route.fulfill({
    contentType: 'application/json',
    body: envelope({
      items: [
        { worker_id: 'worker-7', status: 'active', heartbeat_at: '2026-09-10T00:00:05Z', runtime_version: 'runtime-1', runtime_compatibility_hash: 'hash-7', active_run_id: 'run-1', active_generation: 9, observed_mcp_count: 2, observed_skill_count: 1, last_error: '', availability: 'available', data_quality: 'complete' },
        { worker_id: 'worker-8', status: 'stale', heartbeat_at: '2026-09-09T20:00:00Z', runtime_version: 'runtime-1', runtime_compatibility_hash: 'hash-8', active_run_id: '', active_generation: 0, observed_mcp_count: 0, observed_skill_count: 0, last_error: 'heartbeat expired', availability: 'available', data_quality: 'complete' },
      ],
      page: { page: 1, page_size: 50, total: 2, has_next: false },
      aggregate: { total: 2, active: 1, idle: null, stale: 1, availability: 'partial', data_quality: 'partial' },
      availability: 'available',
      data_quality: 'complete',
    }),
  }))
  await page.route('**/api/runtime/v1/eval**', route => route.fulfill({
    contentType: 'application/json',
    body: envelope({
      item: { suite: 'agent_runtime', case_count: 0, passed: 0, failed: 0, availability: 'unavailable', data_quality: 'unknown', reason_code: 'not_observed', not_run: true },
      availability: 'unavailable',
      data_quality: 'unknown',
      reason_code: 'not_observed',
      not_run: true,
    }),
  }))
  await page.route('**/api/runtime/v1/release', route => route.fulfill({
    contentType: 'application/json',
    body: envelope({
      item: { runtime_version: 'runtime-1', observed_worker_versions: ['runtime-1'], gray_state: null, rollback_state: null, availability: 'unavailable', data_quality: 'unknown', reason_code: 'not_observed' },
      availability: 'unavailable',
      data_quality: 'unknown',
      reason_code: 'not_observed',
      not_run: true,
    }),
  }))
  await page.route('**/api/runtime/v1/retention', route => route.fulfill({
    contentType: 'application/json',
    body: envelope({
      item: { payload_days: 30, audit_days: 180, policy_valid: true, last_cleanup: null, protected_active_count: 2, availability: 'partial', data_quality: 'partial', reason_code: 'cleanup_evidence_missing' },
      availability: 'partial',
      data_quality: 'partial',
      reason_code: 'cleanup_evidence_missing',
    }),
  }))
}

for (const width of [1280, 1440]) {
  test(`agent runtime navigation and capabilities table at ${width}px`, async ({ page }) => {
    await installAuth(page)
    await installRoutes(page)
    await page.setViewportSize({ width, height: 1000 })
    await page.goto('/runtime/runs')

    // Every Agent Runtime link resolves to a real route now (no dead navigation entries).
    await page.getByRole('link', { name: 'Capabilities' }).click()
    await expect.poll(() => new URL(page.url()).pathname).toBe('/runtime/capabilities')
    await expect(page.getByRole('heading', { name: 'Capabilities' })).toBeVisible({ timeout: 15000 })
    await expect(page.getByTestId('runtime-capability-row')).toHaveCount(2)

    // configured and observed are separate; an unobserved capability never reads as loaded success.
    const rows = page.getByTestId('runtime-capability-row')
    await expect(rows.nth(0).getByTestId('runtime-capability-configured')).toHaveAttribute('data-state', 'enabled')
    await expect(rows.nth(0).getByTestId('runtime-capability-observed')).toHaveAttribute('data-state', 'loaded')
    await expect(rows.nth(1).getByTestId('runtime-capability-observed')).toHaveAttribute('data-state', 'not_observed')
    await expect(rows.nth(1).getByTestId('runtime-capability-observed')).toHaveText('not_observed')

    // Read-only surface: refresh + pagination navigation only, no business mutation control.
    await expectReadOnlySurface(page)
    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(width)

    await page.getByRole('link', { name: 'Safety' }).click()
    await expect.poll(() => new URL(page.url()).pathname).toBe('/runtime/safety')
    await expect(page.getByTestId('runtime-safety-shadow')).toContainText('Shadow Mode 开启')
    await expect(page.getByTestId('runtime-safety-l1')).toHaveText('阻止')
    await expect(page.getByTestId('runtime-safety-l2')).toHaveText('阻止')
    await expect(page.getByTestId('runtime-safety-audit')).toHaveText('不可用')
    await expectReadOnlySurface(page)

    await page.getByRole('link', { name: 'Worker Health' }).click()
    await expect.poll(() => new URL(page.url()).pathname).toBe('/runtime/worker-health')
    await expect(page.getByTestId('runtime-worker-row')).toHaveCount(2)
    await expect(page.locator('[data-testid="runtime-worker-status"][data-status="stale"]')).toHaveText('stale')
    // Nullable aggregates keep “—” instead of a zero-valued healthy count.
    await expect(page.getByTestId('runtime-worker-aggregate-idle')).toHaveText('—')
    await expect(page.getByTestId('runtime-worker-aggregate-total')).toHaveText('2')
    // Eval/Release/Retention stay honest not_run/unavailable facts.
    await expect(page.getByTestId('runtime-eval-section')).toContainText('未执行')
    await expect(page.getByTestId('runtime-release-gray')).toHaveText('—')
    await expect(page.getByTestId('runtime-release-rollback')).toHaveText('—')
    await expect(page.getByTestId('runtime-retention-section')).toContainText('30')
    await expect(page.getByTestId('runtime-retention-section')).toContainText('cleanup_evidence_missing')
    // No release/rollback/cleanup control exists on the read-only surface.
    await expectReadOnlySurface(page)
    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(width)
  })
}

test('capabilities unavailable state is explicit', async ({ page }) => {
  await installAuth(page)
  await installRoutes(page)
  await page.unroute('**/api/runtime/v1/capabilities**')
  await page.route('**/api/runtime/v1/capabilities**', route => route.fulfill({
    contentType: 'application/json',
    body: envelope({ items: [], page: { page: 1, page_size: 100, total: 0, has_next: false }, availability: 'unavailable', data_quality: 'unknown', reason_code: 'not_observed', not_run: true }),
  }))
  await page.setViewportSize({ width: 1280, height: 900 })
  await page.goto('/runtime/capabilities')
  await expect(page.getByTestId('runtime-capabilities-empty')).toContainText('不可用')
  await expect(page.getByTestId('runtime-quality-reason')).toContainText('not_observed')
  await expect(page.getByTestId('runtime-quality-not-run')).toBeVisible()
})
