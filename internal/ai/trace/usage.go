package trace

import (
	"encoding/json"
	"strconv"

	"github.com/cloudwego/eino/components/model"
)

type usageBreakdown struct {
	InputTokens       int
	CachedInputTokens int
	OutputTokens      int
	ReasoningTokens   int
}

func usageFromModel(tokenUsage *model.TokenUsage, extras ...map[string]any) usageBreakdown {
	if tokenUsage == nil {
		return usageBreakdown{}
	}
	usage := usageBreakdown{
		InputTokens:       tokenUsage.PromptTokens,
		CachedInputTokens: tokenUsage.PromptTokenDetails.CachedTokens,
		OutputTokens:      tokenUsage.CompletionTokens,
		ReasoningTokens:   tokenUsage.CompletionTokensDetails.ReasoningTokens,
	}
	for _, extra := range extras {
		usage = mergeUsage(usage, parseUsageBreakdown(extra))
	}
	return sanitizeUsage(usage)
}

func parseUsageBreakdown(raw map[string]any) usageBreakdown {
	if nested, ok := asStringMap(raw["usage"]); ok {
		raw = nested
	}
	usage := usageBreakdown{InputTokens: intValue(raw["prompt_tokens"]), OutputTokens: intValue(raw["completion_tokens"])}
	if details, ok := asStringMap(raw["prompt_tokens_details"]); ok {
		usage.CachedInputTokens = intValue(details["cached_tokens"])
	}
	if details, ok := asStringMap(raw["completion_tokens_details"]); ok {
		usage.ReasoningTokens = intValue(details["reasoning_tokens"])
	}
	return sanitizeUsage(usage)
}

func sanitizeUsage(usage usageBreakdown) usageBreakdown {
	if usage.InputTokens < 0 {
		usage.InputTokens = 0
	}
	if usage.OutputTokens < 0 {
		usage.OutputTokens = 0
	}
	if usage.CachedInputTokens < 0 {
		usage.CachedInputTokens = 0
	}
	if usage.CachedInputTokens > usage.InputTokens {
		usage.CachedInputTokens = usage.InputTokens
	}
	if usage.ReasoningTokens < 0 {
		usage.ReasoningTokens = 0
	}
	if usage.ReasoningTokens > usage.OutputTokens {
		usage.ReasoningTokens = usage.OutputTokens
	}
	return usage
}

func mergeUsage(base, details usageBreakdown) usageBreakdown {
	if details.InputTokens > 0 {
		base.InputTokens = details.InputTokens
	}
	if details.OutputTokens > 0 {
		base.OutputTokens = details.OutputTokens
	}
	if details.CachedInputTokens > 0 {
		base.CachedInputTokens = details.CachedInputTokens
	}
	if details.ReasoningTokens > 0 {
		base.ReasoningTokens = details.ReasoningTokens
	}
	return sanitizeUsage(base)
}

func asStringMap(value any) (map[string]any, bool) {
	switch typed := value.(type) {
	case map[string]any:
		return typed, true
	case map[any]any:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			if text, ok := key.(string); ok {
				out[text] = item
			}
		}
		return out, true
	default:
		return nil, false
	}
}

func intValue(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int32:
		return int(typed)
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case json.Number:
		n, _ := typed.Int64()
		return int(n)
	case string:
		n, _ := strconv.Atoi(typed)
		return n
	default:
		return 0
	}
}
