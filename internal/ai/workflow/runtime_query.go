package workflow

import (
	"context"
	"fmt"
	"strings"

	"SentinelOps/internal/dao/mysql"

	"gorm.io/gorm"
)

// ListRuntimeRuns exposes the runtime query through the canonical workflow store.
func (s *GORMStore) ListRuntimeRuns(ctx context.Context, f mysql.RuntimeRunFilter) ([]mysql.WorkflowRun, int64, error) {
	return mysql.ListRuntimeRuns(ctx, s.db, f)
}

// GetRuntimeRun keeps durable-only lookup by default; pass true for an explicit legacy detail.
func (s *GORMStore) GetRuntimeRun(ctx context.Context, runID string, includeLegacy ...bool) (*mysql.WorkflowRun, error) {
	legacy := len(includeLegacy) > 0 && includeLegacy[0]
	return mysql.GetRuntimeRun(ctx, s.db, runID, legacy)
}

// ListRuntimeApprovals exposes the scoped Runtime Approval projection through
// the canonical workflow store used by RuntimeService.
func (s *GORMStore) ListRuntimeApprovals(ctx context.Context, runID string, page, pageSize int) ([]mysql.AgentApproval, int64, error) {
	if s == nil {
		return nil, 0, fmt.Errorf("workflow store is required")
	}
	return mysql.NewGORMStore(s.db).ListRuntimeApprovals(ctx, runID, page, pageSize)
}

// ListRuntimeEffects exposes the scoped Runtime Effect projection through the
// canonical workflow store; it does not expose an executor or mutation path.
func (s *GORMStore) ListRuntimeEffects(ctx context.Context, runID string, f mysql.RuntimeEffectFilter) ([]mysql.AgentEffect, int64, error) {
	if s == nil {
		return nil, 0, fmt.Errorf("workflow store is required")
	}
	return mysql.NewGORMStore(s.db).ListRuntimeEffects(ctx, runID, f)
}

func (s *GORMStore) GetRuntimeEffect(ctx context.Context, effectID string) (*mysql.AgentEffect, error) {
	if s == nil {
		return nil, fmt.Errorf("workflow store is required")
	}
	return mysql.NewGORMStore(s.db).GetRuntimeEffect(ctx, effectID)
}

func (s *GORMStore) ListRuntimeEventsForRun(ctx context.Context, runID string) ([]mysql.WorkflowEvent, error) {
	if s == nil {
		return nil, fmt.Errorf("workflow store is required")
	}
	return mysql.NewGORMStore(s.db).ListRuntimeEventsForRun(ctx, runID)
}

func (s *GORMStore) ListRuntimeEffectEvents(ctx context.Context, runID, effectID string) ([]mysql.WorkflowEvent, error) {
	if s == nil {
		return nil, fmt.Errorf("workflow store is required")
	}
	return mysql.NewGORMStore(s.db).ListRuntimeEffectEvents(ctx, runID, effectID)
}

// ListRuntimeEffectEventsBounded exposes the bounded canonical association
// result, including truncation evidence needed for fail-closed projections.
func (s *GORMStore) ListRuntimeEffectEventsBounded(ctx context.Context, runID, effectID string) (mysql.RuntimeEffectEventQuery, error) {
	if s == nil {
		return mysql.RuntimeEffectEventQuery{}, fmt.Errorf("workflow store is required")
	}
	return mysql.NewGORMStore(s.db).ListRuntimeEffectEventsBounded(ctx, runID, effectID)
}

// ListRuntimeEvidenceSources exposes the metadata-only knowledge source
// projection used to reconstruct stable Evidence IDs.  The service performs
// the Run/Scope checks before accepting any source as visible.
func (s *GORMStore) ListRuntimeEvidenceSources(ctx context.Context) ([]mysql.RuntimeEvidenceSource, error) {
	if s == nil {
		return nil, fmt.Errorf("workflow store is required")
	}
	return mysql.NewGORMStore(s.db).ListRuntimeEvidenceSources(ctx)
}

// GetRuntimeEvidenceSource loads one current chunk, including ContentPreview
// only after the Runtime content gate has been passed by the service.
func (s *GORMStore) GetRuntimeEvidenceSource(ctx context.Context, chunkID string) (*mysql.RuntimeEvidenceSource, error) {
	if s == nil {
		return nil, fmt.Errorf("workflow store is required")
	}
	return mysql.NewGORMStore(s.db).GetRuntimeEvidenceSource(ctx, chunkID)
}

// GetRuntimeCheckpointFact exposes the metadata-only checkpoint projection;
// opaque checkpoint bytes never cross the DAO boundary.
func (s *GORMStore) GetRuntimeCheckpointFact(ctx context.Context, runID, checkpointID string) (*mysql.RuntimeCheckpointFact, error) {
	if s == nil {
		return nil, fmt.Errorf("workflow store is required")
	}
	return mysql.NewGORMStore(s.db).GetRuntimeCheckpointFact(ctx, runID, checkpointID)
}

// LoadRuntimeOperation derives an operation only from operation lifecycle
// events attached to a durable_v1 Run.  The older LoadOperation primitive is
// retained for compatibility callers, but Runtime v1 must not let an
// operation_id become a global legacy-event lookup or an authorization oracle.
func (s *GORMStore) LoadRuntimeOperation(ctx context.Context, operationID string) (Operation, error) {
	if s == nil || s.db == nil {
		return Operation{}, fmt.Errorf("workflow store is required")
	}
	if strings.TrimSpace(operationID) == "" {
		return Operation{}, fmt.Errorf("%w: empty operation_id", ErrInvalidOperationInput)
	}
	var rows []mysql.WorkflowEvent
	query := s.db.WithContext(ctx).Model(&mysql.WorkflowEvent{}).
		Joins("JOIN workflow_runs ON workflow_runs.id = workflow_events.run_id").
		Where("workflow_events.operation_id = ? AND workflow_runs.runtime_mode = ? AND workflow_runs.deleted_at IS NULL", operationID, RuntimeModeDurableV1)
	if err := query.Select("workflow_events.*").Order("workflow_events.seq ASC").Find(&rows).Error; err != nil {
		return Operation{}, fmt.Errorf("load runtime operation events: %w", err)
	}
	if len(rows) == 0 {
		return Operation{}, gorm.ErrRecordNotFound
	}
	// Scope is checked against the Run selected by the durable join before the
	// derived operation is returned.  DeriveOperation then rejects any mixed
	// Run/operation identity that may have been introduced by corrupt rows.
	if err := s.authorizeRunScope(ctx, rows[0].RunID); err != nil {
		return Operation{}, err
	}
	return DeriveOperation(rows)
}
