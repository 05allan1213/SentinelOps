package plan_pipeline

import (
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

func TestHistoryOnceWorkerAgentInputUsesTypedDurableHistory(t *testing.T) {
	got := workerAgentInputWithHistory(&adk.AgentInput{Messages: []adk.Message{schema.UserMessage("current request")}}, []*schema.Message{schema.UserMessage("durable old")})
	if got == nil || len(got.Messages) != 2 || got.Messages[0].Content != "durable old" || got.Messages[1].Content != "current request" {
		t.Fatalf("worker input = %+v", got)
	}
}

func TestHistoryOnceWorkerAgentInputDoesNotReinjectOnResume(t *testing.T) {
	history := []*schema.Message{schema.UserMessage("durable old")}
	input := workerAgentInputWithHistory(&adk.AgentInput{Messages: []adk.Message{history[0], schema.UserMessage("checkpoint request")}}, history)
	if len(input.Messages) != 2 {
		t.Fatalf("resume input = %+v", input)
	}
}
