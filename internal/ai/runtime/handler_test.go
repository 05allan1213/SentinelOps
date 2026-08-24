package runtime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"sync"
	"testing"
	"time"

	"SentinelOps/internal/ai/policy"
	"SentinelOps/internal/ai/workflow"
	appconfig "SentinelOps/internal/config"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

func TestRuntimeHandlerCoversAllEndpoints(t *testing.T) {
	handler := NewRuntimeHandler()
	budget := newP14RecordingBudget()
	ctx, invocation := p14InvocationContext(t, "run-five-endpoints", "user-five", budget)
	ctx, err := WithModelInvocation(ctx, invocation)
	if err != nil {
		t.Fatal(err)
	}

	modelEndpoint := &p14Model{}
	wrappedModel, err := handler.WrapModel(ctx, modelEndpoint, &adk.ModelContext{})
	if err != nil {
		t.Fatalf("wrap model: %v", err)
	}
	if _, err = wrappedModel.Generate(ctx, []*schema.Message{schema.UserMessage("inspect")}); err != nil {
		t.Fatalf("invoke wrapped model: %v", err)
	}

	invokableCalls := 0
	invokable, err := handler.WrapInvokableToolCall(ctx, func(callCtx context.Context, _ string, _ ...tool.Option) (string, error) {
		p14RequireCallMetadata(t, callCtx, BudgetCallKindL0Tool, "query_events")
		invokableCalls++
		return "ok", nil
	}, &adk.ToolContext{Name: "query_events", CallID: "call-invokable"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = invokable(ctx, `{}`); err != nil {
		t.Fatal(err)
	}

	streamableCalls := 0
	streamable, err := handler.WrapStreamableToolCall(ctx, func(callCtx context.Context, _ string, _ ...tool.Option) (*schema.StreamReader[string], error) {
		p14RequireCallMetadata(t, callCtx, BudgetCallKindL0Tool, "query_events")
		streamableCalls++
		return schema.StreamReaderFromArray([]string{"ok"}), nil
	}, &adk.ToolContext{Name: "query_events", CallID: "call-streamable"})
	if err != nil {
		t.Fatal(err)
	}
	stream, err := streamable(ctx, `{}`)
	if err != nil {
		t.Fatal(err)
	}
	p14DrainStream(t, stream)

	enhancedCalls := 0
	enhanced, err := handler.WrapEnhancedInvokableToolCall(ctx, func(callCtx context.Context, _ *schema.ToolArgument, _ ...tool.Option) (*schema.ToolResult, error) {
		p14RequireCallMetadata(t, callCtx, BudgetCallKindL0Tool, "query_events")
		enhancedCalls++
		return &schema.ToolResult{}, nil
	}, &adk.ToolContext{Name: "query_events", CallID: "call-enhanced"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = enhanced(ctx, &schema.ToolArgument{Text: `{}`}); err != nil {
		t.Fatal(err)
	}

	enhancedStreamCalls := 0
	enhancedStream, err := handler.WrapEnhancedStreamableToolCall(ctx, func(callCtx context.Context, _ *schema.ToolArgument, _ ...tool.Option) (*schema.StreamReader[*schema.ToolResult], error) {
		p14RequireCallMetadata(t, callCtx, BudgetCallKindL0Tool, "query_events")
		enhancedStreamCalls++
		return schema.StreamReaderFromArray([]*schema.ToolResult{{}}), nil
	}, &adk.ToolContext{Name: "query_events", CallID: "call-enhanced-stream"})
	if err != nil {
		t.Fatal(err)
	}
	enhancedResultStream, err := enhancedStream(ctx, &schema.ToolArgument{Text: `{}`})
	if err != nil {
		t.Fatal(err)
	}
	p14DrainStream(t, enhancedResultStream)

	if modelEndpoint.calls != 1 || invokableCalls != 1 || streamableCalls != 1 || enhancedCalls != 1 || enhancedStreamCalls != 1 {
		t.Fatalf("endpoint calls = model %d invokable %d streamable %d enhanced %d enhanced_stream %d",
			modelEndpoint.calls, invokableCalls, streamableCalls, enhancedCalls, enhancedStreamCalls)
	}
	if reserved, settled := budget.counts(); reserved != 5 || settled != 5 {
		t.Fatalf("budget calls = reserved %d settled %d, want 5/5", reserved, settled)
	}
	p14RequireAllEndpointsRejectMissingContext(t, handler)
}

func TestRuntimeHandlerRejectsPolicyScopeDeadlineBeforeEndpoint(t *testing.T) {
	handler := NewRuntimeHandler()
	endpointCalls := 0
	endpoint := func(context.Context, string, ...tool.Option) (string, error) {
		endpointCalls++
		return "unexpected", nil
	}

	ctx, _ := p14InvocationContext(t, "run-policy", "user-policy", newP14RecordingBudget())
	for _, testCase := range []struct {
		name string
		ctx  context.Context
		tool adk.ToolContext
		want error
	}{
		{name: "unknown", ctx: ctx, tool: adk.ToolContext{Name: "dynamic_unknown", CallID: "unknown"}, want: policy.ErrUnknownCatalogTool},
		{name: "mutation", ctx: ctx, tool: adk.ToolContext{Name: "create_report", CallID: "mutation"}, want: policy.ErrMutationDisabled},
		{name: "scope", ctx: p14TamperScope(t, ctx), tool: adk.ToolContext{Name: "query_events", CallID: "scope"}, want: policy.ErrForbidden},
		{name: "deadline", ctx: p14ExpireDeadline(t, ctx), tool: adk.ToolContext{Name: "query_events", CallID: "deadline"}, want: context.DeadlineExceeded},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			wrapped, err := handler.WrapInvokableToolCall(testCase.ctx, endpoint, &testCase.tool)
			if err != nil {
				t.Fatalf("wrap endpoint: %v", err)
			}
			if _, err = wrapped(testCase.ctx, `{}`); !errors.Is(err, testCase.want) {
				t.Fatalf("error = %v, want %v", err, testCase.want)
			}
		})
	}
	if endpointCalls != 0 {
		t.Fatalf("rejected endpoint calls = %d, want 0", endpointCalls)
	}
}

func TestHandlerConcurrentIsolation(t *testing.T) {
	handler := NewRuntimeHandler()
	const runs = 128
	frozen, freezeErr := FreezeRuntimeSnapshot(p14SnapshotInput(t))
	if freezeErr != nil {
		t.Fatal(freezeErr)
	}
	start := make(chan struct{})
	errorsCh := make(chan error, runs)
	var wait sync.WaitGroup
	for index := 0; index < runs; index++ {
		index := index
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			runID := fmt.Sprintf("run-concurrent-%03d", index)
			userID := fmt.Sprintf("user-concurrent-%03d", index)
			budget := newP14RecordingBudget()
			ctx, _ := p14InvocationContextWithSnapshot(t, runID, userID, uint64(index+1), budget, frozen)
			wrapped, err := handler.WrapInvokableToolCall(ctx, func(endpointCtx context.Context, _ string, _ ...tool.Option) (string, error) {
				metadata, metadataErr := CallMetadataFromContext(endpointCtx)
				if metadataErr != nil {
					return "", metadataErr
				}
				if metadata.RunID != runID || metadata.UserID != userID || metadata.LeaseGeneration != uint64(index+1) {
					return "", fmt.Errorf("cross-run metadata: %+v", metadata)
				}
				return "ok", nil
			}, &adk.ToolContext{Name: "query_events", CallID: fmt.Sprintf("call-%03d", index)})
			if err == nil {
				_, err = wrapped(ctx, `{}`)
			}
			if err == nil {
				reserved, settled := budget.counts()
				if reserved != 1 || settled != 1 {
					err = fmt.Errorf("run %s budget counts = %d/%d", runID, reserved, settled)
				}
			}
			errorsCh <- err
		}()
	}
	close(start)
	wait.Wait()
	close(errorsCh)
	for err := range errorsCh {
		if err != nil {
			t.Fatal(err)
		}
	}

	handlerType := reflect.TypeOf(*handler)
	for index := 0; index < handlerType.NumField(); index++ {
		name := handlerType.Field(index).Name
		for _, forbidden := range []string{"currentRun", "currentUser", "currentBudget", "currentLease", "currentApproval"} {
			if name == forbidden {
				t.Fatalf("RuntimeHandler contains request-global field %q", name)
			}
		}
	}
}

func TestDynamicToolPolicyAndRuntimeHandlerOrder(t *testing.T) {
	handler := NewRuntimeHandler()
	ordered, err := RuntimeHandlerFirst(handler, &adk.BaseChatModelAgentMiddleware{})
	if err != nil {
		t.Fatal(err)
	}
	if len(ordered) != 2 || ordered[0] != handler {
		t.Fatalf("handler order = %#v, RuntimeHandler must be first", ordered)
	}

	input := p14SnapshotInput(t)
	frameworkEntry, err := policy.LookupCatalog("trigger_ops")
	if err != nil {
		t.Fatal(err)
	}
	if frameworkEntry.Risk != policy.RiskL0 || frameworkEntry.EffectType != "" || len(frameworkEntry.EffectSteps) != 0 {
		t.Fatalf("nested framework Tool must not carry Effect metadata: %+v", frameworkEntry)
	}
	input.Tools = append(input.Tools, ToolSnapshot{
		Name: "trigger_ops", Revision: frameworkEntry.Revision, SchemaHash: frameworkEntry.SchemaHash,
	})
	frozen, err := FreezeRuntimeSnapshot(input)
	if err != nil {
		t.Fatal(err)
	}
	budget := newP14RecordingBudget()
	ctx, _ := p14InvocationContextWithSnapshot(t, "run-dynamic", "user-dynamic", 1, budget, frozen)
	frameworkCalls := 0
	framework, err := handler.WrapInvokableToolCall(ctx, func(callCtx context.Context, _ string, _ ...tool.Option) (string, error) {
		p14RequireCallMetadata(t, callCtx, BudgetCallKindL0Tool, "trigger_ops")
		frameworkCalls++
		return "proposal-only", nil
	}, &adk.ToolContext{Name: "trigger_ops", CallID: "dynamic-framework"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = framework(ctx, `{}`); err != nil {
		t.Fatal(err)
	}
	if reserved, settled := budget.counts(); frameworkCalls != 1 || reserved != 1 || settled != 1 {
		t.Fatalf("nested framework Tool calls/budget = %d/%d/%d, want 1/1/1", frameworkCalls, reserved, settled)
	}

	endpointCalls := 0
	for _, dynamic := range []adk.ToolContext{
		{Name: "create_report", CallID: "dynamic-mutation"},
		{Name: "not_in_before_agent", CallID: "dynamic-unknown"},
		{Name: "web_search", CallID: "dynamic-not-frozen"},
	} {
		wrapped, wrapErr := handler.WrapInvokableToolCall(ctx, func(context.Context, string, ...tool.Option) (string, error) {
			endpointCalls++
			return "unexpected", nil
		}, &dynamic)
		if wrapErr != nil {
			t.Fatal(wrapErr)
		}
		if _, callErr := wrapped(ctx, `{}`); callErr == nil {
			t.Fatalf("dynamic tool %q bypassed per-call policy", dynamic.Name)
		}
	}
	if endpointCalls != 0 {
		t.Fatalf("dynamic rejected endpoint calls = %d", endpointCalls)
	}
	p14RequireBeforeAgentDynamicToolRejected(t, handler)
}

func TestRuntimeHandlerFirstIsOutermostInEinoAgent(t *testing.T) {
	handler := NewRuntimeHandler()
	probe := &p14MetadataProbeHandler{BaseChatModelAgentMiddleware: &adk.BaseChatModelAgentMiddleware{}}
	handlers, err := RuntimeHandlerFirst(handler, probe)
	if err != nil {
		t.Fatal(err)
	}
	budget := newP14RecordingBudget()
	ctx, invocation := p14InvocationContext(t, "run-eino-order", "user-eino-order", budget)
	ctx, err = WithModelInvocation(ctx, invocation)
	if err != nil {
		t.Fatal(err)
	}
	endpoint := &p14Model{}
	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name: "p14-handler-order", Description: "P14 Handler order contract", Model: endpoint,
		GenModelInput: LiteralGenModelInput, Handlers: handlers,
	})
	if err != nil {
		t.Fatal(err)
	}
	runner := adk.NewRunner(ctx, adk.RunnerConfig{Agent: agent})
	p12DrainRuntimeEvents(t, runner.Query(ctx, "verify outermost RuntimeHandler"))
	if probe.calls != 1 || endpoint.calls != 1 {
		t.Fatalf("probe/endpoint calls = %d/%d, want 1/1", probe.calls, endpoint.calls)
	}
	if reserved, settled := budget.counts(); reserved != 1 || settled != 1 {
		t.Fatalf("real Eino wrapper budget calls = %d/%d, want 1/1", reserved, settled)
	}
}

func TestRuntimeHandlerModelMetadataUsesProviderQualifiedSnapshot(t *testing.T) {
	input := p14SnapshotInput(t)
	input.Models = []ModelSnapshot{
		{Kind: "chat", Profile: "default", CatalogRef: "provider_a/shared", Provider: "provider_a", Driver: appconfig.DriverOpenAICompatibleChat, ModelID: "same-vendor-id", Pricing: PricingSnapshot{Revision: "price-a", Currency: "CNY", Unit: "per_million_tokens", Input: 1}},
		{Kind: "chat", Profile: "reasoning", CatalogRef: "provider_b/shared", Provider: "provider_b", Driver: appconfig.DriverOpenAICompatibleChat, ModelID: "same-vendor-id", Pricing: PricingSnapshot{Revision: "price-b", Currency: "CNY", Unit: "per_million_tokens", Input: 2}},
	}
	frozen, err := FreezeRuntimeSnapshot(input)
	if err != nil {
		t.Fatal(err)
	}
	budget := newP14RecordingBudget()
	ctx, _ := p14InvocationContextWithSnapshot(t, "run-model-identity", "user-model", 9, budget, frozen)
	modelSnapshot := input.Models[1]
	ctx, err = WithModelInvocation(ctx, ModelInvocation{
		ReservationIdentity: "physical-call-provider-b", CatalogRef: modelSnapshot.CatalogRef,
		Provider: modelSnapshot.Provider, Driver: modelSnapshot.Driver, ModelID: modelSnapshot.ModelID,
		Profile: modelSnapshot.Profile, SnapshotIdentity: modelSnapshot.Identity(),
	})
	if err != nil {
		t.Fatal(err)
	}
	inner := &p14Model{validate: func(callCtx context.Context) error {
		metadata, metadataErr := CallMetadataFromContext(callCtx)
		if metadataErr != nil {
			return metadataErr
		}
		if metadata.Model == nil || metadata.Model.CatalogRef != "provider_b/shared" || metadata.Model.Provider != "provider_b" ||
			metadata.Model.SnapshotIdentity == "" || metadata.Model.ModelID != "same-vendor-id" {
			return fmt.Errorf("model metadata = %+v", metadata.Model)
		}
		return nil
	}}
	wrapped, err := NewRuntimeHandler().WrapModel(ctx, inner, &adk.ModelContext{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = wrapped.Generate(ctx, []*schema.Message{schema.UserMessage("same model id")}); err != nil {
		t.Fatal(err)
	}

	wrongCtx, err := WithModelInvocation(ctx, ModelInvocation{
		ReservationIdentity: "wrong-provider", CatalogRef: "provider_a/shared", Provider: "provider_b",
		Driver: modelSnapshot.Driver, ModelID: modelSnapshot.ModelID, Profile: "default", SnapshotIdentity: modelSnapshot.Identity(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = wrapped.Generate(wrongCtx, []*schema.Message{schema.UserMessage("tampered")}); err == nil {
		t.Fatal("tampered provider-qualified model identity reached endpoint")
	}
	if inner.calls != 1 {
		t.Fatalf("tampered model endpoint calls = %d, want 1 prior valid call only", inner.calls)
	}
	secretInvocation := ModelInvocation{
		ReservationIdentity: "secret-call", CatalogRef: modelSnapshot.CatalogRef,
		Provider: "sk-12345678901234567890", Driver: modelSnapshot.Driver, ModelID: modelSnapshot.ModelID,
		Profile: modelSnapshot.Profile, SnapshotIdentity: modelSnapshot.Identity(),
	}
	if _, err = WithModelInvocation(ctx, secretInvocation); err == nil {
		t.Fatal("sensitive material entered Model invocation metadata")
	}
}

func TestRuntimeHandlerModelStreamSettlesAfterConsumption(t *testing.T) {
	budget := newP14RecordingBudget()
	ctx, invocation := p14InvocationContext(t, "run-model-stream", "user-model-stream", budget)
	ctx, err := WithModelInvocation(ctx, invocation)
	if err != nil {
		t.Fatal(err)
	}
	wrapped, err := NewRuntimeHandler().WrapModel(ctx, &p14Model{}, &adk.ModelContext{})
	if err != nil {
		t.Fatal(err)
	}
	stream, err := wrapped.Stream(ctx, []*schema.Message{schema.UserMessage("stream")})
	if err != nil {
		t.Fatal(err)
	}
	if reserved, settled := budget.counts(); reserved != 1 || settled != 0 {
		t.Fatalf("budget before stream consumption = %d/%d, want 1/0", reserved, settled)
	}
	p14DrainStream(t, stream)
	if reserved, settled := budget.counts(); reserved != 1 || settled != 1 {
		t.Fatalf("budget after stream consumption = %d/%d, want 1/1", reserved, settled)
	}
}

type p14Model struct {
	calls    int
	validate func(context.Context) error
}

type p14MetadataProbeHandler struct {
	*adk.BaseChatModelAgentMiddleware
	calls int
}

func (h *p14MetadataProbeHandler) WrapModel(_ context.Context, endpoint model.BaseChatModel, _ *adk.ModelContext) (model.BaseChatModel, error) {
	return &p14MetadataProbeModel{handler: h, endpoint: endpoint}, nil
}

type p14MetadataProbeModel struct {
	handler  *p14MetadataProbeHandler
	endpoint model.BaseChatModel
}

func (m *p14MetadataProbeModel) Generate(ctx context.Context, input []*schema.Message, options ...model.Option) (*schema.Message, error) {
	if _, err := CallMetadataFromContext(ctx); err != nil {
		return nil, fmt.Errorf("RuntimeHandler is not the outermost user Model wrapper: %w", err)
	}
	m.handler.calls++
	return m.endpoint.Generate(ctx, input, options...)
}

func (m *p14MetadataProbeModel) Stream(ctx context.Context, input []*schema.Message, options ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	if _, err := CallMetadataFromContext(ctx); err != nil {
		return nil, fmt.Errorf("RuntimeHandler is not the outermost user Model wrapper: %w", err)
	}
	m.handler.calls++
	return m.endpoint.Stream(ctx, input, options...)
}

type p14DynamicToolHandler struct {
	*adk.BaseChatModelAgentMiddleware
	tool tool.BaseTool
}

func (h *p14DynamicToolHandler) BeforeAgent(ctx context.Context, runContext *adk.ChatModelAgentContext) (context.Context, *adk.ChatModelAgentContext, error) {
	runContext.Tools = append(runContext.Tools, h.tool)
	return ctx, runContext, nil
}

type p14DynamicUnknownTool struct{ calls int }

func (*p14DynamicUnknownTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "dynamic_before_agent_unknown", Desc: "P14 dynamic Tool policy contract",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{}),
	}, nil
}

func (t *p14DynamicUnknownTool) InvokableRun(context.Context, string, ...tool.Option) (string, error) {
	t.calls++
	return "unsafe", nil
}

type p14DynamicToolCallingModel struct{ calls int }

func (m *p14DynamicToolCallingModel) Generate(context.Context, []*schema.Message, ...model.Option) (*schema.Message, error) {
	m.calls++
	if m.calls == 1 {
		return &schema.Message{Role: schema.Assistant, ToolCalls: []schema.ToolCall{{
			ID: "dynamic-before-agent-call", Type: "function",
			Function: schema.FunctionCall{Name: "dynamic_before_agent_unknown", Arguments: `{}`},
		}}}, nil
	}
	return schema.AssistantMessage("unexpected", nil), nil
}

func (m *p14DynamicToolCallingModel) Stream(ctx context.Context, input []*schema.Message, options ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	message, err := m.Generate(ctx, input, options...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{message}), nil
}

func (*p14DynamicToolCallingModel) BindTools([]*schema.ToolInfo) error { return nil }

func (m *p14Model) Generate(ctx context.Context, _ []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	m.calls++
	if m.validate != nil {
		if err := m.validate(ctx); err != nil {
			return nil, err
		}
	}
	return schema.AssistantMessage("ok", nil), nil
}

func (m *p14Model) Stream(ctx context.Context, _ []*schema.Message, _ ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	m.calls++
	if m.validate != nil {
		if err := m.validate(ctx); err != nil {
			return nil, err
		}
	}
	return schema.StreamReaderFromArray([]*schema.Message{schema.AssistantMessage("ok", nil)}), nil
}

type p14RecordingBudget struct {
	mu           sync.Mutex
	reservations map[string]BudgetCall
	settled      map[string]BudgetSettlement
}

func newP14RecordingBudget() *p14RecordingBudget {
	return &p14RecordingBudget{reservations: map[string]BudgetCall{}, settled: map[string]BudgetSettlement{}}
}

func (*p14RecordingBudget) RuntimeBudgetHandle() {}

func (b *p14RecordingBudget) ReserveCall(_ context.Context, call BudgetCall) (BudgetReservation, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !time.Now().Before(call.Deadline) {
		return BudgetReservation{}, context.DeadlineExceeded
	}
	if existing, ok := b.reservations[call.ReservationIdentity]; ok {
		if !reflect.DeepEqual(existing, call) {
			return BudgetReservation{}, workflow.ErrBudgetReservationConflict
		}
		return BudgetReservation{Identity: call.ReservationIdentity}, nil
	}
	b.reservations[call.ReservationIdentity] = call
	return BudgetReservation{Identity: call.ReservationIdentity}, nil
}

func (b *p14RecordingBudget) SettleCall(_ context.Context, settlement BudgetSettlement) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.reservations[settlement.ReservationIdentity]; !ok {
		return fmt.Errorf("unknown reservation %q", settlement.ReservationIdentity)
	}
	b.settled[settlement.ReservationIdentity] = settlement
	return nil
}

func (b *p14RecordingBudget) counts() (int, int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.reservations), len(b.settled)
}

func p14InvocationContext(t *testing.T, runID, userID string, budget BudgetHandle) (context.Context, ModelInvocation) {
	t.Helper()
	frozen, err := FreezeRuntimeSnapshot(p14SnapshotInput(t))
	if err != nil {
		t.Fatal(err)
	}
	return p14InvocationContextWithSnapshot(t, runID, userID, 1, budget, frozen)
}

func p14InvocationContextWithSnapshot(t *testing.T, runID, userID string, generation uint64, budget BudgetHandle, frozen FrozenRuntimeSnapshot) (context.Context, ModelInvocation) {
	t.Helper()
	identity := policy.Identity{UserID: userID, Role: policy.RoleViewer, Scope: policy.Scope{UserID: userID}}
	token := workflow.LeaseToken{RunID: runID, Owner: "worker-" + runID, Generation: generation}
	ctx := policy.WithIdentity(context.Background(), identity)
	ctx, err := workflow.ContextWithLeaseToken(ctx, token)
	if err != nil {
		t.Fatal(err)
	}
	attempt := &AttemptContext{
		Run: RunIdentity{ID: runID, SessionID: "session-" + runID, Attempt: 1, LeaseGeneration: generation,
			RuntimeVersion: "runtime-p14", RuntimeCompatibilityHash: frozen.CompatibilityHash()},
		Identity: identity, Scope: identity.Scope, Budget: budget, Lease: token,
		Trace: TraceIdentity{ID: "trace-" + runID}, Deadline: time.Now().Add(time.Hour), Snapshot: frozen,
	}
	ctx = context.WithValue(ctx, attemptContextKey{}, attempt)
	models := frozen.Models()
	modelSnapshot := models[0]
	return ctx, ModelInvocation{
		ReservationIdentity: "model-call-" + runID, CatalogRef: modelSnapshot.CatalogRef,
		Provider: modelSnapshot.Provider, Driver: modelSnapshot.Driver, ModelID: modelSnapshot.ModelID,
		Profile: modelSnapshot.Profile, SnapshotIdentity: modelSnapshot.Identity(),
	}
}

func p14SnapshotInput(t *testing.T) RuntimeSnapshotInput {
	t.Helper()
	input := p11SnapshotInput()
	for index := range input.Tools {
		entry, err := policy.LookupCatalog(input.Tools[index].Name)
		if err != nil {
			t.Fatal(err)
		}
		input.Tools[index].Revision = entry.Revision
		input.Tools[index].SchemaHash = entry.SchemaHash
	}
	return input
}

func p14TamperScope(t *testing.T, ctx context.Context) context.Context {
	t.Helper()
	attempt, err := AttemptContextFromContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	clone := *attempt
	clone.Scope = policy.Scope{UserID: "another-user"}
	return context.WithValue(ctx, attemptContextKey{}, &clone)
}

func p14ExpireDeadline(t *testing.T, ctx context.Context) context.Context {
	t.Helper()
	attempt, err := AttemptContextFromContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	clone := *attempt
	clone.Deadline = time.Now().Add(-time.Second)
	return context.WithValue(ctx, attemptContextKey{}, &clone)
}

func p14RequireCallMetadata(t *testing.T, ctx context.Context, kind BudgetCallKind, subject string) {
	t.Helper()
	metadata, err := CallMetadataFromContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if metadata.Kind != kind || metadata.Subject != subject || metadata.RunID == "" || metadata.UserID == "" ||
		metadata.TraceID == "" || metadata.ReservationIdentity == "" {
		t.Fatalf("call metadata = %+v", metadata)
	}
}

func p14RequireAllEndpointsRejectMissingContext(t *testing.T, handler *RuntimeHandler) {
	t.Helper()
	ctx := context.Background()
	toolContext := &adk.ToolContext{Name: "query_events", CallID: "missing-runtime-context"}
	modelEndpoint := &p14Model{}
	wrappedModel, err := handler.WrapModel(ctx, modelEndpoint, &adk.ModelContext{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = wrappedModel.Generate(ctx, []*schema.Message{schema.UserMessage("unsafe")}); !errors.Is(err, ErrAttemptContextMissing) {
		t.Fatalf("Model missing-context error = %v", err)
	}

	toolEndpointCalls := 0
	invokable, err := handler.WrapInvokableToolCall(ctx, func(context.Context, string, ...tool.Option) (string, error) {
		toolEndpointCalls++
		return "unsafe", nil
	}, toolContext)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = invokable(ctx, `{}`); !errors.Is(err, ErrAttemptContextMissing) {
		t.Fatalf("invokable missing-context error = %v", err)
	}

	streamable, err := handler.WrapStreamableToolCall(ctx, func(context.Context, string, ...tool.Option) (*schema.StreamReader[string], error) {
		toolEndpointCalls++
		return schema.StreamReaderFromArray([]string{"unsafe"}), nil
	}, toolContext)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = streamable(ctx, `{}`); !errors.Is(err, ErrAttemptContextMissing) {
		t.Fatalf("streamable missing-context error = %v", err)
	}

	enhanced, err := handler.WrapEnhancedInvokableToolCall(ctx, func(context.Context, *schema.ToolArgument, ...tool.Option) (*schema.ToolResult, error) {
		toolEndpointCalls++
		return &schema.ToolResult{}, nil
	}, toolContext)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = enhanced(ctx, &schema.ToolArgument{Text: `{}`}); !errors.Is(err, ErrAttemptContextMissing) {
		t.Fatalf("enhanced invokable missing-context error = %v", err)
	}

	enhancedStreamable, err := handler.WrapEnhancedStreamableToolCall(ctx, func(context.Context, *schema.ToolArgument, ...tool.Option) (*schema.StreamReader[*schema.ToolResult], error) {
		toolEndpointCalls++
		return schema.StreamReaderFromArray([]*schema.ToolResult{{}}), nil
	}, toolContext)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = enhancedStreamable(ctx, &schema.ToolArgument{Text: `{}`}); !errors.Is(err, ErrAttemptContextMissing) {
		t.Fatalf("enhanced streamable missing-context error = %v", err)
	}
	if modelEndpoint.calls != 0 || toolEndpointCalls != 0 {
		t.Fatalf("missing typed Context reached endpoints: Model=%d Tools=%d", modelEndpoint.calls, toolEndpointCalls)
	}
}

func p14RequireBeforeAgentDynamicToolRejected(t *testing.T, handler *RuntimeHandler) {
	t.Helper()
	dynamicTool := &p14DynamicUnknownTool{}
	dynamicHandler := &p14DynamicToolHandler{
		BaseChatModelAgentMiddleware: &adk.BaseChatModelAgentMiddleware{}, tool: dynamicTool,
	}
	handlers, err := RuntimeHandlerFirst(handler, dynamicHandler)
	if err != nil {
		t.Fatal(err)
	}
	budget := newP14RecordingBudget()
	ctx, invocation := p14InvocationContext(t, "run-before-agent-dynamic", "user-before-agent-dynamic", budget)
	ctx, err = WithModelInvocation(ctx, invocation)
	if err != nil {
		t.Fatal(err)
	}
	dynamicModel := &p14DynamicToolCallingModel{}
	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name: "p14-dynamic-tool", Description: "P14 BeforeAgent dynamic Tool contract", Model: dynamicModel,
		GenModelInput: LiteralGenModelInput, Handlers: handlers,
	})
	if err != nil {
		t.Fatal(err)
	}
	iterator := adk.NewRunner(ctx, adk.RunnerConfig{Agent: agent}).Query(ctx, "invoke dynamic Tool")
	rejected := false
	for {
		event, ok := iterator.Next()
		if !ok {
			break
		}
		if event.Err == nil {
			continue
		}
		if !errors.Is(event.Err, policy.ErrUnknownCatalogTool) {
			t.Fatalf("dynamic BeforeAgent Tool error = %v", event.Err)
		}
		rejected = true
	}
	if !rejected || dynamicTool.calls != 0 || dynamicModel.calls != 1 {
		t.Fatalf("dynamic BeforeAgent rejection/calls = %v/%d/%d, want true/0/1", rejected, dynamicTool.calls, dynamicModel.calls)
	}
	if reserved, settled := budget.counts(); reserved != 1 || settled != 1 {
		t.Fatalf("dynamic BeforeAgent Model budget = %d/%d, want 1/1", reserved, settled)
	}
}

func p14DrainStream[T any](t *testing.T, stream *schema.StreamReader[T]) {
	t.Helper()
	if stream == nil {
		t.Fatal("stream is nil")
	}
	defer stream.Close()
	for {
		_, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return
		}
		if err != nil {
			t.Fatal(err)
		}
	}
}
