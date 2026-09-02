package runtime

import (
	"context"
	"testing"

	airuntime "SentinelOps/internal/ai/runtime"
)

func TestSafetyShowsShadowAndEffectiveWrites(t *testing.T) {
	values := map[string]bool{}
	for _, key := range airuntime.CanonicalGateKeys() {
		values[key] = true
	}
	static, _ := airuntime.NewGateVector(values)
	evaluator, _ := airuntime.NewGateEvaluator(static, func(context.Context, []string) (map[string]string, error) {
		out := map[string]string{}
		for _, key := range airuntime.CanonicalGateKeys() {
			out[key] = "true"
		}
		return out, nil
	})
	res, err := (&RuntimeService{Gates: evaluator}).GetSafety(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !res.Item.ShadowMode {
		t.Fatal("shadow mode not shown")
	}
	for _, key := range res.Item.CurrentEffective {
		if key == airuntime.GateAgentRuntimeL1Writes || key == airuntime.GateAgentRuntimeL2Writes {
			t.Fatal("writes must close in shadow")
		}
	}
}

func TestSafetyUnknownObservationIsUnavailable(t *testing.T) {
	res, err := (&RuntimeService{}).GetSafety(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Availability != "unavailable" || !res.NotRun || res.ReasonCode != "not_observed" {
		t.Fatalf("meta=%+v", res.ResourceMeta)
	}
}
