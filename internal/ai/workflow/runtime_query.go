package workflow

import (
	"context"
	"fmt"

	"SentinelOps/internal/dao/mysql"
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
