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
