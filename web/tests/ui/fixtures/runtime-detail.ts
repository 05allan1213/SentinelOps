// Controlled Runtime detail/timeline payload builders for desktop UI specs.

export const meta = (overrides: Record<string, unknown> = {}) => ({
  availability: 'available',
  data_quality: 'complete',
  ...overrides,
})

export const budget = (overrides: Record<string, unknown> = {}) => ({
  model_calls: 4,
  tool_calls: 2,
  iterations: 3,
  input_tokens: 1200,
  output_tokens: 800,
  cost_cny: null,
  elapsed_ms: 5400,
  mcp_calls: 1,
  rag_calls: 2,
  reserved_model_calls: 1,
  reserved_tool_calls: 0,
  exhausted: false,
  ...meta(),
  ...overrides,
})

export const runSummary = (overrides: Record<string, unknown> = {}) => ({
  run_id: 'run-1',
  session_id: 'sess-1',
  workflow_key: 'workflow-1',
  runtime_mode: 'durable_v1',
  status: 'parked',
  current_phase: 'unknown',
  agent: 'planner',
  attempt: 2,
  worker_id: 'worker-7',
  lease_state: 'expired',
  lease_generation: 9,
  heartbeat_at: '2026-09-10T00:00:05Z',
  recovery_mode: 'manual',
  park_reason: 'runtime_incompatible',
  runtime_version: 'runtime-1',
  runtime_compatibility_hash: 'run-hash',
  query_hash: 'query-hash',
  budget: budget(),
  usage_quality: 'partial',
  trace_quality: 'reconstructed',
  started_at: '2026-09-10T00:00:00Z',
  finished_at: null,
  duration_ms: 5400,
  ...meta(),
  ...overrides,
})

export const runDetail = (overrides: Record<string, unknown> = {}) => {
  const { summary, ...rest } = overrides
  return {
  summary: runSummary(summary as Record<string, unknown> | undefined),
  overview: { status: 'parked', current_phase: 'unknown', summary: '等待人工恢复', ...meta() },
  current_attempt: {
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
  },
  budget: budget(),
  compatibility: {
    run_fingerprint: 'run-fingerprint',
    checkpoint_fingerprint: 'checkpoint-fingerprint',
    attempt_fingerprint: 'attempt-fingerprint',
    executing_worker_fingerprint: 'worker-fingerprint',
    run_match: true,
    checkpoint_match: false,
    worker_match: true,
    exact_restore_allowed: false,
    reason_code: 'checkpoint_mismatch',
    ...meta({ availability: 'partial', data_quality: 'partial' }),
  },
  context_summary: {
    identity: { user_id: 'u-1', username: 'operator', role: 'admin', scope: 'global' },
    session_revision_used: 4,
    session_revision_committed: 3,
    summary_hash: 'summary-hash',
    history_count: 12,
    budget_limits_hash: 'budget-hash',
    deadline_at: '2026-09-10T01:00:00Z',
    runtime_version: 'runtime-1',
    runtime_compatibility_hash: 'run-hash',
    policy_hash: 'policy-hash',
    config_hash: 'config-hash',
    gate_keys: ['l1_write', 'l2_write'],
    ...meta(),
  },
  gate_summary: {
    static_caps: ['l1_write', 'l2_write'],
    dynamic_caps: ['l1_write'],
    effective_caps: [],
    shadow_mode: true,
    l1_write_allowed: false,
    l2_write_allowed: false,
    policy_hash: 'policy-hash',
    catalog_revision: 'catalog-1',
    audit_available: false,
    ...meta({ availability: 'partial', reason_code: 'gate_audit_unavailable' }),
  },
  allowed_recovery_actions: ['resume', 'cancel'],
  ...meta(),
  ...rest,
  }
}

export const runtimeEvent = (seq: number, eventType: string, overrides: Record<string, unknown> = {}) => ({
  seq,
  run_id: 'run-1',
  event_type: eventType,
  attempt: 1,
  generation: 9,
  trace_id: `trace-${seq}`,
  summary: `event ${seq}`,
  created_at: `2026-09-10T00:00:0${seq}.000Z`,
  ...meta(),
  ...overrides,
})

export const timelineRes = (items: Record<string, unknown>[], extra: Record<string, unknown> = {}) => ({
  items,
  page: { page: 1, page_size: 50, total: items.length, has_next: false },
  ...meta(),
  ...extra,
})

export const envelope = (data: unknown) => JSON.stringify({ message: 'OK', data })

/** GetRunRes wrapper: { item: RunDetailDTO, ResourceMeta }. */
export const runDetailRes = (overrides: Record<string, unknown> = {}) => ({
  item: runDetail(overrides),
  ...meta({
    availability: (overrides.availability as string | undefined) ?? 'available',
    data_quality: (overrides.data_quality as string | undefined) ?? 'complete',
    ...(overrides.reason_code ? { reason_code: overrides.reason_code } : {}),
    ...(overrides.not_run ? { not_run: true } : {}),
  }),
})
