package agent_test

import (
	"context"
	"errors"
	"io"
	"testing"

	agenttest "SentinelOps/internal/testutil/agent"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

func TestScriptedHarnessImplementsEinoContracts(t *testing.T) {
	modelDouble := agenttest.NewScriptedChatModel(
		agenttest.ModelStep{Message: schema.AssistantMessage("", []schema.ToolCall{{ID: "single", Function: schema.FunctionCall{Name: "lookup", Arguments: `{}`}}})},
		agenttest.ModelStep{Message: schema.AssistantMessage("", []schema.ToolCall{
			{ID: "multi-1", Function: schema.FunctionCall{Name: "lookup", Arguments: `{}`}},
			{ID: "multi-2", Function: schema.FunctionCall{Name: "clock", Arguments: `{}`}},
		})},
		agenttest.ModelStep{Chunks: []*schema.Message{schema.AssistantMessage("stream", nil)}},
	)
	var _ model.ToolCallingChatModel = modelDouble
	bound, err := modelDouble.WithTools([]*schema.ToolInfo{{Name: "lookup"}, {Name: "clock"}})
	if err != nil {
		t.Fatal(err)
	}
	first, err := bound.Generate(context.Background(), nil)
	if err != nil || len(first.ToolCalls) != 1 {
		t.Fatalf("single tool call = %#v, err=%v", first, err)
	}
	second, err := bound.Generate(context.Background(), nil)
	if err != nil || len(second.ToolCalls) != 2 {
		t.Fatalf("multiple tool calls = %#v, err=%v", second, err)
	}
	stream, err := bound.Stream(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if chunk, recvErr := stream.Recv(); recvErr != nil || chunk.Content != "stream" {
		t.Fatalf("stream chunk = %#v, err=%v", chunk, recvErr)
	}
	if _, recvErr := stream.Recv(); !errors.Is(recvErr, io.EOF) {
		t.Fatalf("stream terminal error = %v", recvErr)
	}
	if _, err = bound.Generate(context.Background(), nil); !errors.Is(err, agenttest.ErrScriptExhausted) {
		t.Fatalf("max iteration expression = %v", err)
	}

	toolErr := errors.New("tool failed")
	toolDouble := agenttest.NewScriptedTool("lookup", "lookup test tool",
		agenttest.ToolStep{Result: "ok"},
		agenttest.ToolStep{Err: toolErr},
		agenttest.ToolStep{Chunks: []string{"a", "b"}},
		agenttest.InterruptToolStep("approval", map[string]int{"step": 1}),
	)
	var _ tool.InvokableTool = toolDouble
	var _ tool.StreamableTool = toolDouble
	if result, runErr := toolDouble.InvokableRun(context.Background(), `{}`); runErr != nil || result != "ok" {
		t.Fatalf("tool result = %q, err=%v", result, runErr)
	}
	if _, runErr := toolDouble.InvokableRun(context.Background(), `{}`); !errors.Is(runErr, toolErr) {
		t.Fatalf("tool error = %v", runErr)
	}
	toolStream, err := toolDouble.StreamableRun(context.Background(), `{}`)
	if err != nil {
		t.Fatal(err)
	}
	defer toolStream.Close()
	for _, want := range []string{"a", "b"} {
		if chunk, recvErr := toolStream.Recv(); recvErr != nil || chunk != want {
			t.Fatalf("tool stream chunk = %q, err=%v, want %q", chunk, recvErr, want)
		}
	}
	if _, err = toolDouble.InvokableRun(context.Background(), `{}`); err == nil {
		t.Fatal("interrupt step returned nil error")
	} else if signal := new(adk.InterruptSignal); !errors.As(err, &signal) {
		t.Fatalf("interrupt step error = %T %v", err, err)
	}
}
