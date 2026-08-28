package plan_pipeline

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"SentinelOps/internal/ai/agent/mcp_pipeline"
	"SentinelOps/internal/ai/agent/skill_pipeline"
	"SentinelOps/internal/ai/cache"
	"SentinelOps/internal/ai/policy"
	airuntime "SentinelOps/internal/ai/runtime"
	"SentinelOps/internal/ai/workflow"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

func TestAgentToolWorkersWrapRealAgents(t *testing.T) {
	tools := newScriptedWorkerAgentTools(context.Background())
	want := []string{"event_analysis_agent", "report_agent", "risk_assessment_agent", "solve_agent", "intelligence_agent", "ops_agent"}
	got := make([]string, 0, len(tools))
	for _, worker := range tools {
		info, err := worker.Info(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, info.Name)
		if _, ok := worker.(tool.InvokableTool); !ok {
			t.Fatalf("worker %q is not an invokable AgentTool", info.Name)
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("worker tools = %#v, want %#v", got, want)
	}
}

func TestAgentToolFrameworkEntriesAreL0AndEffectFree(t *testing.T) {
	for _, agentTool := range newScriptedWorkerAgentTools(context.Background()) {
		info, err := agentTool.Info(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		name := info.Name
		entry, err := policy.LookupCatalog(name)
		if err != nil {
			t.Fatal(err)
		}
		if entry.Risk != policy.RiskL0 || entry.EffectType != policy.EffectNone || len(entry.EffectSteps) != 0 {
			t.Fatalf("framework AgentTool %q is not effect-free L0: %+v", name, entry)
		}
		if err := policy.RequireExecutable(name); err != nil {
			t.Fatalf("framework AgentTool %q is not executable through L0 policy: %v", name, err)
		}
		hash, err := policy.ToolSchemaHash(info)
		if err != nil || hash != entry.SchemaHash {
			t.Fatalf("framework AgentTool %q schema hash = %q, want %q, err=%v", name, hash, entry.SchemaHash, err)
		}
	}
}

func TestAgentToolMutationLeavesStayDisabled(t *testing.T) {
	for _, name := range []string{
		"create_report", "save_intelligence", "update_event_status", "block_ip",
		"notify_dingtalk", "notify_wecom", "notify_email", "webhook_out",
	} {
		if err := policy.RequireExecutable(name); err == nil || !strings.Contains(err.Error(), policy.PolicyMutationDisabled) {
			t.Fatalf("mutation leaf %q is reachable through AgentTool: %v", name, err)
		}
	}
}

func TestAgentToolWorkersPropagateNestedRun(t *testing.T) {
	called := make(chan string, 1)
	agent := &fixture19RecordingAgent{name: "event_analysis_agent", called: called}
	wrapped := adk.NewAgentTool(context.Background(), &namedWorkerAgent{name: "event_analysis_agent", description: "worker", getter: func(context.Context) (adk.Agent, error) { return agent, nil }})
	invokable, ok := wrapped.(tool.InvokableTool)
	if !ok {
		t.Fatal("AgentTool is not invokable")
	}
	if _, err := invokable.InvokableRun(context.Background(), `{"request":"analyze"}`); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-called:
		if got != "analyze" {
			t.Fatalf("nested request = %q", got)
		}
	default:
		t.Fatal("real wrapped Agent was not invoked")
	}
}

func TestNamedWorkerAgentCachesBuilderAcrossLifecycle(t *testing.T) {
	var builds atomic.Int32
	worker := &namedWorkerAgent{
		name: "cached_worker", description: "worker", handler: &airuntime.RuntimeHandler{},
		builder: func(context.Context, *airuntime.RuntimeHandler) (adk.Agent, error) {
			builds.Add(1)
			return &fixture19RecordingAgent{name: "cached_worker"}, nil
		},
	}
	first, err := worker.resolve(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	second, err := worker.resolve(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if first != second || builds.Load() != 1 {
		t.Fatalf("builder lifecycle = first=%p second=%p builds=%d, want one shared Agent", first, second, builds.Load())
	}
}

func TestCompositeInterruptThroughAgentTool(t *testing.T) {
	wrapped := adk.NewAgentTool(context.Background(), &namedWorkerAgent{
		name: "interrupt_worker", description: "worker",
		getter: func(context.Context) (adk.Agent, error) { return &fixture19InterruptAgent{}, nil },
	})
	invokable := wrapped.(tool.InvokableTool)
	if _, err := invokable.InvokableRun(context.Background(), `{"request":"pause"}`); err == nil {
		t.Fatal("AgentTool interrupt error is nil")
	} else if signal := new(adk.InterruptSignal); !errors.As(err, &signal) {
		t.Fatalf("AgentTool interrupt error = %T %v", err, err)
	}
}

func TestPlanTopologyUsesOfficialAgentToolAndNoBridge(t *testing.T) {
	topology, err := os.ReadFile("plan_pipeline.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(topology), "planexecute.New(ctx, &planexecute.Config{") {
		t.Fatal("Plan topology is not built by planexecute.New")
	}
	workerSource, err := os.ReadFile("agent_worker.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(workerSource)
	for _, forbidden := range []string{"compose.Runnable", "adk.NewRunner", "ResumeWithParams", "StatefulInterrupt"} {
		if strings.Contains(source, forbidden) {
			t.Errorf("worker source contains forbidden Runnable/Event/Resume bridge %q", forbidden)
		}
	}
	if !strings.Contains(source, "adk.NewAgentTool") {
		t.Fatal("worker source does not use official adk.NewAgentTool")
	}
}

func TestPlannerDelegatesAnalysisAndPersistenceExplicitly(t *testing.T) {
	source, err := os.ReadFile("../../prompt/agents/planner.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	for _, phrase := range []string{"EventAnalysisAgent", "IntelligenceAgent", "save_intelligence", "只分析"} {
		if !strings.Contains(text, phrase) {
			t.Errorf("Planner prompt missing explicit delegation phrase %q", phrase)
		}
	}
}

func TestPlannerPromptCoversExecutorCapabilityInventory(t *testing.T) {
	ctx := context.Background()
	handler := airuntime.NewRuntimeHandler()
	agentTools := newWorkerAgentTools(ctx, handler)
	agentTools = append(agentTools,
		mcp_pipeline.NewAgentTool(ctx, handler),
		skill_pipeline.NewAgentTool(ctx, handler),
	)
	cfg, err := newExecutorAgentConfig(ctx, &ExecutorBuilderConfig{
		Model:               &fixture15Model{},
		RegisteredToolNames: []string{"query_internal_docs", "get_current_time"},
		AgentTools:          agentTools,
		RuntimeHandler:      handler,
	})
	if err != nil {
		t.Fatal(err)
	}

	plannerInput, err := customPlannerGenInput(ctx, []adk.Message{schema.UserMessage("query internal documentation")})
	if err != nil {
		t.Fatal(err)
	}
	prompt := plannerInput[0].Content
	want := []string{
		"query_internal_docs", "get_current_time", "event_analysis_agent", "report_agent",
		"risk_assessment_agent", "solve_agent", "intelligence_agent", "ops_agent",
		"mcp_agent", "skill_agent",
	}
	got := make([]string, 0, len(cfg.ToolsConfig.Tools))
	for _, current := range cfg.ToolsConfig.Tools {
		info, infoErr := current.Info(ctx)
		if infoErr != nil {
			t.Fatal(infoErr)
		}
		got = append(got, info.Name)
		if !strings.Contains(prompt, info.Name) {
			t.Errorf("Planner prompt missing executable capability %q", info.Name)
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Executor capability inventory = %#v, want %#v", got, want)
	}
}

func TestAgentToolEventsUseCanonicalCatalog(t *testing.T) {
	recorder := &fixture19Recorder{}
	ctx := WithWorkflowRecorder(context.Background(), recorder)
	for _, eventType := range []string{
		workflow.EventAgentPlan, workflow.EventAgentReplan, workflow.EventAgentToolCall,
		workflow.EventAgentToolResult, workflow.EventAgentInterrupted,
	} {
		eventWorkflow(ctx, eventType, map[string]any{"agent_name": "scripted"})
	}
	if !reflect.DeepEqual(recorder.events, []string{
		workflow.EventAgentPlan, workflow.EventAgentReplan, workflow.EventAgentToolCall,
		workflow.EventAgentToolResult, workflow.EventAgentInterrupted,
	}) {
		t.Fatalf("Agent events = %#v", recorder.events)
	}
}

func TestMigrationParityWorkerHistoryInjectedOnce(t *testing.T) {
	const sessionID = "phase19-worker-history"
	mem := cache.GetSessionMemory(sessionID)
	mem.SetState([]*schema.Message{schema.AssistantMessage("prior answer", nil)}, "prior summary")
	defer mem.SetState(nil, "")
	ctx := context.WithValue(context.Background(), SessionIdCtxKey{}, sessionID)
	got := workerAgentInput(ctx, &adk.AgentInput{Messages: []adk.Message{schema.UserMessage("current request")}, EnableStreaming: true})
	if got == nil || len(got.Messages) != 3 {
		t.Fatalf("worker messages = %#v, want summary + history + current", got)
	}
	counts := map[string]int{}
	for _, message := range got.Messages {
		counts[message.Content]++
	}
	if counts["prior answer"] != 1 || counts["current request"] != 1 {
		t.Fatalf("worker history/query injection counts = %#v", counts)
	}
	if !got.EnableStreaming {
		t.Fatal("worker input lost official streaming state")
	}
}

func TestRecursiveCancelThroughNamedAgentTool(t *testing.T) {
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	child, err := adk.NewChatModelAgent(context.Background(), &adk.ChatModelAgentConfig{
		Name: "cancel_worker", Description: "worker", Model: &fixture19BlockingModel{started: started, release: release},
		GenModelInput: func(_ context.Context, _ string, input *adk.AgentInput) ([]adk.Message, error) {
			return input.Messages, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	worker := &namedWorkerAgent{name: "cancel_worker", description: "worker", getter: func(context.Context) (adk.Agent, error) {
		return child, nil
	}}
	root, err := adk.NewChatModelAgent(context.Background(), &adk.ChatModelAgentConfig{
		Name: "cancel_root", Description: "root", Model: &fixture19ToolCallModel{},
		GenModelInput: func(_ context.Context, _ string, input *adk.AgentInput) ([]adk.Message, error) {
			return input.Messages, nil
		},
		ToolsConfig: adk.ToolsConfig{ToolsNodeConfig: compose.ToolsNodeConfig{Tools: []tool.BaseTool{adk.NewAgentTool(context.Background(), worker)}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	cancelOption, cancel := adk.WithCancel()
	iterator := adk.NewRunner(context.Background(), adk.RunnerConfig{Agent: root}).Query(context.Background(), "cancel", cancelOption)
	drained := make(chan struct{})
	go func() {
		defer close(drained)
		for {
			if _, ok := iterator.Next(); !ok {
				return
			}
		}
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("nested named AgentTool did not start")
	}
	handle, contributed := cancel(adk.WithAgentCancelMode(adk.CancelImmediate), adk.WithRecursive())
	if !contributed {
		t.Fatal("recursive cancel did not contribute")
	}
	close(release)
	if err = handle.Wait(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-drained:
	case <-time.After(time.Second):
		t.Fatal("recursive cancel did not drain root")
	}
}

func newScriptedWorkerAgentTools(ctx context.Context) []tool.BaseTool {
	names := []string{"event_analysis_agent", "report_agent", "risk_assessment_agent", "solve_agent", "intelligence_agent", "ops_agent"}
	tools := make([]tool.BaseTool, 0, len(names))
	for _, name := range names {
		name := name
		tools = append(tools, adk.NewAgentTool(ctx, &namedWorkerAgent{name: name, description: "worker", getter: func(context.Context) (adk.Agent, error) {
			return &fixture19RecordingAgent{name: name}, nil
		}}))
	}
	return tools
}

type fixture19RecordingAgent struct {
	name   string
	called chan<- string
}

type fixture19Recorder struct {
	mu     sync.Mutex
	events []string
}

func (*fixture19Recorder) Checkpoint(context.Context, string, map[string]any) error { return nil }
func (r *fixture19Recorder) Event(_ context.Context, eventType, payload string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !strings.Contains(payload, "agent_name") {
		return fmt.Errorf("missing agent_name")
	}
	r.events = append(r.events, eventType)
	return nil
}

type fixture19InterruptAgent struct{}

type fixture19InterruptState struct{ Step string }

func init() { schema.Register[fixture19InterruptState]() }

func (*fixture19InterruptAgent) Name(context.Context) string        { return "interrupt_worker" }
func (*fixture19InterruptAgent) Description(context.Context) string { return "interrupt worker" }
func (*fixture19InterruptAgent) Run(ctx context.Context, _ *adk.AgentInput, _ ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	iter, gen := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
	gen.Send(adk.StatefulInterrupt(ctx, "phase19 interrupt", fixture19InterruptState{Step: "worker"}))
	gen.Close()
	return iter
}

func (a *fixture19RecordingAgent) Name(context.Context) string        { return a.name }
func (a *fixture19RecordingAgent) Description(context.Context) string { return "recording worker" }
func (a *fixture19RecordingAgent) Run(ctx context.Context, input *adk.AgentInput, _ ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	iter, gen := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
	if len(input.Messages) > 0 && a.called != nil {
		a.called <- input.Messages[len(input.Messages)-1].Content
	}
	gen.Send(&adk.AgentEvent{Output: &adk.AgentOutput{MessageOutput: &adk.MessageVariant{Message: schema.AssistantMessage("done", nil)}}})
	gen.Close()
	return iter
}

type fixture19BlockingModel struct {
	started chan<- struct{}
	release <-chan struct{}
}

func (m *fixture19BlockingModel) Generate(ctx context.Context, _ []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	m.started <- struct{}{}
	select {
	case <-m.release:
		return schema.AssistantMessage("released", nil), nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
func (*fixture19BlockingModel) Stream(context.Context, []*schema.Message, ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return nil, nil
}
func (*fixture19BlockingModel) BindTools([]*schema.ToolInfo) error { return nil }

type fixture19ToolCallModel struct{ calls atomic.Int32 }

func (m *fixture19ToolCallModel) Generate(context.Context, []*schema.Message, ...model.Option) (*schema.Message, error) {
	if m.calls.Add(1) == 1 {
		return schema.AssistantMessage("", []schema.ToolCall{{ID: "call", Function: schema.FunctionCall{Name: "cancel_worker", Arguments: `{"request":"wait"}`}}}), nil
	}
	return schema.AssistantMessage("done", nil), nil
}
func (*fixture19ToolCallModel) Stream(context.Context, []*schema.Message, ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return nil, nil
}
func (*fixture19ToolCallModel) BindTools([]*schema.ToolInfo) error { return nil }
