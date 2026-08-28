package chat_pipeline

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"
)

func TestRetrieverQueryUsesCurrentTaskOnly(t *testing.T) {
	query, err := newInputToRagLambda(context.TODO(), &UserMessage{Query: "current task", History: []*schema.Message{schema.UserMessage("old history")}})
	if err != nil {
		t.Fatalf("build retriever query: %v", err)
	}
	if query != "current task" || strings.Contains(query, "old history") {
		t.Fatalf("retriever query = %q", query)
	}
}

func TestSummarizationChatLambdaDoesNotPerformInlineLLM(t *testing.T) {
	source, err := os.ReadFile("lambda_func.go")
	if err != nil {
		t.Fatalf("read chat lambda: %v", err)
	}
	text := string(source)
	for _, forbidden := range []string{"summarizeOldHistory", "newChatModel(ctx)", "memory.ChatInlineCompress"} {
		if strings.Contains(text, forbidden) {
			t.Errorf("chat lambda retains inline summarization dependency %q", forbidden)
		}
	}
}
