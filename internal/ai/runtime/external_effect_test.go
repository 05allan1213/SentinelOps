package runtime

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"SentinelOps/internal/ai/policy"
	aitools "SentinelOps/internal/ai/tools"
	"SentinelOps/internal/ai/workflow"
	appconfig "SentinelOps/internal/config"
	"SentinelOps/internal/dao/mysql"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

func TestExternalEffectApprovedResumeUsesLedgerDeadlineAndIdempotency(t *testing.T) {
	var calls atomic.Int32
	var idempotencyKey atomic.Value
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		idempotencyKey.Store(request.Header.Get("Idempotency-Key"))
		writer.Header().Set("X-Request-ID", "request-phase24-runtime")
		writer.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	oldConfig, _ := appconfig.Current()
	appconfig.SetCurrent(&appconfig.Config{})
	defer appconfig.SetCurrent(oldConfig)

	db := fixture12NewRuntimeDatabase(t, "phase24_external_resume")
	store, ctx := fixture22RuntimeContext(t, db, "phase24-external-resume", "webhook_out", policy.RiskL2)
	fixture23InstallBudgetTruth(t, db, "run-phase22-phase24-external-resume")
	chatModel := &fixture24WebhookModel{url: server.URL}
	runner, attempt := fixture24NewExternalRunnerHarness(t, store, ctx, chatModel)
	execution, err := InvokeRecoveryRunner(ctx, runner, attempt, RecoveryDecision{
		Mode: workflow.RecoveryModeReplay, ImmutableQuery: "send approved webhook",
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
		t.Fatal("external Tool call emitted no Approval interrupt")
	}
	if calls.Load() != 0 {
		t.Fatalf("external endpoint called %d times before Approval/Gate/lease revalidation", calls.Load())
	}
	durable := &DurableExecutor{store: store}
	if err := durable.publishApprovalInterrupt(ctx, attempt, interruptContexts); err != nil {
		t.Fatal(err)
	}
	var approval mysql.AgentApproval
	if err := db.First(&approval, "run_id = ?", attempt.Run.ID).Error; err != nil {
		t.Fatal(err)
	}
	approverID := "approver-phase24-runtime"
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
		Owner: "worker-phase24-runtime-resume", LeaseDuration: time.Hour,
	})
	if err != nil || !ok || claimed.Run.ID != attempt.Run.ID {
		t.Fatalf("claim approved external Run: claimed=%#v ok=%t err=%v", claimed, ok, err)
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
		ReservationIdentity: "phase24-resume-model", CatalogRef: modelSnapshot.CatalogRef,
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
	var effect mysql.AgentEffect
	if err := db.First(&effect, "run_id = ? AND effect_step = ?", attempt.Run.ID, workflow.EffectStepPrimary).Error; err != nil {
		t.Fatal(err)
	}
	key, _ := idempotencyKey.Load().(string)
	if calls.Load() != 1 || key == "" || key != effect.ID || effect.Status != workflow.EffectStatusSucceeded ||
		effect.ExternalReference == nil || *effect.ExternalReference != "request-phase24-runtime" {
		t.Fatalf("calls=%d key=%q effect=%#v", calls.Load(), key, effect)
	}
}

func fixture24NewExternalRunnerHarness(t *testing.T, store *workflow.GORMStore, ctx context.Context, chatModel model.BaseChatModel) (*adk.Runner, *AttemptContext) {
	t.Helper()
	handler, err := NewHITLRuntimeHandler(store)
	if err != nil {
		t.Fatal(err)
	}
	registered := aitools.Get("webhook_out")
	if registered == nil {
		t.Fatal("webhook_out Tool is not registered")
	}
	handlers, err := RuntimeHandlerFirst(handler)
	if err != nil {
		t.Fatal(err)
	}
	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name: "phase24_external_agent", Description: "phase24 external Effect contract", Model: chatModel,
		GenModelInput: LiteralGenModelInput, Handlers: handlers, MaxIterations: 2,
		ToolsConfig: adk.ToolsConfig{ToolsNodeConfig: compose.ToolsNodeConfig{Tools: []tool.BaseTool{registered}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := AttemptContextFromContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	runner, err := NewDurableRunner(ctx, agent, store, false)
	if err != nil {
		t.Fatal(err)
	}
	return runner, attempt
}

type fixture24WebhookModel struct {
	url   string
	calls int
}

func (m *fixture24WebhookModel) Generate(context.Context, []*schema.Message, ...model.Option) (*schema.Message, error) {
	m.calls++
	if m.calls == 1 {
		return &schema.Message{Role: schema.Assistant, ToolCalls: []schema.ToolCall{{
			ID: "phase24-webhook-call", Type: "function", Function: schema.FunctionCall{
				Name: "webhook_out", Arguments: fmt.Sprintf(`{"url":%q,"payload":"{}","method":"POST"}`, m.url),
			},
		}}}, nil
	}
	return schema.AssistantMessage("webhook persisted", nil), nil
}

func (m *fixture24WebhookModel) Stream(ctx context.Context, input []*schema.Message, options ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	message, err := m.Generate(ctx, input, options...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{message}), nil
}

func (*fixture24WebhookModel) BindTools([]*schema.ToolInfo) error { return nil }
