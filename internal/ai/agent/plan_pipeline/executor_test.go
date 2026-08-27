package plan_pipeline

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	airuntime "SentinelOps/internal/ai/runtime"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/prebuilt/planexecute"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

func TestExecutorBuilder(t *testing.T) {
	ctx := context.Background()
	chatModel := &p15Model{}
	agentTool := adk.NewAgentTool(ctx, &p15Agent{name: "specialist"})
	handler := airuntime.NewRuntimeHandler()

	cfg, err := newExecutorAgentConfig(ctx, &ExecutorBuilderConfig{
		Model:               chatModel,
		RegisteredToolNames: []string{"get_current_time"},
		AgentTools:          []tool.BaseTool{agentTool},
		RuntimeHandler:      handler,
	})
	if err != nil {
		t.Fatalf("构建 Executor 配置失败: %v", err)
	}
	if cfg.OutputKey != planexecute.ExecutedStepSessionKey {
		t.Fatalf("OutputKey = %q, want official %q", cfg.OutputKey, planexecute.ExecutedStepSessionKey)
	}
	if cfg.MaxIterations != 20 {
		t.Fatalf("MaxIterations = %d, want 20", cfg.MaxIterations)
	}
	if !reflect.DeepEqual(cfg.ToolsConfig.ReturnDirectly, map[string]bool{"specialist": true}) {
		t.Fatalf("ReturnDirectly = %#v, want nested AgentTool only", cfg.ToolsConfig.ReturnDirectly)
	}
	if len(cfg.Handlers) != 1 || cfg.Handlers[0] != handler {
		t.Fatalf("Handlers 未保持唯一 RuntimeHandler 最外层: %#v", cfg.Handlers)
	}
	if cfg.Model != chatModel || cfg.ModelRetryConfig != nil || cfg.ModelFailoverConfig != nil {
		t.Fatalf("旧兼容路径可靠性配置 = Model:%T Retry:%#v Failover:%#v, want 仅直连 Model", cfg.Model, cfg.ModelRetryConfig, cfg.ModelFailoverConfig)
	}
	if len(cfg.ToolsConfig.Tools) != 2 {
		t.Fatalf("Executor Tool 数量 = %d, want Registry + AgentTool", len(cfg.ToolsConfig.Tools))
	}
	var toolNames []string
	for _, executorTool := range cfg.ToolsConfig.Tools {
		info, infoErr := executorTool.Info(ctx)
		if infoErr != nil {
			t.Fatalf("读取 Executor ToolInfo: %v", infoErr)
		}
		toolNames = append(toolNames, info.Name)
	}
	if !reflect.DeepEqual(toolNames, []string{"get_current_time", "specialist"}) {
		t.Fatalf("Executor Tool = %#v", toolNames)
	}

	agent, err := NewExecutorBuilder(ctx, &ExecutorBuilderConfig{
		Model:               chatModel,
		RegisteredToolNames: []string{"get_current_time"},
		AgentTools:          []tool.BaseTool{agentTool},
		RuntimeHandler:      airuntime.NewRuntimeHandler(),
	})
	if err != nil {
		t.Fatalf("构建 Executor 失败: %v", err)
	}
	if agent.Name(ctx) != "executor" || agent.Description(ctx) != "an executor agent" {
		t.Fatalf("Executor identity = %q/%q", agent.Name(ctx), agent.Description(ctx))
	}

	if _, err = newExecutorAgentConfig(ctx, &ExecutorBuilderConfig{Model: chatModel}); err == nil || !strings.Contains(err.Error(), "runtime Handler is required") {
		t.Fatalf("缺少 RuntimeHandler 时 error = %v", err)
	}
	if _, err = newExecutorAgentConfig(ctx, &ExecutorBuilderConfig{
		Model: chatModel, RegisteredToolNames: []string{"missing_tool"}, RuntimeHandler: handler,
	}); err == nil || !strings.Contains(err.Error(), "is missing") {
		t.Fatalf("缺少 Registry Tool 时 error = %v", err)
	}
}

func TestExecutorSessionKeys(t *testing.T) {
	plan := &p15Plan{Steps: []string{"collect evidence", "write answer"}}
	sessionValues := map[string]any{
		planexecute.UserInputSessionKey:     []adk.Message{schema.UserMessage("investigate alert")},
		planexecute.PlanSessionKey:          plan,
		planexecute.ExecutedStepsSessionKey: []planexecute.ExecutedStep{{Step: "triage", Result: "confirmed"}},
		planexecute.ExecutedStepSessionKey:  "must not be read as input",
		"sentinelops.forbidden":             "must not leak",
	}
	probe := &p15InputProbeAgent{genInput: executorGenModelInput}
	runner := adk.NewRunner(context.Background(), adk.RunnerConfig{Agent: probe})
	iter := runner.Run(context.Background(), nil, adk.WithSessionValues(sessionValues))
	for {
		event, ok := iter.Next()
		if !ok {
			break
		}
		if event.Err != nil {
			t.Fatalf("生成 Executor 输入失败: %v", event.Err)
		}
	}
	if len(probe.messages) != 2 {
		t.Fatalf("Executor prompt 消息数 = %d, want 2", len(probe.messages))
	}
	got := probe.messages[0].Content + "\n" + probe.messages[1].Content
	for _, want := range []string{"investigate alert", `{"steps":["collect evidence","write answer"]}`, "Step: triage", "Result: confirmed", "collect evidence"} {
		if !strings.Contains(got, want) {
			t.Errorf("Executor golden 缺少 %q:\n%s", want, got)
		}
	}
	for _, forbidden := range []string{"must not be read as input", "must not leak"} {
		if strings.Contains(got, forbidden) {
			t.Errorf("Executor prompt 泄露非 allowlist SessionValue %q", forbidden)
		}
	}

	officialModel := &p15Model{}
	officialExecutor, err := planexecute.NewExecutor(context.Background(), &planexecute.ExecutorConfig{
		Model: officialModel, MaxIterations: 20,
	})
	if err != nil {
		t.Fatalf("构建官方 Executor: %v", err)
	}
	officialRunner := adk.NewRunner(context.Background(), adk.RunnerConfig{Agent: officialExecutor})
	officialIter := officialRunner.Run(context.Background(), nil, adk.WithSessionValues(sessionValues))
	for {
		event, ok := officialIter.Next()
		if !ok {
			break
		}
		if event.Err != nil {
			t.Fatalf("运行官方 Executor golden: %v", event.Err)
		}
	}
	if !reflect.DeepEqual(probe.messages, officialModel.inputs) {
		t.Fatalf("薄 builder 输入与官方 Executor golden 不同\n薄 builder: %#v\n官方: %#v", probe.messages, officialModel.inputs)
	}
}

func TestPlanTopology(t *testing.T) {
	topology, err := os.ReadFile("plan_pipeline.go")
	if err != nil {
		t.Fatalf("读取 Plan topology 源码: %v", err)
	}
	if !strings.Contains(string(topology), "planexecute.New(ctx, &planexecute.Config{") {
		t.Fatal("Plan 总拓扑未由官方 planexecute.New 创建")
	}

	builder, err := os.ReadFile("executor_adk.go")
	if err != nil {
		t.Fatalf("读取 Executor builder 源码: %v", err)
	}
	for _, forbidden := range []string{"planexecute.NewExecutor", "adk.NewRunner", "LoopAgent", "for {"} {
		if strings.Contains(string(builder), forbidden) {
			t.Errorf("Executor builder 包含禁止的 Loop/Runner 实现 %q", forbidden)
		}
	}
	for _, forbidden := range []string{"NewQueryInternalDocsTool", "NewGetCurrentTimeTool", "adk.NewAgentTool", `"UserInput"`, `"Plan"`, `"ExecutedStep"`, `"ExecutedSteps"`} {
		if strings.Contains(string(builder), forbidden) {
			t.Errorf("Executor builder 直接构造 Tool 或复制 Session key %q", forbidden)
		}
	}
	if !strings.Contains(string(builder), "aitools.GetManyRequired") {
		t.Fatal("Executor builder 未通过 strict Registry 解析已注册 Tool")
	}

	for _, path := range []string{"../../../../manifest/config/config.yaml", "../../../../manifest/config/config.docker.yaml"} {
		config, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatalf("读取 durable Gate 配置 %s: %v", path, readErr)
		}
		if !strings.Contains(string(config), "agent_runtime:\n  enabled: false") {
			t.Errorf("%s 未保持 agent_runtime.enabled=false", path)
		}
	}
}

type p15Plan struct {
	Steps []string `json:"steps"`
}

func (p *p15Plan) FirstStep() string {
	if len(p.Steps) == 0 {
		return ""
	}
	return p.Steps[0]
}

func (p *p15Plan) MarshalJSON() ([]byte, error) {
	type plan p15Plan
	return json.Marshal((*plan)(p))
}

func (p *p15Plan) UnmarshalJSON(data []byte) error {
	type plan p15Plan
	return json.Unmarshal(data, (*plan)(p))
}

type p15InputProbeAgent struct {
	genInput adk.GenModelInput
	messages []adk.Message
}

func (*p15InputProbeAgent) Name(context.Context) string        { return "executor_input_probe" }
func (*p15InputProbeAgent) Description(context.Context) string { return "captures Executor input" }
func (a *p15InputProbeAgent) Run(ctx context.Context, input *adk.AgentInput, _ ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	iter, generator := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
	var err error
	a.messages, err = a.genInput(ctx, "", input)
	if err != nil {
		generator.Send(&adk.AgentEvent{Err: err})
	}
	generator.Close()
	return iter
}

type p15Agent struct{ name string }

func (a *p15Agent) Name(context.Context) string      { return a.name }
func (*p15Agent) Description(context.Context) string { return "scripted specialist" }
func (a *p15Agent) Run(context.Context, *adk.AgentInput, ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	iter, generator := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
	generator.Close()
	return iter
}

type p15Model struct {
	inputs []*schema.Message
}

func (m *p15Model) Generate(_ context.Context, input []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	m.inputs = append([]*schema.Message(nil), input...)
	return schema.AssistantMessage("done", nil), nil
}

func (*p15Model) Stream(context.Context, []*schema.Message, ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return nil, nil
}

func (*p15Model) BindTools([]*schema.ToolInfo) error { return nil }
