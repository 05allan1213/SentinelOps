package plan_pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/prebuilt/planexecute"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

type scriptedToolModel struct {
	responses []*schema.Message
	calls     int
	bound     bool
}

func (m *scriptedToolModel) Generate(_ context.Context, _ []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	index := m.calls
	m.calls++
	if index >= len(m.responses) {
		index = len(m.responses) - 1
	}
	if index < 0 {
		return nil, errors.New("no scripted response")
	}
	return m.responses[index], nil
}

func (m *scriptedToolModel) Stream(ctx context.Context, input []*schema.Message, options ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	message, err := m.Generate(ctx, input, options...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{message}), nil
}

func (m *scriptedToolModel) WithTools(_ []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	clone := *m
	clone.bound = true
	return &clone, nil
}

func toolCallMessage(name, arguments string) *schema.Message {
	return schema.AssistantMessage("", []schema.ToolCall{{
		ID: "call-1", Type: "function",
		Function: schema.FunctionCall{Name: name, Arguments: arguments},
	}})
}

func TestRequiredToolCallModelRetriesPlainTextResponse(t *testing.T) {
	inner := &scriptedToolModel{responses: []*schema.Message{
		schema.AssistantMessage("plain text", nil),
		toolCallMessage("plan", `{"steps":["step"]}`),
	}}
	wrapped := withPlannerRequiredToolCalls(inner)
	message, err := wrapped.Generate(context.Background(), nil)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !hasToolCall(message) || message.ToolCalls[0].Function.Name != "plan" {
		t.Fatalf("expected retried plan tool call, got %+v", message)
	}
	if inner.calls != 2 {
		t.Fatalf("Generate called %d times, want 2", inner.calls)
	}
}

func TestRequiredToolCallModelRetriesPlainTextStream(t *testing.T) {
	inner := &scriptedToolModel{responses: []*schema.Message{
		schema.AssistantMessage("plain stream", nil),
		toolCallMessage("plan", `{"steps":["step"]}`),
	}}
	wrapped := withPlannerRequiredToolCalls(inner)
	stream, err := wrapped.Stream(context.Background(), nil)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	chunks, err := drainMessageStream(stream)
	if err != nil {
		t.Fatalf("drain stream: %v", err)
	}
	if message := concatenatedMessage(chunks); !hasToolCall(message) {
		t.Fatalf("expected retried tool call in stream, got %+v", message)
	}
	if inner.calls != 2 {
		t.Fatalf("Stream called provider %d times, want 2", inner.calls)
	}
}

func TestReplannerRequiredToolCallModelFallsBackToRespondTool(t *testing.T) {
	inner := &scriptedToolModel{responses: []*schema.Message{
		schema.AssistantMessage("最终答复：一切正常", nil),
	}}
	wrapped := withReplannerRequiredToolCalls(inner, "respond")
	message, err := wrapped.Generate(context.Background(), nil)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !hasToolCall(message) || message.ToolCalls[0].Function.Name != "respond" {
		t.Fatalf("expected synthesized respond tool call, got %+v", message)
	}
	var arguments map[string]string
	if err := json.Unmarshal([]byte(message.ToolCalls[0].Function.Arguments), &arguments); err != nil {
		t.Fatalf("unmarshal synthesized arguments: %v", err)
	}
	if arguments["response"] != "最终答复：一切正常" {
		t.Fatalf("synthesized response = %q", arguments["response"])
	}
}

func TestPlannerRequiredToolCallModelKeepsLastPlainResponse(t *testing.T) {
	inner := &scriptedToolModel{responses: []*schema.Message{
		schema.AssistantMessage("planner plain text", nil),
	}}
	wrapped := withPlannerRequiredToolCalls(inner)
	message, err := wrapped.Generate(context.Background(), nil)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if hasToolCall(message) || message.Content != "planner plain text" {
		t.Fatalf("planner wrapper must return the last provider response unchanged, got %+v", message)
	}
}

func TestRequiredToolCallModelWithToolsKeepsWrapper(t *testing.T) {
	inner := &scriptedToolModel{responses: []*schema.Message{
		toolCallMessage("plan", `{"steps":["step"]}`),
	}}
	wrapped := withPlannerRequiredToolCalls(inner)
	bound, err := wrapped.WithTools([]*schema.ToolInfo{{Name: "plan"}})
	if err != nil {
		t.Fatalf("WithTools: %v", err)
	}
	if _, ok := bound.(*requiredToolCallModel); !ok {
		t.Fatalf("WithTools must keep the required-tool wrapper, got %T", bound)
	}
	message, err := bound.Generate(context.Background(), nil)
	if err != nil {
		t.Fatalf("bound Generate: %v", err)
	}
	if !hasToolCall(message) {
		t.Fatalf("bound wrapper lost tool call enforcement: %+v", message)
	}
}

func TestRequiredToolCallWrapperRecoversThroughOfficialPlanner(t *testing.T) {
	planner, err := planexecute.NewPlanner(context.Background(), &planexecute.PlannerConfig{
		ToolCallingChatModel: withPlannerRequiredToolCalls(&scriptedToolModel{responses: []*schema.Message{
			schema.AssistantMessage("先返回一段纯文本，而不是 Plan Tool", nil),
			toolCallMessage(planexecute.PlanToolInfo.Name, `{"steps":["查询最近事件"]}`),
		}}),
		GenInputFn: func(_ context.Context, input []adk.Message) ([]adk.Message, error) { return input, nil },
	})
	if err != nil {
		t.Fatalf("NewPlanner: %v", err)
	}
	iterator := planner.Run(context.Background(), &adk.AgentInput{Messages: []adk.Message{schema.UserMessage("生成周报")}})
	var contents []string
	for {
		event, ok := iterator.Next()
		if !ok {
			break
		}
		if event.Err != nil {
			t.Fatalf("official planner failed after plain-text retry: %v", event.Err)
		}
		if event.Output != nil && event.Output.MessageOutput != nil && event.Output.MessageOutput.Message != nil {
			contents = append(contents, event.Output.MessageOutput.Message.Content)
		}
	}
	if joined := strings.Join(contents, "\n"); !strings.Contains(joined, "查询最近事件") {
		t.Fatalf("planner did not emit the recovered plan: %q", joined)
	}
}
