package runtime

import (
	"testing"

	"github.com/cloudwego/eino/schema"
)

func TestBudgetModelUsageActualIncludesCachedAndReasoningOnce(t *testing.T) {
	message := &schema.Message{ResponseMeta: &schema.ResponseMeta{Usage: &schema.TokenUsage{
		PromptTokens: 12, CompletionTokens: 9,
		PromptTokenDetails:      schema.PromptTokenDetails{CachedTokens: 4},
		CompletionTokensDetails: schema.CompletionTokensDetails{ReasoningTokens: 3},
	}}}
	actual, quality := modelBudgetActual(message)
	if quality != "reliable" || actual == nil || actual.InputTokens != 12 || actual.CachedInputTokens != 4 || actual.OutputTokens != 9 || actual.ReasoningTokens != 3 {
		t.Fatalf("actual=%#v quality=%q", actual, quality)
	}
}

func TestUsageUnknownWhenModelUsageMissing(t *testing.T) {
	actual, quality := modelBudgetActual(&schema.Message{})
	if actual != nil || quality != "unknown" {
		t.Fatalf("actual=%#v quality=%q", actual, quality)
	}
}
