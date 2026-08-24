package workflow

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"SentinelOps/internal/ai/policy"
	"SentinelOps/internal/dao/mysql"
)

func TestRuntimeSnapshotPersistenceAndContextEnvelope(t *testing.T) {
	db := newP07Database(t, "p11_snapshot_persistence")
	input := p08CreateInput("run-p11", "session-p11")
	deadline := time.Now().Add(time.Hour).UTC().Truncate(time.Millisecond)
	input.RuntimeSnapshot = RuntimeSnapshotFields{
		RuntimeVersion:           `{"app":"app-rev-1","eino":"v0.9.15","go":"go1.27.0"}`,
		RuntimeCompatibilityHash: strings.Repeat("1", 64),
		AgentRevision:            "agent-rev-1",
		ModelSnapshotJSON:        json.RawMessage(`[{"catalog_ref":"provider_a/chat"}]`),
		ToolSnapshotJSON:         json.RawMessage(`[{"name":"query_events"}]`),
		MCPCatalogHash:           strings.Repeat("2", 64),
		SkillSnapshotJSON:        json.RawMessage(`[]`),
		PromptHash:               strings.Repeat("3", 64),
		PolicyHash:               strings.Repeat("4", 64),
		ConfigHash:               strings.Repeat("5", 64),
		FeatureSnapshotJSON:      json.RawMessage(`{"agent_runtime.enabled":false}`),
	}
	input.BudgetLimitsJSON = json.RawMessage(`{"max_tokens":4096}`)
	input.DeadlineAt = deadline

	run, err := NewGORMStore(db).CreateRunWithSessionLock(p08UserContext("user-p11"), input)
	if err != nil {
		t.Fatalf("create snapshot run: %v", err)
	}
	var stored mysql.WorkflowRun
	if err := db.First(&stored, "id = ?", run.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.RuntimeVersion == nil || *stored.RuntimeVersion != input.RuntimeSnapshot.RuntimeVersion ||
		stored.RuntimeCompatibilityHash == nil || *stored.RuntimeCompatibilityHash != input.RuntimeSnapshot.RuntimeCompatibilityHash ||
		stored.AgentRevision == nil || *stored.AgentRevision != input.RuntimeSnapshot.AgentRevision ||
		stored.ModelSnapshot == nil || !p08JSONEqual(*stored.ModelSnapshot, string(input.RuntimeSnapshot.ModelSnapshotJSON)) ||
		stored.ToolSnapshot == nil || !p08JSONEqual(*stored.ToolSnapshot, string(input.RuntimeSnapshot.ToolSnapshotJSON)) ||
		stored.MCPCatalogHash == nil || *stored.MCPCatalogHash != input.RuntimeSnapshot.MCPCatalogHash ||
		stored.SkillSnapshot == nil || !p08JSONEqual(*stored.SkillSnapshot, string(input.RuntimeSnapshot.SkillSnapshotJSON)) ||
		stored.PromptHash == nil || stored.PolicyHash == nil || stored.ConfigHash == nil || stored.FeatureSnapshot == nil {
		t.Fatalf("runtime snapshot fields not persisted: %+v", stored)
	}
	if stored.ContextSnapshotJSON == nil {
		t.Fatal("context snapshot is nil")
	}
	var contextSnapshot DurableContextSnapshot
	if err := json.Unmarshal([]byte(*stored.ContextSnapshotJSON), &contextSnapshot); err != nil {
		t.Fatalf("decode durable context snapshot: %v", err)
	}
	if contextSnapshot.Schema != DurableContextSnapshotSchema || contextSnapshot.Identity.UserID != "user-p11" ||
		contextSnapshot.Identity.Role != string(policy.RoleViewer) || contextSnapshot.Identity.Scope.UserID != "user-p11" ||
		!p08JSONEqual(string(contextSnapshot.BudgetLimits), string(input.BudgetLimitsJSON)) || !contextSnapshot.DeadlineAt.Equal(deadline) {
		t.Fatalf("durable context snapshot = %+v", contextSnapshot)
	}
}
