package runtime

// T3「真实 MCP 链路」正式实验 harness（第二部分：真实 tools/call 走 durable budget）。
//
// 这条链路复用生产原语：workflow ClaimNextRun → BuildAttemptContext → DurableBudget
// → MCPBudgetHook（ReserveMCP/SettleMCP）→ mcptools budgetTool 包装 → 真实
// Context7 tools/call。调用结束后从 MySQL 读取 budget.reserved / budget.settled
// 事件与 workflow_runs.budget_usage_json，形成"真实公网/真实 Server 调用 → durable
// 预算账本"的证据链。
//
// 本文件不修改任何产品代码。

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"SentinelOps/internal/ai/policy"
	mcptools "SentinelOps/internal/ai/tools/mcp"
	"SentinelOps/internal/ai/workflow"
	"SentinelOps/internal/dao/mysql"
	appconfig "SentinelOps/internal/config"

	"github.com/cloudwego/eino/components/tool"
)

const t3BudgetRunID = "run-t3-mcp-budget"

type t3BudgetEventFact struct {
	Seq       uint64 `json:"seq"`
	EventType string `json:"event_type"`
	Payload   string `json:"payload"`
}

// TestMCPRealCallThroughDurableBudget 让真实 MCP 调用经过 durable reservation。
func TestMCPRealCallThroughDurableBudget(t *testing.T) {
	evidenceRoot := strings.TrimSpace(os.Getenv("SENTINELOPS_T3_EVIDENCE_DIR"))
	if evidenceRoot == "" {
		t.Skip("T3 evidence harness: set SENTINELOPS_T3_EVIDENCE_DIR to run the durable budget part")
	}
	evidenceDir, err := filepath.Abs(evidenceRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(evidenceDir, 0o755); err != nil {
		t.Fatal(err)
	}

	db := killexNewRuntimeDatabase(t, "t3_mcp_budget")
	store := workflow.NewGORMStore(db)
	frozen, err := FreezeRuntimeSnapshot(fixture14SnapshotInput(t))
	if err != nil {
		t.Fatal(err)
	}
	limits := workflow.BaseBudgetLimits{
		MaxModelCalls: 3, MaxL0ToolCalls: 10, MaxDurationMS: 600_000,
		MaxMCPCalls: 4, MaxMCPConcurrency: 1, MaxMCPResultChars: 4000, MaxMCPResultBytes: 16_000,
	}
	limitsJSON, err := json.Marshal(limits)
	if err != nil {
		t.Fatal(err)
	}
	identity := policy.Identity{UserID: "operator-t3-mcp", Role: policy.RoleOperator, Scope: policy.Scope{UserID: "operator-t3-mcp"}}
	if _, err := store.CreateRunWithSessionLock(policy.WithIdentity(context.Background(), identity), workflow.CreateRunInput{
		ID: t3BudgetRunID, WorkflowKey: "t3-mcp-budget", SessionID: "session-" + t3BudgetRunID,
		QueryText: "T3 real MCP call through durable budget", ImmutableInputJSON: json.RawMessage(`{"agent":"test","query":"t3-mcp-budget"}`),
		RuntimeSnapshot: frozen.WorkflowFields(), BudgetLimitsJSON: limitsJSON,
		DeadlineAt: time.Now().Add(time.Hour), CreatedEvent: workflow.WorkflowEventInput{Type: workflow.EventRunCreated},
	}); err != nil {
		t.Fatalf("create budget run: %v", err)
	}
	claimed, ok, err := store.ClaimNextRun(context.Background(), workflow.ClaimInput{
		Owner: "t3-mcp-budget-worker", LeaseDuration: 5 * time.Minute,
		ExecutingWorkerFingerprint: frozen.CompatibilityHash(),
	})
	if err != nil || !ok || claimed == nil {
		t.Fatalf("claim budget run: ok=%v err=%v", ok, err)
	}
	budgets, err := NewDurableBudget(store)
	if err != nil {
		t.Fatal(err)
	}
	attemptCtx, attempt, err := BuildAttemptContext(context.Background(), *claimed, budgets)
	if err != nil {
		t.Fatalf("build production AttemptContext: %v", err)
	}

	server := t3BudgetServerConfig(t)
	server.Budget = &MCPBudgetHook{Attempt: attempt}
	owner, err := mcptools.NewSessionOwner(server)
	if err != nil {
		t.Fatalf("build SessionOwner with durable budget: %v", err)
	}
	defer func() { _ = owner.Close() }()
	callCtx, cancel := context.WithTimeout(attemptCtx, 90*time.Second)
	defer cancel()
	exposed, err := owner.Tools(callCtx)
	if err != nil {
		t.Fatalf("tools() with durable budget: %v", err)
	}
	target := t3SelectBudgetTool(t, callCtx, exposed, "resolve-library-id")
	invokable, ok := target.(tool.InvokableTool)
	if !ok {
		t.Fatal("resolve-library-id tool is not invokable")
	}
	started := time.Now()
	result, callErr := invokable.InvokableRun(callCtx, `{"libraryName":"Go","query":"standard library http server"}`)
	duration := time.Since(started)

	var events []t3BudgetEventFact
	if err := db.Model(&mysql.WorkflowEvent{}).
		Where("run_id = ? AND event_type IN ?", t3BudgetRunID, []string{workflow.EventBudgetReserved, workflow.EventBudgetSettled}).
		Order("seq ASC").Find(&events).Error; err != nil {
		t.Fatalf("read budget events: %v", err)
	}
	var run mysql.WorkflowRun
	if err := db.First(&run, "id = ?", t3BudgetRunID).Error; err != nil {
		t.Fatal(err)
	}
	payload := map[string]any{
		"run_id": t3BudgetRunID,
		"attempt_context": map[string]any{
			"lease_owner": attempt.Lease.Owner, "lease_generation": attempt.Lease.Generation,
			"trace_id": attempt.Trace.ID, "deadline": attempt.Deadline.UTC().Format(time.RFC3339Nano),
			"budget_limits": json.RawMessage(limitsJSON),
		},
		"mcp_call": map[string]any{
			"tool": "context7__resolve-library-id", "input": `{"libraryName":"Go","query":"standard library http server"}`,
			"success": callErr == nil, "error": t3ErrorString(callErr),
			"duration_ms": duration.Milliseconds(), "result_chars": len([]rune(result)), "result_bytes": len(result),
		},
		"budget_events": events,
		"budget_usage_json": t3RawJSON(run.BudgetUsageJSON),
		"reservations_json": t3RawJSON(run.BudgetReservationsJSON),
		"note": "真实 MCP tools/call 经 production MCPBudgetHook 预留/结算，事件与 usage 从 MySQL 真值读取",
	}
	encoded, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(evidenceDir, "budget-durable-call.json"), append(encoded, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("T3 durable budget evidence: call_success=%v duration_ms=%d budget_events=%d usage=%s",
		callErr == nil, duration.Milliseconds(), len(events), t3RawJSON(run.BudgetUsageJSON))
}

func t3BudgetServerConfig(t *testing.T) mcptools.ServerConfig {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	cfg, _, err := appconfig.LoadDirectory(filepath.Join(root, "manifest", "config"))
	if err != nil {
		t.Fatalf("load app config: %v", err)
	}
	runtimeCfg, err := mcptools.FromAppConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, server := range runtimeCfg.Servers {
		if server.Name == "context7" {
			return server
		}
	}
	t.Fatal("context7 server not configured")
	return mcptools.ServerConfig{}
}

func t3SelectBudgetTool(t *testing.T, ctx context.Context, tools []tool.BaseTool, contains string) tool.BaseTool {
	t.Helper()
	for _, base := range tools {
		info, err := base.Info(ctx)
		if err != nil {
			t.Fatalf("tool info: %v", err)
		}
		if strings.Contains(info.Name, contains) {
			return base
		}
	}
	t.Fatalf("no tool contains %q", contains)
	return nil
}

func t3ErrorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func t3RawJSON(raw *string) string {
	if raw == nil {
		return ""
	}
	return *raw
}
