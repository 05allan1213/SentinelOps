package agenttest

import (
	"context"
	"errors"
	"io"
	"reflect"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

func TestScriptedHarnessModelSupportsToolCallsStreamAndIterations(t *testing.T) {
	modelDouble := NewScriptedChatModel(
		ModelStep{Message: schema.AssistantMessage("", []schema.ToolCall{{ID: "one", Function: schema.FunctionCall{Name: "lookup", Arguments: `{}`}}})},
		ModelStep{Message: schema.AssistantMessage("", []schema.ToolCall{
			{ID: "two", Function: schema.FunctionCall{Name: "lookup", Arguments: `{"id":1}`}},
			{ID: "three", Function: schema.FunctionCall{Name: "clock", Arguments: `{}`}},
		})},
		ModelStep{Chunks: []*schema.Message{schema.AssistantMessage("final ", nil), schema.AssistantMessage("answer", nil)}},
	)
	var _ model.ToolCallingChatModel = modelDouble

	bound, err := modelDouble.WithTools([]*schema.ToolInfo{{Name: "lookup"}, {Name: "clock"}})
	if err != nil {
		t.Fatal(err)
	}
	first, err := bound.Generate(context.Background(), []*schema.Message{schema.UserMessage("single")})
	if err != nil || len(first.ToolCalls) != 1 {
		t.Fatalf("single tool call = %#v, err=%v", first, err)
	}
	second, err := bound.Generate(context.Background(), []*schema.Message{schema.UserMessage("multi")})
	if err != nil || len(second.ToolCalls) != 2 {
		t.Fatalf("multiple tool calls = %#v, err=%v", second, err)
	}
	stream, err := bound.Stream(context.Background(), []*schema.Message{schema.UserMessage("stream")})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	var chunks []string
	for {
		chunk, recvErr := stream.Recv()
		if errors.Is(recvErr, io.EOF) {
			break
		}
		if recvErr != nil {
			t.Fatal(recvErr)
		}
		chunks = append(chunks, chunk.Content)
	}
	if !reflect.DeepEqual(chunks, []string{"final ", "answer"}) {
		t.Fatalf("stream chunks = %#v", chunks)
	}
	if _, err = bound.Generate(context.Background(), nil); !errors.Is(err, ErrScriptExhausted) {
		t.Fatalf("iteration exhaustion error = %v", err)
	}
	calls := modelDouble.Calls()
	if len(calls) != 3 || !reflect.DeepEqual(calls[0].ToolNames, []string{"lookup", "clock"}) || calls[2].Mode != CallModeStream {
		t.Fatalf("model calls = %#v", calls)
	}
}

func TestScriptedHarnessToolSupportsResultErrorStreamAndInterrupt(t *testing.T) {
	toolErr := errors.New("tool failed")
	toolDouble := NewScriptedTool("lookup", "scripted lookup",
		ToolStep{Result: "ok"},
		ToolStep{Err: toolErr},
		ToolStep{Chunks: []string{"a", "b"}},
		InterruptToolStep("approval required", map[string]int{"step": 1}),
	)
	var _ tool.InvokableTool = toolDouble
	var _ tool.StreamableTool = toolDouble

	if got, err := toolDouble.InvokableRun(context.Background(), `{"id":1}`); err != nil || got != "ok" {
		t.Fatalf("tool result = %q, err=%v", got, err)
	}
	if _, err := toolDouble.InvokableRun(context.Background(), `{"id":2}`); !errors.Is(err, toolErr) {
		t.Fatalf("tool error = %v", err)
	}
	stream, err := toolDouble.StreamableRun(context.Background(), `{"id":3}`)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	var chunks []string
	for {
		chunk, recvErr := stream.Recv()
		if errors.Is(recvErr, io.EOF) {
			break
		}
		if recvErr != nil {
			t.Fatal(recvErr)
		}
		chunks = append(chunks, chunk)
	}
	if !reflect.DeepEqual(chunks, []string{"a", "b"}) {
		t.Fatalf("tool stream = %#v", chunks)
	}
	if _, err = toolDouble.InvokableRun(context.Background(), `{}`); err == nil {
		t.Fatal("interrupt step returned nil error")
	} else if signal := new(adk.InterruptSignal); !errors.As(err, &signal) {
		t.Fatalf("interrupt step error = %T %v", err, err)
	}
	if len(toolDouble.Calls()) != 4 {
		t.Fatalf("tool calls = %#v", toolDouble.Calls())
	}
}
