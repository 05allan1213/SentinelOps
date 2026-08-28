package runtime

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"SentinelOps/internal/ai/policy"
	"SentinelOps/internal/ai/workflow"
	"SentinelOps/internal/dao/mysql"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"gorm.io/gorm"
)

func TestTransactionalEffectApprovedResumeCallsOriginalEndpointOnce(t *testing.T) {
	db := fixture12NewRuntimeDatabase(t, "phase23_approved_resume")
	store, ctx := fixture22RuntimeContext(t, db, "phase23-approved-resume", "create_report", policy.RiskL1)
	fixture23InstallBudgetTruth(t, db, "run-phase22-phase23-approved-resume")
	chatModel := &fixture23CreateReportModel{}
	runner, attempt := fixture22NewRunnerHarnessWithModel(t, store, ctx, store, chatModel)
	execution, err := InvokeRecoveryRunner(ctx, runner, attempt, RecoveryDecision{
		Mode: workflow.RecoveryModeReplay, ImmutableQuery: "create a transactional report",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var interruptContexts []*adk.InterruptCtx
	for {
		event, ok := execution.Events.Next()
		if !ok {
			break
		}
		if event.Err != nil {
			t.Fatal(event.Err)
		}
		if event.Action != nil && event.Action.Interrupted != nil {
			interruptContexts = event.Action.Interrupted.InterruptContexts
		}
	}
	if len(interruptContexts) == 0 {
		t.Fatal("initial transactional Tool call emitted no Approval interrupt")
	}
	durable := &DurableExecutor{store: store}
	if err := durable.publishApprovalInterrupt(ctx, attempt, interruptContexts); err != nil {
		t.Fatal(err)
	}
	var approval mysql.AgentApproval
	if err := db.First(&approval, "run_id = ?", attempt.Run.ID).Error; err != nil {
		t.Fatal(err)
	}
	approverID := "approver-phase23-runtime"
	approverCtx := policy.WithIdentity(context.Background(), policy.Identity{
		UserID: approverID, Role: policy.RoleApprover, Scope: policy.Scope{UserID: approverID},
	})
	if _, err := store.DecideApprovalAndWakeRun(approverCtx, workflow.DecideApprovalInput{
		ApprovalID: approval.ID, ProposalHash: approval.ProposalHash,
		ExpectedVersion: approval.Version, Decision: workflow.ApprovalStatusApproved,
	}); err != nil {
		t.Fatal(err)
	}
	claimed, ok, err := store.ClaimNextRun(context.Background(), workflow.ClaimInput{
		Owner: "worker-phase23-runtime-resume", LeaseDuration: time.Hour,
	})
	if err != nil || !ok || claimed.Run.ID != attempt.Run.ID {
		t.Fatalf("claim approved transactional Run: claimed=%#v ok=%t err=%v", claimed, ok, err)
	}
	budgets, err := NewDurableBudget(store)
	if err != nil {
		t.Fatal(err)
	}
	resumeCtx, resumeAttempt, err := BuildAttemptContext(context.Background(), *claimed, budgets)
	if err != nil {
		t.Fatal(err)
	}
	modelSnapshot := resumeAttempt.Snapshot.Models()[0]
	resumeCtx, err = WithModelInvocation(resumeCtx, ModelInvocation{
		ReservationIdentity: "phase23-resume-model", CatalogRef: modelSnapshot.CatalogRef,
		Provider: modelSnapshot.Provider, Driver: modelSnapshot.Driver, ModelID: modelSnapshot.ModelID,
		Profile: modelSnapshot.Profile, SnapshotIdentity: modelSnapshot.Identity(),
	})
	if err != nil {
		t.Fatal(err)
	}
	params, err := durable.approvalResumeParams(resumeCtx, resumeAttempt)
	if err != nil {
		t.Fatal(err)
	}
	checkpointID, err := workflow.EinoCheckpointID(attempt.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	resumed, err := InvokeRecoveryRunner(resumeCtx, runner, resumeAttempt, RecoveryDecision{
		Mode: workflow.RecoveryModeResume, CheckpointID: checkpointID,
	}, params)
	if err != nil {
		t.Fatal(err)
	}
	for {
		event, ok := resumed.Events.Next()
		if !ok {
			break
		}
		if event.Err != nil {
			t.Fatal(event.Err)
		}
	}

	var reportCount, primaryCount, unknownCount int64
	if err := db.Model(&mysql.Report{}).Where("title = ?", "phase23 transactional report").Count(&reportCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&mysql.AgentEffect{}).Where("run_id = ? AND effect_role = ? AND status = ?", attempt.Run.ID, workflow.EffectRolePrimary, workflow.EffectStatusSucceeded).Count(&primaryCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&mysql.AgentEffect{}).Where("run_id = ? AND status = ?", attempt.Run.ID, workflow.EffectStatusUnknown).Count(&unknownCount).Error; err != nil {
		t.Fatal(err)
	}
	if reportCount != 1 || primaryCount != 1 || unknownCount != 0 || chatModel.calls != 2 {
		t.Fatalf("approved Resume truth reports=%d primary=%d unknown=%d model_calls=%d", reportCount, primaryCount, unknownCount, chatModel.calls)
	}
}

func fixture23InstallBudgetTruth(t *testing.T, db *gorm.DB, runID string) {
	t.Helper()
	limits := json.RawMessage(`{"max_model_calls":10,"max_l0_tool_calls":10,"max_duration_ms":3600000}`)
	usage := `{"schema":"sentinelops/run-base-budget/v1","model_calls":0,"l0_tool_calls":0}`
	reservations := `{"schema":"sentinelops/run-base-budget/v1","items":{}}`
	var run mysql.WorkflowRun
	if err := db.Model(&mysql.WorkflowRun{}).Where("id = ?", runID).First(&run).Error; err != nil {
		t.Fatal(err)
	}
	if run.ContextSnapshotJSON == nil {
		t.Fatal("phase23 fixture missing context snapshot")
	}
	var snapshot workflow.DurableContextSnapshot
	if err := json.Unmarshal([]byte(*run.ContextSnapshotJSON), &snapshot); err != nil {
		t.Fatal(err)
	}
	snapshot.BudgetLimits = limits
	contextJSON, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&mysql.WorkflowRun{}).Where("id = ?", runID).Updates(map[string]any{
		"budget_limits_json": string(limits), "budget_usage_json": usage,
		"budget_reservations_json": reservations, "context_snapshot_json": string(contextJSON),
	}).Error; err != nil {
		t.Fatal(err)
	}
}

type fixture23CreateReportModel struct {
	calls int
}

func (m *fixture23CreateReportModel) Generate(context.Context, []*schema.Message, ...model.Option) (*schema.Message, error) {
	m.calls++
	if m.calls == 1 {
		return &schema.Message{Role: schema.Assistant, ToolCalls: []schema.ToolCall{{
			ID: "phase23-create-report-call", Type: "function",
			Function: schema.FunctionCall{Name: "create_report", Arguments: `{"title":"phase23 transactional report","content":"atomic","type":"custom"}`},
		}}}, nil
	}
	return schema.AssistantMessage("report persisted", nil), nil
}

func (m *fixture23CreateReportModel) Stream(ctx context.Context, input []*schema.Message, options ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	message, err := m.Generate(ctx, input, options...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{message}), nil
}

func (*fixture23CreateReportModel) BindTools([]*schema.ToolInfo) error { return nil }
