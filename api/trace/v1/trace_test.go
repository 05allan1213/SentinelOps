package v1

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestTraceDTOIncludesUsageBreakdowns(t *testing.T) {
	b, err := json.Marshal(struct {
		Run      TraceRunVO      `json:"run"`
		Stats    StatsRes        `json:"stats"`
		Overview CostOverviewRes `json:"overview"`
		Daily    DailyCostPoint  `json:"daily"`
		Model    ModelCostItem   `json:"model"`
		Trend    TokenTrendPoint `json:"trend"`
	}{
		Run:      TraceRunVO{CachedInputTokens: 2, ReasoningTokens: 3},
		Stats:    StatsRes{CachedInputTokens: 2, ReasoningTokens: 3},
		Overview: CostOverviewRes{CachedInputTokens: 2, ReasoningTokens: 3},
		Daily:    DailyCostPoint{CachedInputTokens: 2, ReasoningTokens: 3},
		Model:    ModelCostItem{CachedInputTokens: 2, ReasoningTokens: 3},
		Trend:    TokenTrendPoint{CachedInputTokens: 2, ReasoningTokens: 3},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	for _, field := range []string{"cachedInputTokens", "reasoningTokens"} {
		if strings.Count(got, field) != 6 {
			t.Fatalf("JSON %s missing %q", got, field)
		}
	}
}
