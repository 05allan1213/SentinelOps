package v1

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"SentinelOps/internal/ai/workflow"

	"github.com/gogf/gf/v2/frame/g"
)

// ErrRuntimeRequestValidation marks all client-supplied Runtime request validation failures.
var ErrRuntimeRequestValidation = errors.New("runtime request validation failed")

func validationErrorf(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrRuntimeRequestValidation, fmt.Sprintf(format, args...))
}

type Availability string

const (
	AvailabilityAvailable   Availability = "available"
	AvailabilityPartial     Availability = "partial"
	AvailabilityUnavailable Availability = "unavailable"
)

func (v Availability) Valid() bool {
	return v == AvailabilityAvailable || v == AvailabilityPartial || v == AvailabilityUnavailable
}

type DataQuality string

const (
	DataQualityComplete      DataQuality = "complete"
	DataQualityReconstructed DataQuality = "reconstructed"
	DataQualityPartial       DataQuality = "partial"
	DataQualityUnknown       DataQuality = "unknown"
)

func (v DataQuality) Valid() bool {
	return v == DataQualityComplete || v == DataQualityReconstructed || v == DataQualityPartial || v == DataQualityUnknown
}

type RuntimeStatus string

const (
	RuntimeStatusPending         RuntimeStatus = RuntimeStatus(workflow.RunStatusPending)
	RuntimeStatusRunning         RuntimeStatus = RuntimeStatus(workflow.RunStatusRunning)
	RuntimeStatusWaitingApproval RuntimeStatus = RuntimeStatus(workflow.RunStatusWaitingApproval)
	RuntimeStatusRetryableFailed RuntimeStatus = RuntimeStatus(workflow.RunStatusRetryableFailed)
	RuntimeStatusParked          RuntimeStatus = RuntimeStatus(workflow.RunStatusParked)
	RuntimeStatusReconciling     RuntimeStatus = RuntimeStatus(workflow.RunStatusReconciling)
	RuntimeStatusSucceeded       RuntimeStatus = RuntimeStatus(workflow.RunStatusSucceeded)
	RuntimeStatusFailed          RuntimeStatus = RuntimeStatus(workflow.RunStatusFailed)
	RuntimeStatusCanceled        RuntimeStatus = RuntimeStatus(workflow.RunStatusCanceled)
)

func (v RuntimeStatus) Valid() bool {
	switch v {
	case RuntimeStatusPending, RuntimeStatusRunning, RuntimeStatusWaitingApproval, RuntimeStatusRetryableFailed, RuntimeStatusParked, RuntimeStatusReconciling, RuntimeStatusSucceeded, RuntimeStatusFailed, RuntimeStatusCanceled:
		return true
	}
	return false
}

type CurrentPhase string

const (
	CurrentPhasePlanning        CurrentPhase = "planning"
	CurrentPhaseExecuting       CurrentPhase = "executing"
	CurrentPhaseWaitingApproval CurrentPhase = "waiting_approval"
	CurrentPhaseRecovering      CurrentPhase = "recovering"
	CurrentPhaseReconciling     CurrentPhase = "reconciling"
	CurrentPhaseCompleted       CurrentPhase = "completed"
	CurrentPhaseFailed          CurrentPhase = "failed"
	CurrentPhaseUnknown         CurrentPhase = "unknown"
)

func (v CurrentPhase) Valid() bool {
	switch v {
	case CurrentPhasePlanning, CurrentPhaseExecuting, CurrentPhaseWaitingApproval, CurrentPhaseRecovering, CurrentPhaseReconciling, CurrentPhaseCompleted, CurrentPhaseFailed, CurrentPhaseUnknown:
		return true
	}
	return false
}

type RecoveryAction string

const (
	RecoveryActionResume  RecoveryAction = "resume"
	RecoveryActionReplay  RecoveryAction = "replay"
	RecoveryActionCancel  RecoveryAction = "cancel"
	RecoveryActionRestore RecoveryAction = "restore"
)

func (v RecoveryAction) Valid() bool {
	return v == RecoveryActionResume || v == RecoveryActionReplay || v == RecoveryActionCancel || v == RecoveryActionRestore
}

type OperationStatus string

const (
	OperationStatusAccepted  OperationStatus = "accepted"
	OperationStatusRunning   OperationStatus = "running"
	OperationStatusSucceeded OperationStatus = "succeeded"
	OperationStatusFailed    OperationStatus = "failed"
	OperationStatusCanceled  OperationStatus = "canceled"
	OperationStatusRejected  OperationStatus = "rejected"
)

func (v OperationStatus) Valid() bool {
	switch v {
	case OperationStatusAccepted, OperationStatusRunning, OperationStatusSucceeded, OperationStatusFailed, OperationStatusCanceled, OperationStatusRejected:
		return true
	}
	return false
}

type SortDirection string

const (
	SortDirectionAsc  SortDirection = "asc"
	SortDirectionDesc SortDirection = "desc"
)

func (v SortDirection) Valid() bool { return v == SortDirectionAsc || v == SortDirectionDesc }

type ResourceMeta struct {
	Availability Availability `json:"availability"`
	DataQuality  DataQuality  `json:"data_quality"`
	ReasonCode   string       `json:"reason_code,omitempty"`
	NotRun       bool         `json:"not_run,omitempty"`
}
type PageMeta struct {
	Page     int   `json:"page"`
	PageSize int   `json:"page_size"`
	Total    int64 `json:"total"`
	HasNext  bool  `json:"has_next"`
}
type PageRequest struct {
	Page     int `json:"page" form:"page"`
	PageSize int `json:"page_size" form:"page_size"`
}

func (r PageRequest) Valid() error {
	if r.Page < 1 {
		return validationErrorf("page must be >= 1")
	}
	if r.PageSize < 1 || r.PageSize > 100 {
		return validationErrorf("page_size must be between 1 and 100")
	}
	return nil
}
func validID(s, name string) error {
	if strings.TrimSpace(s) == "" {
		return validationErrorf("%s must not be empty", name)
	}
	return nil
}
func validPagedRun(runID string, page, pageSize int) error {
	if err := validID(runID, "run_id"); err != nil {
		return err
	}
	return (PageRequest{Page: page, PageSize: pageSize}).Valid()
}
func validTime(s, name string) error {
	if _, err := ParseRFC3339UTC(s); err != nil {
		return validationErrorf("%s must be RFC3339", name)
	}
	return nil
}

// ParseRFC3339UTC validates a request timestamp and returns its UTC-normalized value.
// An omitted filter is represented by nil so request DTO fields remain the frozen strings.
func ParseRFC3339UTC(value string) (*time.Time, error) {
	if value == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil, validationErrorf("invalid timestamp: %v", err)
	}
	utc := parsed.UTC()
	return &utc, nil
}

type ListRunsReq struct {
	g.Meta        `path:"/runtime/v1/runs" method:"GET"`
	Status        RuntimeStatus `json:"status"`
	SessionID     string        `json:"session_id"`
	Agent         string        `json:"agent"`
	From          string        `json:"from"`
	To            string        `json:"to"`
	Scope         string        `json:"scope"`
	IncludeLegacy bool          `json:"include_legacy"`
	Sort          string        `json:"sort"`
	Direction     SortDirection `json:"direction"`
	Page          int           `json:"page"`
	PageSize      int           `json:"page_size"`
}

func (r ListRunsReq) Valid() error {
	if err := (PageRequest{r.Page, r.PageSize}).Valid(); err != nil {
		return err
	}
	if err := validTime(r.From, "from"); err != nil {
		return err
	}
	if err := validTime(r.To, "to"); err != nil {
		return err
	}
	if r.Status != "" && !r.Status.Valid() {
		return validationErrorf("invalid status")
	}
	if r.Direction != "" && !r.Direction.Valid() {
		return validationErrorf("invalid direction")
	}
	if r.Sort != "" && !validRunSort(r.Sort) {
		return validationErrorf("invalid sort")
	}
	return nil
}

func validTimelineSort(v string) bool {
	return v == "seq" || v == "created_at"
}

func validRunSort(v string) bool {
	switch v {
	case "created_at", "updated_at", "started_at", "finished_at", "status", "attempt":
		return true
	}
	return false
}

type GetRunReq struct {
	g.Meta `path:"/runtime/v1/runs/{run_id}" method:"GET"`
	RunID  string `json:"run_id"`
}

func (r GetRunReq) Valid() error { return validID(r.RunID, "run_id") }

type GetTimelineReq struct {
	g.Meta     `path:"/runtime/v1/runs/{run_id}/timeline" method:"GET"`
	RunID      string        `json:"run_id"`
	EventTypes []string      `json:"event_types"`
	Attempt    int           `json:"attempt"`
	Generation uint64        `json:"generation"`
	From       string        `json:"from"`
	To         string        `json:"to"`
	Sort       string        `json:"sort"`
	Direction  SortDirection `json:"direction"`
	Page       int           `json:"page"`
	PageSize   int           `json:"page_size"`
}

func (r GetTimelineReq) Valid() error {
	if e := validID(r.RunID, "run_id"); e != nil {
		return e
	}
	if e := (PageRequest{r.Page, r.PageSize}).Valid(); e != nil {
		return e
	}
	if e := validTime(r.From, "from"); e != nil {
		return e
	}
	if err := validTime(r.To, "to"); err != nil {
		return err
	}
	if r.Direction != "" && !r.Direction.Valid() {
		return validationErrorf("invalid direction")
	}
	if r.Sort != "" && !validTimelineSort(r.Sort) {
		return validationErrorf("invalid sort")
	}
	for _, eventType := range r.EventTypes {
		if !validEventType(eventType) {
			return validationErrorf("invalid event_type")
		}
	}
	return nil
}

func validEventType(value string) bool {
	for _, item := range workflow.VersionedEventCatalog() {
		if value == item {
			return true
		}
	}
	return false
}

type RunEventsReq struct {
	g.Meta   `path:"/runtime/v1/runs/{run_id}/events" method:"GET"`
	RunID    string `json:"run_id"`
	AfterSeq int64  `json:"after_seq"`
}

func (r RunEventsReq) Valid() error {
	if e := validID(r.RunID, "run_id"); e != nil {
		return e
	}
	if r.AfterSeq < 0 {
		return validationErrorf("after_seq must be >= 0")
	}
	return nil
}

type GetAttemptsReq struct {
	g.Meta   `path:"/runtime/v1/runs/{run_id}/attempts" method:"GET"`
	RunID    string `json:"run_id"`
	Page     int    `json:"page"`
	PageSize int    `json:"page_size"`
}

func (r GetAttemptsReq) Valid() error { return validPagedRun(r.RunID, r.Page, r.PageSize) }

type GetCheckpointsReq struct {
	g.Meta   `path:"/runtime/v1/runs/{run_id}/checkpoints" method:"GET"`
	RunID    string `json:"run_id"`
	Page     int    `json:"page"`
	PageSize int    `json:"page_size"`
}

func (r GetCheckpointsReq) Valid() error { return validPagedRun(r.RunID, r.Page, r.PageSize) }

type GetApprovalsReq struct {
	g.Meta   `path:"/runtime/v1/runs/{run_id}/approvals" method:"GET"`
	RunID    string `json:"run_id"`
	Page     int    `json:"page"`
	PageSize int    `json:"page_size"`
}

func (r GetApprovalsReq) Valid() error { return validPagedRun(r.RunID, r.Page, r.PageSize) }

type GetEffectsReq struct {
	g.Meta     `path:"/runtime/v1/runs/{run_id}/effects" method:"GET"`
	RunID      string `json:"run_id"`
	Status     string `json:"status"`
	EffectRole string `json:"effect_role"`
	EffectStep string `json:"effect_step"`
	Attempt    int    `json:"attempt"`
	Generation uint64 `json:"generation"`
	Page       int    `json:"page"`
	PageSize   int    `json:"page_size"`
}

func (r GetEffectsReq) Valid() error {
	if err := validPagedRun(r.RunID, r.Page, r.PageSize); err != nil {
		return err
	}
	if r.Status != "" {
		switch r.Status {
		case "pending", "running", "succeeded", "failed", "unknown", "reconciling":
		default:
			return validationErrorf("invalid effect status")
		}
	}
	if r.EffectRole != "" && r.EffectRole != "primary" && r.EffectRole != "derived" {
		return validationErrorf("invalid effect role")
	}
	if strings.TrimSpace(r.EffectStep) != r.EffectStep || len(r.EffectStep) > 128 {
		return validationErrorf("invalid effect step")
	}
	if r.Attempt < 0 {
		return validationErrorf("attempt must be >= 0")
	}
	return nil
}

type GetEvidenceReq struct {
	g.Meta   `path:"/runtime/v1/runs/{run_id}/evidence" method:"GET"`
	RunID    string `json:"run_id"`
	Page     int    `json:"page"`
	PageSize int    `json:"page_size"`
}

func (r GetEvidenceReq) Valid() error { return validPagedRun(r.RunID, r.Page, r.PageSize) }

type ExpandEvidenceReq struct {
	g.Meta     `path:"/runtime/v1/runs/{run_id}/evidence/{evidence_id}" method:"GET"`
	RunID      string `json:"run_id"`
	EvidenceID string `json:"evidence_id"`
	Include    string `json:"include"`
}

func (r ExpandEvidenceReq) Valid() error {
	if e := validID(r.RunID, "run_id"); e != nil {
		return e
	}
	if e := validID(r.EvidenceID, "evidence_id"); e != nil {
		return e
	}
	if r.Include != "" && r.Include != "quote" {
		return validationErrorf("invalid include")
	}
	return nil
}

type GetContextReq struct {
	g.Meta  `path:"/runtime/v1/runs/{run_id}/context" method:"GET"`
	RunID   string `json:"run_id"`
	Include string `json:"include"`
}

func (r GetContextReq) Valid() error {
	if e := validID(r.RunID, "run_id"); e != nil {
		return e
	}
	if r.Include != "" && r.Include != "history" {
		return validationErrorf("invalid include")
	}
	return nil
}

type GetTracesReq struct {
	g.Meta   `path:"/runtime/v1/runs/{run_id}/traces" method:"GET"`
	RunID    string `json:"run_id"`
	Page     int    `json:"page"`
	PageSize int    `json:"page_size"`
}

func (r GetTracesReq) Valid() error { return validPagedRun(r.RunID, r.Page, r.PageSize) }

type RecoverRunReq struct {
	g.Meta                    `path:"/runtime/v1/runs/{run_id}/recovery" method:"POST"`
	RunID                     string         `json:"run_id"`
	Action                    RecoveryAction `json:"action"`
	IdempotencyKey            string         `json:"idempotency_key"`
	Reason                    string         `json:"reason"`
	ExpectedGeneration        uint64         `json:"expected_generation"`
	ExpectedCompatibilityHash string         `json:"expected_compatibility_hash,omitempty"`
}

func (r RecoverRunReq) Valid() error {
	if e := validID(r.RunID, "run_id"); e != nil {
		return e
	}
	if !r.Action.Valid() {
		return validationErrorf("invalid action")
	}
	if strings.TrimSpace(r.IdempotencyKey) == "" {
		return validationErrorf("idempotency_key must not be empty")
	}
	if strings.TrimSpace(r.Reason) == "" {
		return validationErrorf("reason must not be empty")
	}
	return nil
}

type GetOperationReq struct {
	g.Meta      `path:"/runtime/v1/operations/{operation_id}" method:"GET"`
	OperationID string `json:"operation_id"`
}

func (r GetOperationReq) Valid() error { return validID(r.OperationID, "operation_id") }

type GetCapabilitiesReq struct {
	g.Meta `path:"/runtime/v1/capabilities" method:"GET"`
}
type GetSafetyReq struct {
	g.Meta `path:"/runtime/v1/safety" method:"GET"`
}
type GetWorkerHealthReq struct {
	g.Meta `path:"/runtime/v1/worker-health" method:"GET"`
}
type GetEvalReq struct {
	g.Meta   `path:"/runtime/v1/eval" method:"GET"`
	Suite    string `json:"suite"`
	Page     int    `json:"page"`
	PageSize int    `json:"page_size"`
}

func (r GetEvalReq) Valid() error { return (PageRequest{r.Page, r.PageSize}).Valid() }

type GetReleaseReq struct {
	g.Meta `path:"/runtime/v1/release" method:"GET"`
}
type GetRetentionReq struct {
	g.Meta `path:"/runtime/v1/retention" method:"GET"`
}

type RuntimeBudgetDTO struct {
	MaxModelCalls      *int     `json:"max_model_calls,omitempty"`
	MaxL0ToolCalls     *int     `json:"max_l0_tool_calls,omitempty"`
	MaxDurationMs      *int64   `json:"max_duration_ms,omitempty"`
	ModelCalls         *int     `json:"model_calls,omitempty"`
	ToolCalls          *int     `json:"tool_calls,omitempty"`
	Iterations         *int     `json:"iterations,omitempty"`
	InputTokens        *int64   `json:"input_tokens,omitempty"`
	OutputTokens       *int64   `json:"output_tokens,omitempty"`
	CostCNY            *float64 `json:"cost_cny,omitempty"`
	ElapsedMs          *int64   `json:"elapsed_ms,omitempty"`
	MCPCalls           *int     `json:"mcp_calls,omitempty"`
	RAGCalls           *int     `json:"rag_calls,omitempty"`
	ReservedModelCalls *int     `json:"reserved_model_calls,omitempty"`
	ReservedToolCalls  *int     `json:"reserved_tool_calls,omitempty"`
	Exhausted          bool     `json:"exhausted"`
	ExhaustedReason    string   `json:"exhausted_reason,omitempty"`
	ResourceMeta
}
type RuntimeCompatibilityDTO struct {
	RunFingerprint             string `json:"run_fingerprint"`
	CheckpointFingerprint      string `json:"checkpoint_fingerprint"`
	AttemptFingerprint         string `json:"attempt_fingerprint"`
	ExecutingWorkerFingerprint string `json:"executing_worker_fingerprint"`
	RunMatch                   bool   `json:"run_match"`
	CheckpointMatch            bool   `json:"checkpoint_match"`
	WorkerMatch                bool   `json:"worker_match"`
	ExactRestoreAllowed        bool   `json:"exact_restore_allowed"`
	ReasonCode                 string `json:"reason_code,omitempty"`
	ResourceMeta
}
type RuntimeGateSummaryDTO struct {
	StaticCaps      []string `json:"static_caps"`
	DynamicCaps     []string `json:"dynamic_caps"`
	EffectiveCaps   []string `json:"effective_caps"`
	ShadowMode      bool     `json:"shadow_mode"`
	L1WriteAllowed  bool     `json:"l1_write_allowed"`
	L2WriteAllowed  bool     `json:"l2_write_allowed"`
	PolicyHash      string   `json:"policy_hash"`
	CatalogRevision string   `json:"catalog_revision"`
	AuditAvailable  bool     `json:"audit_available"`
	ResourceMeta
}
type RuntimeContextSummaryDTO struct {
	Identity                 IdentityDTO `json:"identity"`
	SessionRevisionUsed      uint64      `json:"session_revision_used"`
	SessionRevisionCommitted uint64      `json:"session_revision_committed"`
	SummaryHash              string      `json:"summary_hash"`
	HistoryCount             int         `json:"history_count"`
	BudgetLimitsHash         string      `json:"budget_limits_hash"`
	DeadlineAt               *time.Time  `json:"deadline_at,omitempty"`
	RuntimeVersion           string      `json:"runtime_version"`
	RuntimeCompatibilityHash string      `json:"runtime_compatibility_hash"`
	PolicyHash               string      `json:"policy_hash"`
	ConfigHash               string      `json:"config_hash"`
	GateKeys                 []string    `json:"gate_keys"`
	ResourceMeta
}
type IdentityDTO struct {
	UserID       string `json:"user_id"`
	Username     string `json:"username,omitempty"`
	Role         string `json:"role"`
	Scope        string `json:"scope"`
	AuthDisabled bool   `json:"auth_disabled,omitempty"`
}
type RunOverviewDTO struct {
	Status       RuntimeStatus `json:"status"`
	CurrentPhase CurrentPhase  `json:"current_phase"`
	Summary      string        `json:"summary,omitempty"`
	ResourceMeta
}
type RunSummaryDTO struct {
	RunID                    string           `json:"run_id"`
	SessionID                string           `json:"session_id"`
	WorkflowKey              string           `json:"workflow_key"`
	RuntimeMode              string           `json:"runtime_mode"`
	Status                   RuntimeStatus    `json:"status"`
	CurrentPhase             CurrentPhase     `json:"current_phase"`
	Agent                    string           `json:"agent"`
	Attempt                  int              `json:"attempt"`
	WorkerID                 string           `json:"worker_id"`
	LeaseState               string           `json:"lease_state"`
	LeaseGeneration          uint64           `json:"lease_generation"`
	HeartbeatAt              *time.Time       `json:"heartbeat_at,omitempty"`
	RecoveryMode             string           `json:"recovery_mode"`
	ParkReason               string           `json:"park_reason"`
	RuntimeVersion           string           `json:"runtime_version"`
	RuntimeCompatibilityHash string           `json:"runtime_compatibility_hash"`
	QueryHash                string           `json:"query_hash"`
	Budget                   RuntimeBudgetDTO `json:"budget"`
	UsageQuality             DataQuality      `json:"usage_quality"`
	TraceQuality             DataQuality      `json:"trace_quality"`
	StartedAt                *time.Time       `json:"started_at,omitempty"`
	FinishedAt               *time.Time       `json:"finished_at,omitempty"`
	DurationMs               int64            `json:"duration_ms"`
	ResourceMeta
}

// RuntimeAnswerDTO 暴露 Run 终态的权威答案原文。
// SSE 事件只承载截断后的 summary，长回答需要读模型给出完整文本；
// 该字段直接来自 output_payload，不新增持久化。
type RuntimeAnswerDTO struct {
	Content         string `json:"content"`
	Grounding       string `json:"grounding,omitempty"`
	GroundingReason string `json:"grounding_reason,omitempty"`
}

type RunDetailDTO struct {
	Summary                RunSummaryDTO            `json:"summary"`
	Overview               RunOverviewDTO           `json:"overview"`
	CurrentAttempt         *AttemptDTO              `json:"current_attempt,omitempty"`
	Budget                 RuntimeBudgetDTO         `json:"budget"`
	Compatibility          RuntimeCompatibilityDTO  `json:"compatibility"`
	ContextSummary         RuntimeContextSummaryDTO `json:"context_summary"`
	GateSummary            RuntimeGateSummaryDTO    `json:"gate_summary"`
	AllowedRecoveryActions []RecoveryAction         `json:"allowed_recovery_actions"`
	Answer                 *RuntimeAnswerDTO        `json:"answer,omitempty"`
	ResourceMeta
}
type RuntimeEventDTO struct {
	Seq         uint64            `json:"seq"`
	RunID       string            `json:"run_id"`
	EventType   string            `json:"event_type"`
	Attempt     int               `json:"attempt"`
	Generation  uint64            `json:"generation"`
	TraceID     string            `json:"trace_id"`
	OperationID string            `json:"operation_id,omitempty"`
	Summary     string            `json:"summary"`
	Reference   string            `json:"reference,omitempty"`
	Attributes  map[string]string `json:"attributes,omitempty"`
	CreatedAt   time.Time         `json:"created_at"`
	ResourceMeta
}
type AttemptDTO struct {
	AttemptID                   string        `json:"attempt_id"`
	RunID                       string        `json:"run_id"`
	Attempt                     int           `json:"attempt"`
	Mode                        string        `json:"mode"`
	Status                      RuntimeStatus `json:"status"`
	CurrentPhase                CurrentPhase  `json:"current_phase"`
	WorkerID                    string        `json:"worker_id"`
	LeaseGeneration             uint64        `json:"lease_generation"`
	RuntimeVersion              string        `json:"runtime_version"`
	RunCompatibilityHash        string        `json:"run_compatibility_hash"`
	CheckpointCompatibilityHash string        `json:"checkpoint_compatibility_hash"`
	ExecutingWorkerFingerprint  string        `json:"executing_worker_fingerprint"`
	TraceID                     string        `json:"trace_id"`
	OperationID                 string        `json:"operation_id,omitempty"`
	RetryCount                  int           `json:"retry_count"`
	FailoverCount               int           `json:"failover_count"`
	FailureCode                 string        `json:"failure_code,omitempty"`
	FailureMessage              string        `json:"failure_message,omitempty"`
	UsageQuality                DataQuality   `json:"usage_quality"`
	TraceQuality                DataQuality   `json:"trace_quality"`
	StartedAt                   *time.Time    `json:"started_at,omitempty"`
	FinishedAt                  *time.Time    `json:"finished_at,omitempty"`
	ResourceMeta
}
type CheckpointDTO struct {
	CheckpointID             string     `json:"checkpoint_id"`
	CheckpointKey            string     `json:"checkpoint_key"`
	PayloadSHA256            string     `json:"payload_sha256"`
	RuntimeVersion           string     `json:"runtime_version"`
	RuntimeCompatibilityHash string     `json:"runtime_compatibility_hash"`
	LeaseGeneration          uint64     `json:"lease_generation"`
	State                    string     `json:"state"`
	CommittedAt              *time.Time `json:"committed_at,omitempty"`
	ExpiresAt                *time.Time `json:"expires_at,omitempty"`
	CreatedAt                time.Time  `json:"created_at"`
	ResourceMeta
}
type ApprovalDTO struct {
	ID                        string            `json:"id"`
	RunID                     string            `json:"run_id"`
	ToolName                  string            `json:"tool_name"`
	ToolRevision              string            `json:"tool_revision"`
	RiskLevel                 string            `json:"risk_level"`
	ProposalHash              string            `json:"proposal_hash"`
	RequestedBy               string            `json:"requested_by"`
	DecidedBy                 string            `json:"decided_by"`
	Status                    string            `json:"status"`
	Version                   int               `json:"version"`
	DecisionReason            string            `json:"decision_reason,omitempty"`
	PublishedAt               *time.Time        `json:"published_at,omitempty"`
	ExpiresAt                 *time.Time        `json:"expires_at,omitempty"`
	DecidedAt                 *time.Time        `json:"decided_at,omitempty"`
	ToolSchemaHash            string            `json:"tool_schema_hash"`
	PolicyHash                string            `json:"policy_hash"`
	RuntimeCompatibilityHash  string            `json:"runtime_compatibility_hash"`
	CheckpointID              string            `json:"checkpoint_id,omitempty"`
	CheckpointPayloadSHA256   string            `json:"checkpoint_payload_sha256,omitempty"`
	CheckpointLeaseGeneration uint64            `json:"checkpoint_lease_generation,omitempty"`
	Proposal                  map[string]string `json:"proposal,omitempty"`
	EventSeq                  uint64            `json:"event_seq,omitempty"`
	ResourceMeta
}
type EffectHistoryDTO struct {
	Seq               uint64    `json:"seq"`
	EventType         string    `json:"event_type"`
	Status            string    `json:"status"`
	ActorID           string    `json:"actor_id,omitempty"`
	Reason            string    `json:"reason,omitempty"`
	EvidenceReference string    `json:"evidence_reference,omitempty"`
	CreatedAt         time.Time `json:"created_at"`
}
type EffectDTO struct {
	ID                     string             `json:"id"`
	RunID                  string             `json:"run_id"`
	EffectRole             string             `json:"effect_role"`
	EffectStep             string             `json:"effect_step"`
	ParentEffectID         string             `json:"parent_effect_id,omitempty"`
	IdempotencyKeyDigest   string             `json:"idempotency_key_digest"`
	ProposalHash           string             `json:"proposal_hash"`
	ToolName               string             `json:"tool_name"`
	ToolRevision           string             `json:"tool_revision"`
	ToolSchemaHash         string             `json:"tool_schema_hash"`
	TargetHash             string             `json:"target_hash"`
	EffectType             string             `json:"effect_type"`
	Status                 string             `json:"status"`
	Version                int                `json:"version"`
	ExternalReference      string             `json:"external_reference,omitempty"`
	LeaseGeneration        uint64             `json:"lease_generation"`
	Attempt                int                `json:"attempt"`
	ReconciliationAttempts int                `json:"reconciliation_attempts"`
	Resolution             string             `json:"resolution,omitempty"`
	ResolvedBy             string             `json:"resolved_by,omitempty"`
	CreatedAt              time.Time          `json:"created_at"`
	UpdatedAt              *time.Time         `json:"updated_at,omitempty"`
	History                []EffectHistoryDTO `json:"history,omitempty"`
	ResourceMeta
}
type EvidenceDTO struct {
	EvidenceID       string     `json:"evidence_id"`
	SourceType       string     `json:"source_type"`
	SourceID         string     `json:"source_id"`
	BaseID           string     `json:"base_id"`
	DocumentID       string     `json:"document_id"`
	ChunkID          string     `json:"chunk_id"`
	SourceVersion    string     `json:"source_version"`
	ContentHash      string     `json:"content_hash"`
	AccessScope      string     `json:"access_scope"`
	IndexedVersion   string     `json:"indexed_version"`
	RetrievedAt      *time.Time `json:"retrieved_at,omitempty"`
	VectorScore      *float64   `json:"vector_score,omitempty"`
	RerankScore      *float64   `json:"rerank_score,omitempty"`
	AnswerReferences []string   `json:"answer_references,omitempty"`
	QuoteAvailable   bool       `json:"quote_available"`
	ResourceMeta
}
type EvidenceContentDTO struct {
	EvidenceID       string `json:"evidence_id"`
	Quote            string `json:"quote"`
	ContentHash      string `json:"content_hash"`
	SourceVersion    string `json:"source_version"`
	AccessScope      string `json:"access_scope"`
	RedactionApplied bool   `json:"redaction_applied"`
	ResourceMeta
}
type RuntimeHistoryMessageDTO struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}
type ContextDTO struct {
	Identity                 IdentityDTO                `json:"identity"`
	SessionRevisionUsed      uint64                     `json:"session_revision_used"`
	SessionRevisionCommitted uint64                     `json:"session_revision_committed"`
	SummaryHash              string                     `json:"summary_hash"`
	HistoryCount             int                        `json:"history_count"`
	History                  []RuntimeHistoryMessageDTO `json:"history,omitempty"`
	HistoryTruncated         bool                       `json:"history_truncated,omitempty"`
	RedactionApplied         bool                       `json:"redaction_applied,omitempty"`
	BudgetLimitsHash         string                     `json:"budget_limits_hash"`
	DeadlineAt               *time.Time                 `json:"deadline_at,omitempty"`
	RuntimeVersion           string                     `json:"runtime_version"`
	RuntimeCompatibilityHash string                     `json:"runtime_compatibility_hash"`
	PolicyHash               string                     `json:"policy_hash"`
	ConfigHash               string                     `json:"config_hash"`
	GateKeys                 []string                   `json:"gate_keys"`
	ResourceMeta
}
type TraceAggregateDTO struct {
	TraceID      string      `json:"trace_id"`
	Attempt      int         `json:"attempt"`
	Status       string      `json:"status"`
	TraceQuality DataQuality `json:"trace_quality"`
	DurationMs   int64       `json:"duration_ms"`
	InputTokens  int64       `json:"input_tokens"`
	OutputTokens int64       `json:"output_tokens"`
	CostCNY      float64     `json:"cost_cny"`
	NodeCount    int         `json:"node_count"`
	RawAvailable bool        `json:"raw_available"`
	DetailURL    string      `json:"detail_url,omitempty"`
	ResourceMeta
}
type OperationDTO struct {
	OperationID      string          `json:"operation_id"`
	RunID            string          `json:"run_id"`
	Action           RecoveryAction  `json:"action"`
	Status           OperationStatus `json:"status"`
	Terminal         bool            `json:"terminal"`
	IdempotentReplay bool            `json:"idempotent_replay"`
	AcceptedAt       time.Time       `json:"accepted_at"`
	StartedAt        *time.Time      `json:"started_at,omitempty"`
	FinishedAt       *time.Time      `json:"finished_at,omitempty"`
	Reason           string          `json:"reason,omitempty"`
	ErrorCode        string          `json:"error_code,omitempty"`
	CorrelationSeq   uint64          `json:"correlation_seq,omitempty"`
	ResourceMeta
}
type CapabilityDTO struct {
	Name                string     `json:"name"`
	Source              string     `json:"source"`
	Risk                string     `json:"risk"`
	Revision            string     `json:"revision"`
	SchemaHash          string     `json:"schema_hash"`
	EffectType          string     `json:"effect_type"`
	RequiredGate        string     `json:"required_gate"`
	RequiredRole        string     `json:"required_role"`
	AllowedAgents       []string   `json:"allowed_agents"`
	ConfiguredState     string     `json:"configured_state"`
	ObservedWorkerState string     `json:"observed_worker_state"`
	LastObservedAt      *time.Time `json:"last_observed_at,omitempty"`
	ResourceMeta
}
type SafetyDTO struct {
	StaticCaps         []string `json:"static_caps"`
	DynamicCaps        []string `json:"dynamic_caps"`
	CurrentEffective   []string `json:"current_effective"`
	ShadowMode         bool     `json:"shadow_mode"`
	PolicyHash         string   `json:"policy_hash"`
	CatalogRevision    string   `json:"catalog_revision"`
	GateAuditAvailable bool     `json:"gate_audit_available"`
	ResourceMeta
}
type WorkerObservationDTO struct {
	WorkerID                 string     `json:"worker_id"`
	Status                   string     `json:"status"`
	HeartbeatAt              *time.Time `json:"heartbeat_at,omitempty"`
	RuntimeVersion           string     `json:"runtime_version"`
	RuntimeCompatibilityHash string     `json:"runtime_compatibility_hash"`
	ActiveRunID              string     `json:"active_run_id,omitempty"`
	ActiveGeneration         uint64     `json:"active_generation,omitempty"`
	ObservedMCPCount         int        `json:"observed_mcp_count"`
	ObservedSkillCount       int        `json:"observed_skill_count"`
	LastError                string     `json:"last_error,omitempty"`
	ResourceMeta
}
type WorkerAggregateDTO struct {
	Total  *int `json:"total,omitempty"`
	Active *int `json:"active,omitempty"`
	Idle   *int `json:"idle,omitempty"`
	Stale  *int `json:"stale,omitempty"`
	ResourceMeta
}
type EvalDTO struct {
	Suite                   string   `json:"suite"`
	CaseCount               int      `json:"case_count"`
	Passed                  int      `json:"passed"`
	Failed                  int      `json:"failed"`
	BaselineVersion         string   `json:"baseline_version,omitempty"`
	RuntimeVersion          string   `json:"runtime_version,omitempty"`
	Regression              *bool    `json:"regression,omitempty"`
	Artifacts               []string `json:"artifacts,omitempty"`
	DeterministicGateResult string   `json:"deterministic_gate_result,omitempty"`
	LLMJudgeResult          string   `json:"llm_judge_result,omitempty"`
	ResourceMeta
}
type ReleaseDTO struct {
	RuntimeVersion         string          `json:"runtime_version"`
	GateVector             map[string]bool `json:"gate_vector,omitempty"`
	ObservedWorkerVersions []string        `json:"observed_worker_versions,omitempty"`
	GrayState              *string         `json:"gray_state,omitempty"`
	RollbackState          *string         `json:"rollback_state,omitempty"`
	ResourceMeta
}
type RetentionDTO struct {
	PayloadDays          int        `json:"payload_days"`
	AuditDays            int        `json:"audit_days"`
	PolicyValid          bool       `json:"policy_valid"`
	LastCleanup          *time.Time `json:"last_cleanup,omitempty"`
	ProtectedActiveCount int        `json:"protected_active_count"`
	ResourceMeta
}

type ListRunsRes struct {
	Items []RunSummaryDTO `json:"items"`
	Page  PageMeta        `json:"page"`
	ResourceMeta
}
type TimelineRes struct {
	Items []RuntimeEventDTO `json:"items"`
	Page  PageMeta          `json:"page"`
	ResourceMeta
}
type AttemptsRes struct {
	Items []AttemptDTO `json:"items"`
	Page  PageMeta     `json:"page"`
	ResourceMeta
}
type CheckpointsRes struct {
	Items []CheckpointDTO `json:"items"`
	Page  PageMeta        `json:"page"`
	ResourceMeta
}
type ApprovalsRes struct {
	Items []ApprovalDTO `json:"items"`
	Page  PageMeta      `json:"page"`
	ResourceMeta
}
type EffectsRes struct {
	Items []EffectDTO `json:"items"`
	Page  PageMeta    `json:"page"`
	ResourceMeta
}
type EvidenceRes struct {
	Items []EvidenceDTO `json:"items"`
	Page  PageMeta      `json:"page"`
	ResourceMeta
}
type TracesRes struct {
	Items []TraceAggregateDTO `json:"items"`
	Page  PageMeta            `json:"page"`
	ResourceMeta
}
type GetRunRes struct {
	Item RunDetailDTO `json:"item"`
	ResourceMeta
}
type GetContextRes struct {
	Item ContextDTO `json:"item"`
	ResourceMeta
}
type ExpandEvidenceRes struct {
	Item EvidenceContentDTO `json:"item"`
	ResourceMeta
}
type GetOperationRes struct {
	Item OperationDTO `json:"item"`
	ResourceMeta
}
type OperationAcceptedRes struct {
	Operation OperationDTO `json:"operation"`
	ResourceMeta
}
type CapabilitiesRes struct {
	Items []CapabilityDTO `json:"items"`
	Page  PageMeta        `json:"page"`
	ResourceMeta
}
type SafetyRes struct {
	Item SafetyDTO `json:"item"`
	ResourceMeta
}
type WorkerHealthRes struct {
	Items     []WorkerObservationDTO `json:"items"`
	Page      PageMeta               `json:"page"`
	Aggregate *WorkerAggregateDTO    `json:"aggregate,omitempty"`
	ResourceMeta
}
type EvalRes struct {
	Item EvalDTO `json:"item"`
	ResourceMeta
}
type ReleaseRes struct {
	Item ReleaseDTO `json:"item"`
	ResourceMeta
}
type RetentionRes struct {
	Item RetentionDTO `json:"item"`
	ResourceMeta
}
type RunEventsRes struct{}

type RecoveryCommandRequest struct {
	Action                    RecoveryAction `json:"action"`
	IdempotencyKey            string         `json:"idempotency_key"`
	Reason                    string         `json:"reason"`
	ExpectedGeneration        uint64         `json:"expected_generation"`
	ExpectedCompatibilityHash string         `json:"expected_compatibility_hash,omitempty"`
}
