package bootstrap

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"SentinelOps/internal/ai/effects"
	"SentinelOps/internal/ai/policy"
	"SentinelOps/internal/ai/workflow"
	appconfig "SentinelOps/internal/config"
	"SentinelOps/internal/dao/mysql"
)

func TestReconciliationProviderRetryUsesSameEffectKey(t *testing.T) {
	var calls atomic.Int32
	var idempotencyKey string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		idempotencyKey = request.Header.Get("Idempotency-Key")
		writer.Header().Set("X-Request-ID", "provider-phase25")
		writer.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	oldConfig, _ := appconfig.Current()
	appconfig.SetCurrent(&appconfig.Config{})
	defer appconfig.SetCurrent(oldConfig)

	reconciler, err := effects.NewReconciler(workflow.NewGORMStore(nil))
	if err != nil {
		t.Fatal(err)
	}
	request := `{"url":"` + server.URL + `","payload":"{}","method":"POST"}`
	claim := &workflow.ReconciliationClaim{
		Effect: mysql.AgentEffect{
			ID: "effect-phase25-provider", IdempotencyKey: "effect-phase25-provider",
			EffectRole: workflow.EffectRolePrimary, EffectStep: workflow.EffectStepPrimary,
			EffectType: string(policy.EffectProviderIdempotent), ToolName: "webhook_out",
			RequestRedacted: &request, Version: 3,
		},
		Token: workflow.LeaseToken{RunID: "run-phase25-provider", Owner: "worker-phase25-provider", Generation: 2},
	}
	resolution, err := reconciler.Query(context.Background(), claim, queryEffectTargetState, "worker-phase25-provider")
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 || idempotencyKey != claim.Effect.IdempotencyKey || resolution.Resolution != workflow.EffectResolutionExecuted || resolution.ExternalReference != "provider-phase25" {
		t.Fatalf("calls=%d key=%q resolution=%#v", calls.Load(), idempotencyKey, resolution)
	}
}

// TestReconciliationProviderRetryIsIdempotentAcrossRounds 固定生产 queryEffectTargetState 的
// provider-idempotent 语义：对账复打携带同一 Effect 幂等键；下游已生效时回放原结果、
// 不再产生第二次副作用，本地结论仍是 executed 且沿用原始 external_reference。
func TestReconciliationProviderRetryIsIdempotentAcrossRounds(t *testing.T) {
	var (
		mu        sync.Mutex
		applied   = map[string]string{}
		requests  int
		mutations int
		replays   int
		keys      []string
	)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		key := request.Header.Get("Idempotency-Key")
		mu.Lock()
		requests++
		keys = append(keys, key)
		if reference, ok := applied[key]; ok {
			replays++
			mu.Unlock()
			writer.Header().Set("X-Request-ID", reference)
			writer.WriteHeader(http.StatusOK)
			return
		}
		reference := "provider-effect-reference"
		applied[key] = reference
		mutations++
		mu.Unlock()
		writer.Header().Set("X-Request-ID", reference)
		writer.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	oldConfig, _ := appconfig.Current()
	appconfig.SetCurrent(&appconfig.Config{})
	defer appconfig.SetCurrent(oldConfig)

	reconciler, err := effects.NewReconciler(workflow.NewGORMStore(nil))
	if err != nil {
		t.Fatal(err)
	}
	request := `{"url":"` + server.URL + `","payload":"{}","method":"POST"}`
	claim := &workflow.ReconciliationClaim{
		Effect: mysql.AgentEffect{
			ID: "effect-phase25-idempotent", IdempotencyKey: "effect-phase25-idempotent",
			EffectRole: workflow.EffectRolePrimary, EffectStep: workflow.EffectStepPrimary,
			EffectType: string(policy.EffectProviderIdempotent), ToolName: "webhook_out",
			RequestRedacted: &request, Version: 3,
		},
		Token: workflow.LeaseToken{RunID: "run-phase25-idempotent", Owner: "worker-phase25-idempotent", Generation: 2},
	}
	for round := 0; round < 2; round++ {
		resolution, err := reconciler.Query(context.Background(), claim, queryEffectTargetState, "worker-phase25-idempotent")
		if err != nil {
			t.Fatal(err)
		}
		if resolution.Resolution != workflow.EffectResolutionExecuted || resolution.ExternalReference != "provider-effect-reference" {
			t.Fatalf("round %d resolution=%#v", round, resolution)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if requests != 2 || mutations != 1 || replays != 1 {
		t.Fatalf("requests=%d mutations=%d replays=%d", requests, mutations, replays)
	}
	if len(keys) != 2 || keys[0] != claim.Effect.IdempotencyKey || keys[1] != claim.Effect.IdempotencyKey {
		t.Fatalf("idempotency keys=%v want two calls with %q", keys, claim.Effect.IdempotencyKey)
	}
}
