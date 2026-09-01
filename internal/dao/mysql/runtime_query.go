package mysql

import (
	"context"
	"fmt"
	"strings"
	"time"

	"SentinelOps/internal/ai/policy"

	"gorm.io/gorm"
)

// RuntimeRunFilter contains the server-side filters for the durable Run list.
// Scope is advisory ("all" is honored only for an admin identity).
type RuntimeRunFilter struct {
	Status, SessionID, Agent string
	From, To                 *time.Time
	Scope                    string
	RuntimeMode              string
	IncludeLegacy            bool
	Sort, Direction          string
	Page, PageSize           int
}

// GORMStore is the query-side MySQL store. It intentionally exposes only the
// WorkflowRun projection and never loads payload/snapshot columns for lists.
type GORMStore struct{ db *gorm.DB }

func NewGORMStore(db *gorm.DB) *GORMStore { return &GORMStore{db: db} }

func ListRuntimeRuns(ctx context.Context, db *gorm.DB, f RuntimeRunFilter) ([]WorkflowRun, int64, error) {
	return NewGORMStore(db).ListRuntimeRuns(ctx, f)
}

func GetRuntimeRun(ctx context.Context, db *gorm.DB, runID string, includeLegacy bool) (*WorkflowRun, error) {
	return NewGORMStore(db).GetRuntimeRun(ctx, runID, includeLegacy)
}

var runtimeRunSortColumns = map[string]string{
	"created_at": "created_at", "updated_at": "updated_at", "started_at": "started_at",
	"finished_at": "finished_at", "status": "status", "attempt": "attempt",
}

var runtimeRunListColumns = []string{
	"id", "workflow_key", "user_id", "session_id", "parent_run_id", "status", "available_at",
	"priority", "runtime_mode", "attempt", "max_attempts", "lease_owner", "lease_until",
	"lease_generation", "heartbeat_at", "session_revision", "runtime_version",
	"runtime_compatibility_hash", "agent_revision", "mcp_catalog_hash", "prompt_hash", "policy_hash",
	"config_hash", "usage_quality", "trace_quality", "last_event_seq", "cancel_requested_at", "park_reason",
	"started_at", "finished_at", "duration_ms", "created_at", "updated_at",
}

func (s *GORMStore) query(ctx context.Context) (*gorm.DB, policy.Identity, error) {
	if ctx == nil || s == nil || s.db == nil {
		return nil, policy.Identity{}, fmt.Errorf("database context/store is required")
	}
	identity, err := policy.IdentityFromContext(ctx)
	if err != nil {
		return nil, identity, err
	}
	return s.db.WithContext(ctx), identity, nil
}

func (s *GORMStore) ListRuntimeRuns(ctx context.Context, f RuntimeRunFilter) ([]WorkflowRun, int64, error) {
	db, identity, err := s.query(ctx)
	if err != nil {
		return nil, 0, err
	}
	q := db.Model(&WorkflowRun{})
	mode := "durable_v1"
	if !f.IncludeLegacy {
		q = q.Where("runtime_mode = ?", mode)
	} else if f.RuntimeMode != "" {
		q = q.Where("runtime_mode = ?", f.RuntimeMode)
	}
	if !identity.Scope.All {
		q = q.Where("user_id = ?", identity.Scope.UserID)
	}
	if f.Status != "" {
		q = q.Where("status = ?", f.Status)
	}
	if f.SessionID != "" {
		q = q.Where("session_id = ?", f.SessionID)
	}
	if !f.ScopeIsAll() && f.Scope != "" && !identity.Scope.All {
		q = q.Where("user_id = ?", identity.Scope.UserID)
	}
	if f.From != nil {
		q = q.Where("created_at >= ?", f.From.UTC())
	}
	if f.To != nil {
		q = q.Where("created_at < ?", f.To.UTC())
	}
	if f.Agent != "" {
		q = q.Where("JSON_VALID(immutable_input_json) AND JSON_UNQUOTE(JSON_EXTRACT(immutable_input_json, '$.agent')) = ?", f.Agent)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	sortCol := runtimeRunSortColumns[f.Sort]
	if sortCol == "" {
		sortCol = "created_at"
	}
	dir := "DESC"
	if strings.EqualFold(f.Direction, "asc") {
		dir = "ASC"
	}
	projection := strings.Join(runtimeRunListColumns, ", ") + ", CASE WHEN immutable_input_json IS NULL OR immutable_input_json = '' THEN '' WHEN JSON_VALID(immutable_input_json) THEN COALESCE(JSON_UNQUOTE(JSON_EXTRACT(immutable_input_json, '$.agent')), '') ELSE '' END AS runtime_agent, CASE WHEN immutable_input_json IS NULL OR immutable_input_json = '' THEN 'missing' WHEN JSON_VALID(immutable_input_json) THEN 'complete' ELSE 'partial' END AS runtime_agent_quality"
	q = q.Select(projection).Order(sortCol + " " + dir).Order("id ASC")
	page, size := f.Page, f.PageSize
	if page < 1 {
		page = 1
	}
	if size <= 0 {
		size = 50
	}
	if size > 200 {
		size = 200
	}
	var rows []WorkflowRun
	if err := q.Offset((page - 1) * size).Limit(size).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

func (f RuntimeRunFilter) ScopeIsAll() bool {
	return strings.EqualFold(strings.TrimSpace(f.Scope), "all")
}

func (s *GORMStore) GetRuntimeRun(ctx context.Context, runID string, includeLegacy ...bool) (*WorkflowRun, error) {
	db, identity, err := s.query(ctx)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(runID) == "" {
		return nil, gorm.ErrRecordNotFound
	}
	q := db.Where("id = ?", runID)
	if len(includeLegacy) == 0 || !includeLegacy[0] {
		q = q.Where("runtime_mode = ?", "durable_v1")
	}
	if !identity.Scope.All {
		q = q.Where("user_id = ?", identity.Scope.UserID)
	}
	var row WorkflowRun
	if err := q.First(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

// RuntimeEffectFilter contains the server-side filters for the read-only
// effect ledger projection.  It deliberately contains no request/response
// content and no authorization identity supplied by a client.
type RuntimeEffectFilter struct {
	Status, EffectRole, EffectStep string
	Attempt                        int
	Generation                     uint64
	Page, PageSize                 int
}

// EffectFilter is kept as a short package-level name for callers which build
// Runtime query filters outside the DAO package.
type EffectFilter = RuntimeEffectFilter

func normalizeRuntimePage(page, size int) (int, int) {
	if page < 1 {
		page = 1
	}
	if size <= 0 {
		size = 50
	}
	if size > 100 {
		size = 100
	}
	return page, size
}

// runtimeScopedQuery starts every Approval/Effect/Event read from the owned
// durable Run.  The run ID is only a selector; identity.Scope remains the
// authorization boundary, so a guessed ID cannot widen visibility.
func (s *GORMStore) runtimeScopedQuery(ctx context.Context, runID string, model any, table string) (*gorm.DB, error) {
	db, identity, err := s.query(ctx)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(runID) == "" {
		return nil, gorm.ErrRecordNotFound
	}
	q := db.Model(model).
		Joins("JOIN workflow_runs ON workflow_runs.id = "+table+".run_id").
		Where(table+".run_id = ? AND workflow_runs.runtime_mode = ?", runID, "durable_v1")
	if !identity.Scope.All {
		q = q.Where("workflow_runs.user_id = ?", identity.Scope.UserID)
	}
	return q, nil
}

// ListRuntimeApprovals returns every persisted Approval lifecycle state for an
// owned durable Run.  It intentionally does not call ListPendingApprovals:
// that workflow primitive is an action-queue query and hides terminal facts
// needed by Runtime Detail.
func (s *GORMStore) ListRuntimeApprovals(ctx context.Context, runID string, page, pageSize int) ([]AgentApproval, int64, error) {
	q, err := s.runtimeScopedQuery(ctx, runID, &AgentApproval{}, "agent_approvals")
	if err != nil {
		return nil, 0, err
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("count runtime approvals: %w", err)
	}
	page, pageSize = normalizeRuntimePage(page, pageSize)
	var rows []AgentApproval
	if err := q.Select("agent_approvals.*").
		Order("agent_approvals.created_at ASC, agent_approvals.id ASC").
		Offset((page - 1) * pageSize).Limit(pageSize).Find(&rows).Error; err != nil {
		return nil, 0, fmt.Errorf("list runtime approvals: %w", err)
	}
	return rows, total, nil
}

// ListRuntimeEffects returns only metadata columns represented by AgentEffect
// and applies all filters as bound values.  Request/response columns are
// loaded for the in-process mapper but are never copied into a Runtime DTO.
func (s *GORMStore) ListRuntimeEffects(ctx context.Context, runID string, f RuntimeEffectFilter) ([]AgentEffect, int64, error) {
	q, err := s.runtimeScopedQuery(ctx, runID, &AgentEffect{}, "agent_effects")
	if err != nil {
		return nil, 0, err
	}
	if f.Status != "" {
		q = q.Where("agent_effects.status = ?", f.Status)
	}
	if f.EffectRole != "" {
		q = q.Where("agent_effects.effect_role = ?", f.EffectRole)
	}
	if f.EffectStep != "" {
		q = q.Where("agent_effects.effect_step = ?", f.EffectStep)
	}
	if f.Attempt < 0 {
		return nil, 0, fmt.Errorf("effect attempt filter must be non-negative")
	}
	if f.Attempt > 0 {
		q = q.Where("agent_effects.attempt = ?", f.Attempt)
	}
	if f.Generation > 0 {
		q = q.Where("agent_effects.lease_generation = ?", f.Generation)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("count runtime effects: %w", err)
	}
	page, pageSize := normalizeRuntimePage(f.Page, f.PageSize)
	var rows []AgentEffect
	if err := q.Select("agent_effects.*").
		// Primary first makes the parent/derived relationship deterministic even
		// when timestamps were written by separate transactions.
		Order("CASE WHEN agent_effects.effect_role = 'primary' THEN 0 ELSE 1 END ASC").
		Order("agent_effects.created_at ASC, agent_effects.effect_step ASC, agent_effects.id ASC").
		Offset((page - 1) * pageSize).Limit(pageSize).Find(&rows).Error; err != nil {
		return nil, 0, fmt.Errorf("list runtime effects: %w", err)
	}
	return rows, total, nil
}

// GetRuntimeEffect loads one effect only when both its ID and its owning
// durable Run are visible to the current server identity.
func (s *GORMStore) GetRuntimeEffect(ctx context.Context, effectID string) (*AgentEffect, error) {
	if strings.TrimSpace(effectID) == "" {
		return nil, gorm.ErrRecordNotFound
	}
	db, identity, err := s.query(ctx)
	if err != nil {
		return nil, err
	}
	q := db.Model(&AgentEffect{}).
		Joins("JOIN workflow_runs ON workflow_runs.id = agent_effects.run_id").
		Where("agent_effects.id = ? AND workflow_runs.runtime_mode = ?", effectID, "durable_v1")
	if !identity.Scope.All {
		q = q.Where("workflow_runs.user_id = ?", identity.Scope.UserID)
	}
	var row AgentEffect
	if err := q.Select("agent_effects.*").First(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

// ListRuntimeEventsForRun returns canonical event rows for a visible durable
// Run.  The service layer performs the allowlisted mapping; callers must not
// serialize Payload directly.
func (s *GORMStore) ListRuntimeEventsForRun(ctx context.Context, runID string) ([]WorkflowEvent, error) {
	q, err := s.runtimeScopedQuery(ctx, runID, &WorkflowEvent{}, "workflow_events")
	if err != nil {
		return nil, err
	}
	var rows []WorkflowEvent
	if err := q.Select("workflow_events.*").Order("workflow_events.seq ASC").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list runtime safety events: %w", err)
	}
	return rows, nil
}

// ListRuntimeEffectEvents is a scoped convenience for callers that only need
// the event stream associated with one effect.  Filtering by the canonical
// effect event catalog happens in the mapper, not in SQL, so malformed or
// future events remain auditable as partial facts.
func (s *GORMStore) ListRuntimeEffectEvents(ctx context.Context, runID, effectID string) ([]WorkflowEvent, error) {
	if strings.TrimSpace(effectID) == "" {
		return nil, gorm.ErrRecordNotFound
	}
	effect, err := s.GetRuntimeEffect(ctx, effectID)
	if err != nil {
		return nil, err
	}
	if effect.RunID != runID {
		return nil, gorm.ErrRecordNotFound
	}
	rows, err := s.ListRuntimeEventsForRun(ctx, runID)
	if err != nil {
		return nil, err
	}
	return rows, nil
}
