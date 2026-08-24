package v1

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestTraceDTOIncludesUsageBreakdowns(t *testing.T) {
	b, err := json.Marshal(TraceRunVO{CachedInputTokens: 2, ReasoningTokens: 3})
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	for _, field := range []string{"cachedInputTokens", "reasoningTokens"} {
		if !strings.Contains(got, field) {
			t.Fatalf("JSON %s missing %q", got, field)
		}
	}
}
