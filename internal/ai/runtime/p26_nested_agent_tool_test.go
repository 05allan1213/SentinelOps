package runtime

import (
	"context"
	"testing"
	"time"

	"SentinelOps/internal/ai/policy"
	aitools "SentinelOps/internal/ai/tools"
	"SentinelOps/internal/ai/workflow"
	"SentinelOps/internal/dao/mysql"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

func TestNestedLedgerAgentToolLeafCreatesOnePrimary(t *testing.T) {
	db := p12NewRuntimeDatabase(t, "p26_nested_agent_tool")
	store, ctx := p22RuntimeContext(t, db, "p26-nested-agent-tool", "create_report", policy.RiskL1, "report_agent")
	p23InstallBudgetTruth(t, db, "run-p22-p26-nested-agent-tool")
	handler, err := NewHITLRuntimeHandler(store)
	if err != nil {
		t.Fatal(err)
	}
	leaf := aitools.Get("create_report")
	if leaf == nil {
		t.Fatal("create_report Tool is not registered")
	}
	handlers, err := RuntimeHandlerFirst(handler)
	if err != nil {
		t.Fatal(err)
	}
	inner, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name: "report_agent", Description: "P26 nested report specialist", Model: &p23CreateReportModel{},
		GenModelInput: LiteralGenModelInput, Handlers: handlers, MaxIterations: 2,
		ToolsConfig: adk.ToolsConfig{ToolsNodeConfig: compose.ToolsNodeConfig{Tools: []tool.BaseTool{leaf}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	root, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name: "p26_root", Description: "P26 AgentTool root", Model: &p26RootAgentToolModel{},
		GenModelInput: LiteralGenModelInput, Handlers: handlers, MaxIterations: 2,
		ToolsConfig: adk.ToolsConfig{ToolsNodeConfig: compose.ToolsNodeConfig{Tools: []tool.BaseTool{adk.NewAgentTool(ctx, inner)}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	runner, err := NewDurableRunner(ctx, root, store, false)
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := AttemptContextFromContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	execution, err := InvokeRecoveryRunner(ctx, runner, attempt, RecoveryDecision{
		Mode: workflow.RecoveryModeReplay, ImmutableQuery: "create one nested report",
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
		t.Fatal("nested leaf emitted no Approval interrupt")
	}
	durable := &DurableExecutor{store: store}
	if err := durable.publishApprovalInterrupt(ctx, attempt, interruptContexts); err != nil {
		t.Fatal(err)
	}
	var approval mysql.AgentApproval
	if err := db.First(&approval, "run_id = ?", attempt.Run.ID).Error; err != nil {
		t.Fatal(err)
	}
	approverCtx := policy.WithIdentity(context.Background(), policy.Identity{
		UserID: "approver-p26-nested", Role: policy.RoleApprover, Scope: policy.Scope{UserID: "approver-p26-nested"},
	})
	if _, err := store.DecideApprovalAndWakeRun(approverCtx, workflow.DecideApprovalInput{
		ApprovalID: approval.ID, ProposalHash: approval.ProposalHash,
		ExpectedVersion: approval.Version, Decision: workflow.ApprovalStatusApproved,
	}); err != nil {
		t.Fatal(err)
	}
	claimed, ok, err := store.ClaimNextRun(context.Background(), workflow.ClaimInput{
		Owner: "worker-p26-nested-resume", LeaseDuration: time.Hour,
	})
	if err != nil || !ok || claimed.Run.ID != attempt.Run.ID {
		t.Fatalf("claim nested approved Run: claimed=%#v ok=%t err=%v", claimed, ok, err)
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
		ReservationIdentity: "p26-nested-resume-model", CatalogRef: modelSnapshot.CatalogRef,
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

	var reportCount, effectCount, primaryCount int64
	if err := db.Model(&mysql.Report{}).Where("title = ?", "P23 transactional report").Count(&reportCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&mysql.AgentEffect{}).Where("run_id = ?", attempt.Run.ID).Count(&effectCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&mysql.AgentEffect{}).Where("run_id = ? AND effect_role = ?", attempt.Run.ID, workflow.EffectRolePrimary).Count(&primaryCount).Error; err != nil {
		t.Fatal(err)
	}
	if reportCount != 1 || effectCount != 1 || primaryCount != 1 {
		t.Fatalf("nested AgentTool reports=%d effects=%d primary=%d", reportCount, effectCount, primaryCount)
	}
}

type p26RootAgentToolModel struct{ calls int }

func (m *p26RootAgentToolModel) Generate(context.Context, []*schema.Message, ...model.Option) (*schema.Message, error) {
	m.calls++
	if m.calls == 1 {
		return &schema.Message{Role: schema.Assistant, ToolCalls: []schema.ToolCall{{
			ID: "p26-report-agent-call", Type: "function",
			Function: schema.FunctionCall{Name: "report_agent", Arguments: `{"request":"create one report"}`},
		}}}, nil
	}
	return schema.AssistantMessage("nested report persisted", nil), nil
}

func (m *p26RootAgentToolModel) Stream(ctx context.Context, input []*schema.Message, options ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	message, err := m.Generate(ctx, input, options...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{message}), nil
}

func (*p26RootAgentToolModel) BindTools([]*schema.ToolInfo) error { return nil }
