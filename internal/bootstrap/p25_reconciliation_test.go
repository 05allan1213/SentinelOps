package bootstrap

import (
	"context"
	"net/http"
	"net/http/httptest"
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
		writer.Header().Set("X-Request-ID", "provider-p25")
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
			ID: "effect-p25-provider", IdempotencyKey: "effect-p25-provider",
			EffectRole: workflow.EffectRolePrimary, EffectStep: workflow.EffectStepPrimary,
			EffectType: string(policy.EffectProviderIdempotent), ToolName: "webhook_out",
			RequestRedacted: &request, Version: 3,
		},
		Token: workflow.LeaseToken{RunID: "run-p25-provider", Owner: "worker-p25-provider", Generation: 2},
	}
	resolution, err := reconciler.Query(context.Background(), claim, queryEffectTargetState, "worker-p25-provider")
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 || idempotencyKey != claim.Effect.IdempotencyKey || resolution.Resolution != workflow.EffectResolutionExecuted || resolution.ExternalReference != "provider-p25" {
		t.Fatalf("calls=%d key=%q resolution=%#v", calls.Load(), idempotencyKey, resolution)
	}
}
