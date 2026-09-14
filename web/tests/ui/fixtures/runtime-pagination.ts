// Controlled multi-page Runtime fixtures for Batch 4V browser pagination verification.
// These mirror the frozen /runtime/v1 envelopes (ResourceMeta + PageMeta) without inventing new fields.
import type { Page } from '@playwright/test'

export const envelope = (data: unknown) => JSON.stringify({ message: 'OK', data })

export interface PageSlice {
  items: unknown[]
  page: { page: number; page_size: number; total: number; has_next: boolean }
}

/** Server-side read-model slice: the frontend must never paginate on its own. */
export function slicePage<T>(items: T[], requestUrl: string, defaultPageSize = 50): PageSlice {
  const url = new URL(requestUrl)
  const page = Number(url.searchParams.get('page') ?? '1')
  const pageSize = Number(url.searchParams.get('page_size') ?? String(defaultPageSize))
  const start = (page - 1) * pageSize
  const slice = start >= items.length ? [] : items.slice(start, start + pageSize)
  return {
    items: slice,
    page: { page, page_size: pageSize, total: items.length, has_next: start + pageSize < items.length },
  }
}

export const queryOf = (url: URL) => Object.fromEntries(url.searchParams.entries())

/** Deterministic gate for "request is pending" states; no fixed sleeps. */
export function deferred() {
  let release: () => void = () => {}
  const promise = new Promise<void>(resolve => { release = resolve })
  return { promise, release: () => release() }
}

export async function installAuth(page: Page) {
  await page.addInitScript(() => {
    localStorage.setItem('token', 'controlled-runtime-token')
    localStorage.setItem('app-storage', JSON.stringify({
      state: { theme: 'light', sidebarWidth: 288, sidebarCollapsed: false }, version: 1,
    }))
  })
}

/** Low-priority catch-all so no request escapes the fixtures; register specific routes afterwards. */
export async function installFallback(page: Page) {
  await page.route('**/api/**', route => route.fulfill({
    json: { data: { items: [], page: { page: 1, page_size: 20, total: 0, has_next: false }, availability: 'available', data_quality: 'complete' } },
  }))
}

export const capability = (name: string, overrides: Record<string, unknown> = {}) => ({
  name,
  source: 'local_tool',
  risk: 'medium',
  revision: 'rev-3',
  schema_hash: `schema-${name}`,
  effect_type: 'read',
  required_gate: 'l1_write',
  required_role: 'admin',
  allowed_agents: ['executor'],
  configured_state: 'enabled',
  observed_worker_state: 'loaded',
  last_observed_at: '2026-09-10T00:00:00Z',
  availability: 'available',
  data_quality: 'complete',
  ...overrides,
})

// 5 capabilities -> 3 pages at page_size=2, 1 page at page_size=5.
export const CAPABILITIES = [
  capability('capability-a'),
  capability('capability-b', { observed_worker_state: 'not_observed', availability: 'partial', data_quality: 'partial', reason_code: 'not_observed', not_run: true }),
  capability('capability-c', { configured_state: 'disabled', observed_worker_state: 'not_observed', availability: 'partial', data_quality: 'partial', reason_code: 'not_observed' }),
  capability('capability-d', { source: 'mcp', configured_state: 'allowed' }),
  capability('capability-e', { configured_state: 'denied', observed_worker_state: 'not_observed', availability: 'partial', data_quality: 'partial' }),
]

export const worker = (workerId: string, status: string, overrides: Record<string, unknown> = {}) => ({
  worker_id: workerId,
  status,
  heartbeat_at: '2026-09-10T00:00:05Z',
  runtime_version: 'runtime-1',
  runtime_compatibility_hash: `hash-${workerId}`,
  active_run_id: '',
  active_generation: 0,
  observed_mcp_count: 2,
  observed_skill_count: 1,
  last_error: '',
  availability: 'available',
  data_quality: 'complete',
  ...overrides,
})

// 4 workers -> 2 pages at page_size=2. The aggregate is global and must not track the current page.
export const WORKERS = [
  worker('worker-a', 'active', { active_run_id: 'run-1', active_generation: 9 }),
  worker('worker-b', 'stale', { heartbeat_at: '2026-09-09T20:00:00Z', last_error: 'heartbeat expired' }),
  worker('worker-c', 'idle'),
  worker('worker-d', 'active', { active_run_id: 'run-2', active_generation: 4 }),
]

export const WORKER_AGGREGATE = { total: 4, active: 2, idle: 1, stale: 1, availability: 'available', data_quality: 'complete' }

export const runRow = (index: number, overrides: Record<string, unknown> = {}) => ({
  run_id: `run-${String(index).padStart(2, '0')}`,
  session_id: `sess-${index}`,
  workflow_key: 'wf',
  runtime_mode: 'durable_v1',
  status: 'succeeded',
  current_phase: 'completed',
  agent: 'planner',
  attempt: 1,
  worker_id: 'worker-1',
  lease_state: 'active',
  lease_generation: 3,
  heartbeat_at: '2026-09-10T00:00:05Z',
  recovery_mode: '',
  park_reason: '',
  runtime_version: 'runtime-1',
  runtime_compatibility_hash: 'hash-1',
  query_hash: 'query-1',
  budget: { model_calls: 2, tool_calls: 1, elapsed_ms: 1000, exhausted: false, availability: 'available', data_quality: 'complete' },
  usage_quality: 'complete',
  trace_quality: 'complete',
  started_at: '2026-09-10T00:00:00Z',
  finished_at: null,
  duration_ms: 1234,
  availability: 'available',
  data_quality: 'complete',
  ...overrides,
})

// 45 runs -> 3 pages at page_size=20, 1 page at page_size=50.
export const RUNS = Array.from({ length: 45 }, (_, index) => runRow(index + 1))
