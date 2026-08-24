package trace

import (
	"math"
	"testing"

	"github.com/cloudwego/eino/components/model"
)

func TestEstimateTokenCostDoesNotDoubleCountBreakdowns(t *testing.T) {
	pricing := costConfig{Input: 12, CachedInput: 2.4, Output: 36}
	tests := []struct {
		name      string
		input     int64
		cached    int64
		output    int64
		reasoning int64
		want      float64
	}{
		{"split cached input", 1_000_000, 250_000, 500_000, 100_000, 27.6},
		{"cached exceeds input is clamped", 100, 200, 0, 0, 0.00024},
		{"negative details are zero", 100, -1, 50, -1, 0.003},
		{"negative totals are zero", -1, 0, -1, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := estimateTokenCost(pricing, tt.input, tt.cached, tt.output, tt.reasoning)
			if math.Abs(got-tt.want) > 1e-9 {
				t.Fatalf("estimateTokenCost() = %.12f, want %.12f", got, tt.want)
			}
		})
	}
}

func TestParseUsageBreakdownSupportsProviderDetails(t *testing.T) {
	got := parseUsageBreakdown(map[string]any{
		"prompt_tokens":             100,
		"prompt_tokens_details":     map[string]any{"cached_tokens": 40},
		"completion_tokens":         80,
		"completion_tokens_details": map[string]any{"reasoning_tokens": 30},
	})
	if got.InputTokens != 100 || got.CachedInputTokens != 40 || got.OutputTokens != 80 || got.ReasoningTokens != 30 {
		t.Fatalf("usage = %#v", got)
	}
}

func TestUsageFromModelKeepsCachedAndReasoningDetails(t *testing.T) {
	got := usageFromModel(&model.TokenUsage{
		PromptTokens:            100,
		PromptTokenDetails:      model.PromptTokenDetails{CachedTokens: 40},
		CompletionTokens:        80,
		CompletionTokensDetails: model.CompletionTokensDetails{ReasoningTokens: 30},
	})
	if got.InputTokens != 100 || got.CachedInputTokens != 40 || got.OutputTokens != 80 || got.ReasoningTokens != 30 {
		t.Fatalf("usage = %#v", got)
	}
}
