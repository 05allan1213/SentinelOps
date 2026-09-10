// Runtime /runtime/v1 DTOs. Field names mirror api/runtime/v1/runtime.go JSON tags exactly.
// Missing or unavailable facts stay `null | undefined`; they are never replaced by zero-valued success defaults.

export type Availability = 'available' | 'partial' | 'unavailable'
export type DataQuality = 'complete' | 'reconstructed' | 'partial' | 'unknown'
export type RuntimeStatus =
  | 'pending'
  | 'running'
  | 'waiting_approval'
  | 'retryable_failed'
  | 'parked'
  | 'reconciling'
  | 'succeeded'
  | 'failed'
  | 'canceled'
export type CurrentPhase =
  | 'planning'
  | 'executing'
  | 'waiting_approval'
  | 'recovering'
  | 'reconciling'
  | 'completed'
  | 'failed'
  | 'unknown'
export type RecoveryAction = 'resume' | 'replay' | 'cancel' | 'restore'
export type OperationStatus = 'accepted' | 'running' | 'succeeded' | 'failed' | 'canceled' | 'rejected'
export type SortDirection = 'asc' | 'desc'
export type CheckpointState = 'valid' | 'missing' | 'corrupt' | 'expired' | 'incompatible'
export type EffectRole = 'primary' | 'derived'
export type EffectStatus = 'pending' | 'running' | 'succeeded' | 'failed' | 'unknown' | 'reconciling'

export interface ResourceMeta {
  availability: Availability
  data_quality: DataQuality
  reason_code?: string
  not_run?: boolean
}

export interface PageMeta {
  page: number
  page_size: number
  total: number
  has_next: boolean
}

export interface RuntimeBudgetDTO extends ResourceMeta {
  max_model_calls?: number | null
  max_l0_tool_calls?: number | null
  max_duration_ms?: number | null
  model_calls?: number | null
  tool_calls?: number | null
  iterations?: number | null
  input_tokens?: number | null
  output_tokens?: number | null
  cost_cny?: number | null
  elapsed_ms?: number | null
  mcp_calls?: number | null
  rag_calls?: number | null
  reserved_model_calls?: number | null
  reserved_tool_calls?: number | null
  exhausted: boolean
  exhausted_reason?: string
}

export interface RuntimeCompatibilityDTO extends ResourceMeta {
  run_fingerprint: string
  checkpoint_fingerprint: string
  attempt_fingerprint: string
  executing_worker_fingerprint: string
  run_match: boolean
  checkpoint_match: boolean
  worker_match: boolean
  exact_restore_allowed: boolean
  reason_code?: string
}

export interface RuntimeGateSummaryDTO extends ResourceMeta {
  static_caps: string[]
  dynamic_caps: string[]
  effective_caps: string[]
  shadow_mode: boolean
  l1_write_allowed: boolean
  l2_write_allowed: boolean
  policy_hash: string
  catalog_revision: string
  audit_available: boolean
}

export interface IdentityDTO {
  user_id: string
  username?: string
  role: string
  scope: string
  auth_disabled?: boolean
}

export interface RuntimeContextSummaryDTO extends ResourceMeta {
  identity: IdentityDTO
  session_revision_used: number
  session_revision_committed: number
  summary_hash: string
  history_count: number
  budget_limits_hash: string
  deadline_at?: string | null
  runtime_version: string
  runtime_compatibility_hash: string
  policy_hash: string
  config_hash: string
  gate_keys: string[]
}

export interface RunOverviewDTO extends ResourceMeta {
  status: RuntimeStatus
  current_phase: CurrentPhase
  summary?: string
}

export interface RunSummaryDTO extends ResourceMeta {
  run_id: string
  session_id: string
  workflow_key: string
  runtime_mode: string
  status: RuntimeStatus
  current_phase: CurrentPhase
  agent: string
  attempt: number
  worker_id: string
  lease_state: string
  lease_generation: number
  heartbeat_at?: string | null
  recovery_mode: string
  park_reason: string
  runtime_version: string
  runtime_compatibility_hash: string
  query_hash: string
  budget: RuntimeBudgetDTO
  usage_quality: DataQuality
  trace_quality: DataQuality
  started_at?: string | null
  finished_at?: string | null
  duration_ms: number
}

export interface RunDetailDTO extends ResourceMeta {
  summary: RunSummaryDTO
  overview: RunOverviewDTO
  current_attempt?: AttemptDTO | null
  budget: RuntimeBudgetDTO
  compatibility: RuntimeCompatibilityDTO
  context_summary: RuntimeContextSummaryDTO
  gate_summary: RuntimeGateSummaryDTO
  allowed_recovery_actions: RecoveryAction[]
}

export interface RuntimeEventDTO extends ResourceMeta {
  seq: number
  run_id: string
  event_type: string
  attempt: number
  generation: number
  trace_id: string
  operation_id?: string
  summary: string
  reference?: string
  attributes?: Record<string, string>
  created_at: string
}

export interface AttemptDTO extends ResourceMeta {
  attempt_id: string
  run_id: string
  attempt: number
  mode: 'fresh' | 'resume' | 'replay' | string
  status: RuntimeStatus
  current_phase: CurrentPhase
  worker_id: string
  lease_generation: number
  runtime_version: string
  run_compatibility_hash: string
  checkpoint_compatibility_hash: string
  executing_worker_fingerprint: string
  trace_id: string
  operation_id?: string
  retry_count: number
  failover_count: number
  failure_code?: string
  failure_message?: string
  usage_quality: DataQuality
  trace_quality: DataQuality
  started_at?: string | null
  finished_at?: string | null
}

export interface CheckpointDTO extends ResourceMeta {
  checkpoint_id: string
  checkpoint_key: string
  payload_sha256: string
  runtime_version: string
  runtime_compatibility_hash: string
  lease_generation: number
  state: CheckpointState | string
  committed_at?: string | null
  expires_at?: string | null
  created_at: string
}

export interface ApprovalDTO extends ResourceMeta {
  id: string
  run_id: string
  tool_name: string
  tool_revision: string
  risk_level: string
  proposal_hash: string
  requested_by: string
  decided_by: string
  status: string
  version: number
  decision_reason?: string
  published_at?: string | null
  expires_at?: string | null
  decided_at?: string | null
  tool_schema_hash: string
  policy_hash: string
  runtime_compatibility_hash: string
  checkpoint_id?: string
  checkpoint_payload_sha256?: string
  checkpoint_lease_generation?: number
  proposal?: Record<string, string>
  event_seq?: number
}

export interface EffectHistoryDTO {
  seq: number
  event_type: string
  status: string
  actor_id?: string
  reason?: string
  evidence_reference?: string
  created_at: string
}

export interface EffectDTO extends ResourceMeta {
  id: string
  run_id: string
  effect_role: EffectRole | string
  effect_step: string
  parent_effect_id?: string
  idempotency_key_digest: string
  proposal_hash: string
  tool_name: string
  tool_revision: string
  tool_schema_hash: string
  target_hash: string
  effect_type: string
  status: EffectStatus | string
  version: number
  external_reference?: string
  lease_generation: number
  attempt: number
  reconciliation_attempts: number
  resolution?: string
  resolved_by?: string
  created_at: string
  updated_at?: string | null
  history?: EffectHistoryDTO[]
}

export interface EvidenceDTO extends ResourceMeta {
  evidence_id: string
  source_type: string
  source_id: string
  base_id: string
  document_id: string
  chunk_id: string
  source_version: string
  content_hash: string
  access_scope: string
  indexed_version: string
  retrieved_at?: string | null
  vector_score?: number | null
  rerank_score?: number | null
  answer_references?: string[]
  quote_available: boolean
}

export interface EvidenceContentDTO extends ResourceMeta {
  evidence_id: string
  quote: string
  content_hash: string
  source_version: string
  access_scope: string
  redaction_applied: boolean
}

export interface RuntimeHistoryMessageDTO {
  role: string
  content: string
}

export interface ContextDTO extends ResourceMeta {
  identity: IdentityDTO
  session_revision_used: number
  session_revision_committed: number
  summary_hash: string
  history_count: number
  history?: RuntimeHistoryMessageDTO[]
  history_truncated?: boolean
  redaction_applied?: boolean
  budget_limits_hash: string
  deadline_at?: string | null
  runtime_version: string
  runtime_compatibility_hash: string
  policy_hash: string
  config_hash: string
  gate_keys: string[]
}

export interface TraceAggregateDTO extends ResourceMeta {
  trace_id: string
  attempt: number
  status: string
  trace_quality: DataQuality
  duration_ms: number
  input_tokens: number
  output_tokens: number
  cost_cny: number
  node_count: number
  raw_available: boolean
  detail_url?: string
}

export interface OperationDTO extends ResourceMeta {
  operation_id: string
  run_id: string
  action: RecoveryAction
  status: OperationStatus
  terminal: boolean
  idempotent_replay: boolean
  accepted_at: string
  started_at?: string | null
  finished_at?: string | null
  reason?: string
  error_code?: string
  correlation_seq?: number
}

export interface CapabilityDTO extends ResourceMeta {
  name: string
  source: string
  risk: string
  revision: string
  schema_hash: string
  effect_type: string
  required_gate: string
  required_role: string
  allowed_agents: string[]
  configured_state: string
  observed_worker_state: string
  last_observed_at?: string | null
}

export interface SafetyDTO extends ResourceMeta {
  static_caps: string[]
  dynamic_caps: string[]
  current_effective: string[]
  shadow_mode: boolean
  policy_hash: string
  catalog_revision: string
  gate_audit_available: boolean
}

export interface WorkerObservationDTO extends ResourceMeta {
  worker_id: string
  status: string
  heartbeat_at?: string | null
  runtime_version: string
  runtime_compatibility_hash: string
  active_run_id?: string
  active_generation?: number
  observed_mcp_count: number
  observed_skill_count: number
  last_error?: string
}

export interface WorkerAggregateDTO extends ResourceMeta {
  total?: number | null
  active?: number | null
  idle?: number | null
  stale?: number | null
}

export interface EvalDTO extends ResourceMeta {
  suite: string
  case_count: number
  passed: number
  failed: number
  baseline_version?: string
  runtime_version?: string
  regression?: boolean | null
  artifacts?: string[]
  deterministic_gate_result?: string
  llm_judge_result?: string
}

export interface ReleaseDTO extends ResourceMeta {
  runtime_version: string
  gate_vector?: Record<string, boolean>
  observed_worker_versions?: string[]
  gray_state?: string | null
  rollback_state?: string | null
}

export interface RetentionDTO extends ResourceMeta {
  payload_days: number
  audit_days: number
  policy_valid: boolean
  last_cleanup?: string | null
  protected_active_count: number
}

export interface ListRunsRes extends ResourceMeta {
  items?: RunSummaryDTO[]
  page?: PageMeta
}

export interface TimelineRes extends ResourceMeta {
  items?: RuntimeEventDTO[]
  page?: PageMeta
}

export interface AttemptsRes extends ResourceMeta {
  items?: AttemptDTO[]
  page?: PageMeta
}

export interface CheckpointsRes extends ResourceMeta {
  items?: CheckpointDTO[]
  page?: PageMeta
}

export interface ApprovalsRes extends ResourceMeta {
  items?: ApprovalDTO[]
  page?: PageMeta
}

export interface EffectsRes extends ResourceMeta {
  items?: EffectDTO[]
  page?: PageMeta
}

export interface EvidenceRes extends ResourceMeta {
  items?: EvidenceDTO[]
  page?: PageMeta
}

export interface TracesRes extends ResourceMeta {
  items?: TraceAggregateDTO[]
  page?: PageMeta
}

export interface GetRunRes extends ResourceMeta {
  item: RunDetailDTO
}

export interface GetContextRes extends ResourceMeta {
  item: ContextDTO
}

export interface ExpandEvidenceRes extends ResourceMeta {
  item: EvidenceContentDTO
}

export interface GetOperationRes extends ResourceMeta {
  item: OperationDTO
}

export interface OperationAcceptedRes extends ResourceMeta {
  operation: OperationDTO
}

export interface CapabilitiesRes extends ResourceMeta {
  items?: CapabilityDTO[]
  page?: PageMeta
}

export interface SafetyRes extends ResourceMeta {
  item: SafetyDTO
}

export interface WorkerHealthRes extends ResourceMeta {
  items?: WorkerObservationDTO[]
  page?: PageMeta
  aggregate?: WorkerAggregateDTO | null
}

export interface EvalRes extends ResourceMeta {
  item: EvalDTO
}

export interface ReleaseRes extends ResourceMeta {
  item: ReleaseDTO
}

export interface RetentionRes extends ResourceMeta {
  item: RetentionDTO
}

export interface PageParams {
  page?: number
  page_size?: number
}

export interface ListRunsParams extends PageParams {
  status?: RuntimeStatus | ''
  session_id?: string
  agent?: string
  from?: string
  to?: string
  scope?: string
  include_legacy?: boolean
  sort?: string
  direction?: SortDirection | ''
}

export interface TimelineParams extends PageParams {
  event_types?: string[]
  attempt?: number
  generation?: number
  from?: string
  to?: string
  sort?: string
  direction?: SortDirection | ''
}

export interface EffectsParams extends PageParams {
  status?: EffectStatus | ''
  effect_role?: EffectRole | ''
  effect_step?: string
  attempt?: number
  generation?: number
}

export interface EvalParams extends PageParams {
  suite?: string
}

export interface RecoverRunRequest {
  action: RecoveryAction
  idempotency_key: string
  reason: string
  expected_generation: number
  expected_compatibility_hash?: string
}
