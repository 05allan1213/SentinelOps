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

	"SentinelOps/internal/ai/cache"
	"SentinelOps/internal/ai/policy"
	"SentinelOps/internal/ai/workflow"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"gopkg.in/yaml.v3"
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
	agent := &p19RecordingAgent{name: "event_analysis_agent", called: called}
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

func TestCompositeInterruptThroughAgentTool(t *testing.T) {
	wrapped := adk.NewAgentTool(context.Background(), &namedWorkerAgent{
		name: "interrupt_worker", description: "worker",
		getter: func(context.Context) (adk.Agent, error) { return &p19InterruptAgent{}, nil },
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

func TestAgentToolEventsUseCanonicalCatalog(t *testing.T) {
	recorder := &p19Recorder{}
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

func TestMigrationParityWorkerManifestAndPlanTopology(t *testing.T) {
	type manifestAgent struct {
		Name        string `yaml:"name"`
		Description string `yaml:"description"`
	}
	var manifest struct {
		Agents []manifestAgent `yaml:"agents"`
	}
	raw, err := os.ReadFile("../../../../manifest/agent/migration-contract-v1.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err = yaml.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	want := make(map[string]string, len(manifest.Agents))
	for _, contract := range manifest.Agents {
		want[contract.Name] = contract.Description
	}
	for _, spec := range workerSpecs() {
		if got, ok := want[spec.name]; !ok || got != spec.description {
			t.Errorf("worker %q description drift: got %q want %q", spec.name, spec.description, got)
		}
	}

	plannerInput, err := customPlannerGenInput(context.Background(), []adk.Message{schema.UserMessage("analyze and persist intelligence")})
	if err != nil {
		t.Fatal(err)
	}
	joined := plannerInput[0].Content + plannerInput[len(plannerInput)-1].Content
	for _, text := range []string{"event_analysis_agent", "intelligence_agent", "analyze and persist intelligence"} {
		if !strings.Contains(joined, text) {
			t.Errorf("Planner fixed input missing %q", text)
		}
	}
	for _, file := range []string{"planner.go", "replan.go"} {
		source, readErr := os.ReadFile(file)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if !strings.Contains(string(source), "planexecute.New") {
			t.Errorf("%s does not use official planexecute builder", file)
		}
	}
}

func TestMigrationParityWorkerHistoryInjectedOnce(t *testing.T) {
	const sessionID = "p19-worker-history"
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
		Name: "cancel_worker", Description: "worker", Model: &p19BlockingModel{started: started, release: release},
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
		Name: "cancel_root", Description: "root", Model: &p19ToolCallModel{},
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
			return &p19RecordingAgent{name: name}, nil
		}}))
	}
	return tools
}

type p19RecordingAgent struct {
	name   string
	called chan<- string
}

type p19Recorder struct {
	mu     sync.Mutex
	events []string
}

func (*p19Recorder) Checkpoint(context.Context, string, map[string]any) error { return nil }
func (r *p19Recorder) Event(_ context.Context, eventType, payload string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !strings.Contains(payload, "agent_name") {
		return fmt.Errorf("missing agent_name")
	}
	r.events = append(r.events, eventType)
	return nil
}

type p19InterruptAgent struct{}

type p19InterruptState struct{ Step string }

func init() { schema.Register[p19InterruptState]() }

func (*p19InterruptAgent) Name(context.Context) string        { return "interrupt_worker" }
func (*p19InterruptAgent) Description(context.Context) string { return "interrupt worker" }
func (*p19InterruptAgent) Run(ctx context.Context, _ *adk.AgentInput, _ ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	iter, gen := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
	gen.Send(adk.StatefulInterrupt(ctx, "p19 interrupt", p19InterruptState{Step: "worker"}))
	gen.Close()
	return iter
}

func (a *p19RecordingAgent) Name(context.Context) string        { return a.name }
func (a *p19RecordingAgent) Description(context.Context) string { return "recording worker" }
func (a *p19RecordingAgent) Run(ctx context.Context, input *adk.AgentInput, _ ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	iter, gen := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
	if len(input.Messages) > 0 && a.called != nil {
		a.called <- input.Messages[len(input.Messages)-1].Content
	}
	gen.Send(&adk.AgentEvent{Output: &adk.AgentOutput{MessageOutput: &adk.MessageVariant{Message: schema.AssistantMessage("done", nil)}}})
	gen.Close()
	return iter
}

type p19BlockingModel struct {
	started chan<- struct{}
	release <-chan struct{}
}

func (m *p19BlockingModel) Generate(ctx context.Context, _ []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	m.started <- struct{}{}
	select {
	case <-m.release:
		return schema.AssistantMessage("released", nil), nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
func (*p19BlockingModel) Stream(context.Context, []*schema.Message, ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return nil, nil
}
func (*p19BlockingModel) BindTools([]*schema.ToolInfo) error { return nil }

type p19ToolCallModel struct{ calls atomic.Int32 }

func (m *p19ToolCallModel) Generate(context.Context, []*schema.Message, ...model.Option) (*schema.Message, error) {
	if m.calls.Add(1) == 1 {
		return schema.AssistantMessage("", []schema.ToolCall{{ID: "call", Function: schema.FunctionCall{Name: "cancel_worker", Arguments: `{"request":"wait"}`}}}), nil
	}
	return schema.AssistantMessage("done", nil), nil
}
func (*p19ToolCallModel) Stream(context.Context, []*schema.Message, ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return nil, nil
}
func (*p19ToolCallModel) BindTools([]*schema.ToolInfo) error { return nil }
