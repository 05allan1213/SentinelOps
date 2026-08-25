package base

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

func TestSpecialistGenModelInputRetrievesOnceAndInjectsCurrentQueryOnce(t *testing.T) {
	calls := 0
	gen := NewSpecialistGenModelInput(SpecialistPromptConfig{
		Instruction: "instruction {date} {documents}",
		Retrieval: func(_ context.Context, input *UserMessage, _ RetrievalOptions) ([]*schema.Document, error) {
			calls++
			if input.Query != "current query" {
				t.Fatalf("query = %q", input.Query)
			}
			if !reflect.DeepEqual(input.History, []*schema.Message{schema.UserMessage("history")}) {
				t.Fatalf("history = %#v", input.History)
			}
			return []*schema.Document{{ID: "doc-1", Content: "evidence"}}, nil
		},
	})
	messages, err := gen(context.Background(), "ignored", &adk.AgentInput{Messages: []*schema.Message{
		schema.UserMessage("history"), schema.UserMessage("current query"),
	}})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("retrieval calls = %d, want 1", calls)
	}
	if len(messages) != 3 || messages[0].Role != schema.System || messages[1].Content != "history" || messages[2].Content != "current query" {
		t.Fatalf("messages = %#v", messages)
	}
	if got := messages[0].Content; !containsAll(got, "evidence") {
		t.Fatalf("system input = %q", got)
	}
	if got := strings.Count(messages[0].Content+messages[1].Content+messages[2].Content, "current query"); got != 1 {
		t.Fatalf("current query occurrences = %d, want 1", got)
	}
}

func containsAll(value string, parts ...string) bool {
	for _, part := range parts {
		if !strings.Contains(value, part) {
			return false
		}
	}
	return true
}
