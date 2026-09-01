package workflow

import (
	"SentinelOps/internal/dao/mysql"
	"context"
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
