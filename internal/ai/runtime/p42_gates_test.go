package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"SentinelOps/internal/ai/effects"
	"SentinelOps/internal/ai/policy"
	aitrace "SentinelOps/internal/ai/trace"
	"SentinelOps/internal/ai/workflow"
	appconfig "SentinelOps/internal/config"
	"SentinelOps/internal/dao/mysql"
)

func TestEffectiveGateUsesExactCanonicalKeysAndFailsClosed(t *testing.T) {
	wantKeys := []string{
		GateAgentRuntimeEnabled,
		GateAgentRuntimeAcceptNewRuns,
		GateAgentRuntimeShadowMode,
		GateAgentRuntimeL1Writes,
		GateAgentRuntimeL2Writes,
		GateAgentRuntimeAdminQueryDatabaseDebug,
		GateMCPEnabled,
		GateSkillEnabled,
		GateLangfuseEnabled,
	}
	if got := CanonicalGateKeys(); !reflect.DeepEqual(got, wantKeys) {
		t.Fatalf("canonical gates = %#v, want %#v", got, wantKeys)
	}

	static := allOpenP42StaticCaps()
	cases := []struct {
		name   string
		values map[string]string
		gate   string
		want   bool
	}{
		{name: "all true", values: p42DynamicValues("true"), gate: GateMCPEnabled, want: true},
		{name: "explicit false", values: p42DynamicValues("true"), gate: GateSkillEnabled, want: false},
		{name: "missing", values: p42DynamicValues("true"), gate: GateLangfuseEnabled, want: false},
		{name: "illegal", values: p42DynamicValues("true"), gate: GateAgentRuntimeEnabled, want: false},
		{name: "unknown alias", values: p42DynamicValues("true"), gate: GateAgentRuntimeAcceptNewRuns, want: false},
	}
	cases[1].values[GateSkillEnabled] = "false"
	delete(cases[2].values, GateLangfuseEnabled)
	cases[3].values[GateAgentRuntimeEnabled] = "1"
	cases[4].values["observability.langfuse.enabled"] = "true"

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			evaluator, err := NewGateEvaluator(static, func(context.Context, []string) (map[string]string, error) {
				return tc.values, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			current, err := evaluator.Current(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if got := current.Enabled(tc.gate); got != tc.want {
				t.Fatalf("gate %s = %t, want %t; vector=%v", tc.gate, got, tc.want, current.Map())
			}
			if tc.name == "unknown alias" {
				for _, key := range CanonicalGateKeys() {
					if current.Enabled(key) {
						t.Fatalf("unknown dynamic key did not close vector: %v", current.Map())
					}
				}
			}
		})
	}
}

func TestEffectiveGateTruthTableForEveryCanonicalKey(t *testing.T) {
	for _, key := range CanonicalGateKeys() {
		t.Run(key, func(t *testing.T) {
			baseline := p42GateBools(true)
			baseline[GateAgentRuntimeShadowMode] = key == GateAgentRuntimeShadowMode
			assertP42EffectiveGate(t, key, baseline, baseline, baseline, true)

			frozenClosed := cloneP42BoolMap(baseline)
			frozenClosed[key] = false
			assertP42EffectiveGate(t, key, frozenClosed, baseline, baseline, false)

			staticClosed := cloneP42BoolMap(baseline)
			staticClosed[key] = false
			assertP42EffectiveGate(t, key, baseline, staticClosed, baseline, false)

			dynamicClosed := cloneP42BoolMap(baseline)
			dynamicClosed[key] = false
			assertP42EffectiveGate(t, key, baseline, baseline, dynamicClosed, false)
		})
	}
}

func TestEffectiveGateCurrentStateUsesOneDynamicRead(t *testing.T) {
	reads := 0
	evaluator, err := NewGateEvaluator(allOpenP42StaticCaps(), func(context.Context, []string) (map[string]string, error) {
		reads++
		return p42DynamicValues("true"), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	state, err := evaluator.CurrentState(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if reads != 1 || !state.DynamicCaps.Enabled(GateMCPEnabled) || !state.CurrentEffective.Enabled(GateMCPEnabled) {
		t.Fatalf("reads=%d state=%+v", reads, state)
	}
}

func TestFrozenSnapshotCannotBeReopenedByCurrentGate(t *testing.T) {
	input := p11SnapshotInput()
	input.FeatureGates[GateAgentRuntimeL1Writes] = false
	frozen, err := FreezeRuntimeSnapshot(input)
	if err != nil {
		t.Fatal(err)
	}
	evaluator, err := NewGateEvaluator(allOpenP42StaticCaps(), func(context.Context, []string) (map[string]string, error) {
		return p42DynamicValues("true"), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	effective, err := evaluator.Effective(context.Background(), frozen)
	if err != nil {
		t.Fatal(err)
	}
	if effective.Enabled(GateAgentRuntimeL1Writes) {
		t.Fatalf("current gate reopened frozen=false capability: %v", effective.Map())
	}
}

func TestShadowModeClosesL1AndL2Writes(t *testing.T) {
	values := p42DynamicValues("true")
	evaluator, err := NewGateEvaluator(allOpenP42StaticCaps(), func(context.Context, []string) (map[string]string, error) {
		return values, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	current, err := evaluator.Current(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !current.Enabled(GateAgentRuntimeShadowMode) || current.Enabled(GateAgentRuntimeL1Writes) || current.Enabled(GateAgentRuntimeL2Writes) {
		t.Fatalf("shadow vector permits mutation: %v", current.Map())
	}
}

func TestLegacyCompatibilityGateRequiresEnabledAndShadow(t *testing.T) {
	cases := []struct {
		name          string
		staticEnabled bool
		staticShadow  bool
		dynamic       map[string]string
		readErr       error
		want          bool
	}{
		{name: "enabled shadow", staticEnabled: true, staticShadow: true, dynamic: p42DynamicValues("true"), want: true},
		{name: "enabled closed", staticEnabled: true, staticShadow: true, dynamic: p42DynamicValues("true")},
		{name: "shadow closed", staticEnabled: true, staticShadow: true, dynamic: p42DynamicValues("true")},
		{name: "static enabled closed", staticShadow: true, dynamic: p42DynamicValues("true")},
		{name: "static shadow closed", staticEnabled: true, dynamic: p42DynamicValues("true")},
		{name: "missing", staticEnabled: true, staticShadow: true, dynamic: p42DynamicValues("true")},
		{name: "illegal", staticEnabled: true, staticShadow: true, dynamic: p42DynamicValues("true")},
		{name: "read error", staticEnabled: true, staticShadow: true, dynamic: p42DynamicValues("true"), readErr: errors.New("read failed")},
	}
	cases[1].dynamic[GateAgentRuntimeEnabled] = "false"
	cases[2].dynamic[GateAgentRuntimeShadowMode] = "false"
	delete(cases[5].dynamic, GateAgentRuntimeEnabled)
	cases[6].dynamic[GateAgentRuntimeShadowMode] = "1"

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			static := allOpenP42StaticCaps()
			static.AgentRuntimeEnabled = tc.staticEnabled
			static.AgentRuntimeShadowMode = tc.staticShadow
			evaluator, err := NewGateEvaluator(static, func(context.Context, []string) (map[string]string, error) {
				return tc.dynamic, tc.readErr
			})
			if err != nil {
				t.Fatal(err)
			}
			if got := evaluator.AllowLegacyCompatibility(context.Background()); got != tc.want {
				t.Fatalf("AllowLegacyCompatibility=%t, want %t", got, tc.want)
			}
			if got := evaluator.AllowLegacyOpsWrites(context.Background()); got != tc.want {
				t.Fatalf("AllowLegacyOpsWrites=%t, want %t", got, tc.want)
			}
		})
	}

	var evaluator *GateEvaluator
	if evaluator.AllowLegacyCompatibility(context.Background()) || evaluator.AllowLegacyOpsWrites(context.Background()) {
		t.Fatal("nil Gate evaluator opened legacy compatibility")
	}
}

func TestRollbackCompatibilityUsesRunFrozenSnapshot(t *testing.T) {
	input := p11SnapshotInput()
	frozen, err := FreezeRuntimeSnapshot(input)
	if err != nil {
		t.Fatal(err)
	}
	if frozen.RuntimeVersion() == "" || frozen.Gates().Enabled("unknown") {
		t.Fatalf("frozen runtime identity/gates are not exact: version=%q gates=%v", frozen.RuntimeVersion(), frozen.Gates().Map())
	}
	if got := RecoveryCompatibilityHash(frozen); got != frozen.CompatibilityHash() {
		t.Fatalf("recovery hash = %q, want Run frozen hash %q", got, frozen.CompatibilityHash())
	}
}

func TestRollbackCompatibilityReleaseRuntimeVersionIsImmutable(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	t.Setenv(RuntimeVersionEnv, digest)
	if err := ValidateCurrentRuntimeVersion(); err != nil {
		t.Fatalf("immutable release runtime version rejected: %v", err)
	}
	if got := CurrentRuntimeVersionSnapshot().App; got != digest {
		t.Fatalf("runtime version app=%q, want %q", got, digest)
	}

	for _, value := range []string{"development", "latest", "sha256:xyz", strings.Repeat("A", 40)} {
		t.Run(value, func(t *testing.T) {
			t.Setenv(RuntimeVersionEnv, value)
			if err := ValidateCurrentRuntimeVersion(); err == nil {
				t.Fatalf("mutable runtime version %q was accepted", value)
			}
		})
	}
}

func TestRollbackCompatibilityCurrentStaticGateDoesNotRewriteFrozenHash(t *testing.T) {
	config := &appconfig.Config{MCP: appconfig.MCPConfig{Enabled: true}}
	frozenGates := p42GateBools(false)
	frozenGates[GateMCPEnabled] = true
	vector, err := NewGateVector(frozenGates)
	if err != nil {
		t.Fatal(err)
	}
	before, err := BuildDurableRuntimeSnapshotWithSkillsAndGates(config, nil, vector)
	if err != nil {
		t.Fatal(err)
	}
	config.MCP.Enabled = false
	after, err := BuildDurableRuntimeSnapshotWithSkillsAndGates(config, nil, vector)
	if err != nil {
		t.Fatal(err)
	}
	if before.CompatibilityHash() != after.CompatibilityHash() {
		t.Fatalf("current static MCP cap rewrote frozen compatibility: before=%s after=%s", before.CompatibilityHash(), after.CompatibilityHash())
	}
}

func TestEffectiveGateLangfuseClosedSkipsAttemptFactory(t *testing.T) {
	input := p11SnapshotInput()
	input.FeatureGates[GateLangfuseEnabled] = true
	frozen, err := FreezeRuntimeSnapshot(input)
	if err != nil {
		t.Fatal(err)
	}
	values := p42DynamicValues("true")
	values[GateAgentRuntimeShadowMode] = "false"
	values[GateLangfuseEnabled] = "false"
	evaluator, err := NewGateEvaluator(allOpenP42StaticCaps(), func(context.Context, []string) (map[string]string, error) {
		return values, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	executor := &DurableExecutor{
		gates: evaluator,
		loadLangfuse: func(context.Context) (*aitrace.LangfuseRuntime, error) {
			calls++
			return nil, nil
		},
	}
	runtime, err := executor.attemptLangfuse(context.Background(), frozen)
	if err != nil || runtime != nil || calls != 0 {
		t.Fatalf("closed Langfuse Gate runtime=%v calls=%d err=%v", runtime, calls, err)
	}
}

func TestRollbackCompatibilityWorkerClaimsExactRuntimeVersion(t *testing.T) {
	db := p12NewRuntimeDatabase(t, "p42_runtime_version")
	store := workflow.NewGORMStore(db)
	identity := policy.Identity{UserID: "admin-p42-version", Role: policy.RoleAdmin, Scope: policy.Scope{UserID: "admin-p42-version"}}
	ctx := policy.WithIdentity(context.Background(), identity)

	mismatchInput := p11SnapshotInput()
	mismatchInput.Runtime.App = "compatibility-image"
	mismatch, err := FreezeRuntimeSnapshot(mismatchInput)
	if err != nil {
		t.Fatal(err)
	}
	currentInput := p11SnapshotInput()
	currentInput.Runtime.App = "current-image"
	current, err := FreezeRuntimeSnapshot(currentInput)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		id       string
		session  string
		snapshot FrozenRuntimeSnapshot
	}{
		{id: "a-mismatch-p42", session: "session-mismatch-p42", snapshot: mismatch},
		{id: "b-current-p42", session: "session-current-p42", snapshot: current},
	} {
		if _, err := store.CreateRunWithSessionLock(ctx, workflow.CreateRunInput{
			ID: item.id, WorkflowKey: "p42.runtime-version", SessionID: item.session,
			QueryText: "version", ImmutableInputJSON: json.RawMessage(`{"agent":"plan_agent","query":"version"}`),
			RuntimeSnapshot: item.snapshot.WorkflowFields(), BudgetLimitsJSON: json.RawMessage(`{}`),
			DeadlineAt: time.Now().Add(time.Hour), CreatedEvent: workflow.WorkflowEventInput{Type: workflow.EventRunCreated},
		}); err != nil {
			t.Fatal(err)
		}
	}
	claimed, ok, err := store.ClaimNextRun(context.Background(), workflow.ClaimInput{
		Owner: "worker-current-p42", LeaseDuration: time.Minute, RuntimeVersion: current.RuntimeVersion(),
	})
	if err != nil || !ok || claimed.Run.ID != "b-current-p42" {
		t.Fatalf("exact runtime claim = %#v ok=%t err=%v", claimed, ok, err)
	}
	if other, ok, err := store.ClaimNextRun(context.Background(), workflow.ClaimInput{
		Owner: "worker-unknown-p42", LeaseDuration: time.Minute, RuntimeVersion: `{"app":"unknown","eino":"v0","go":"go0"}`,
	}); err != nil || ok || other != nil {
		t.Fatalf("mismatched runtime claimed Run: %#v ok=%t err=%v", other, ok, err)
	}
}

func TestFrozenSnapshotExistingRunStillClaimsWhenAcceptNewRunsCloses(t *testing.T) {
	db := p12NewRuntimeDatabase(t, "p42_accept_closed_existing")
	store := workflow.NewGORMStore(db)
	identity := policy.Identity{UserID: "admin-p42-existing", Role: policy.RoleAdmin, Scope: policy.Scope{UserID: "admin-p42-existing"}}
	ctx := policy.WithIdentity(context.Background(), identity)
	input := p11SnapshotInput()
	input.FeatureGates[GateAgentRuntimeEnabled] = true
	input.FeatureGates[GateAgentRuntimeAcceptNewRuns] = true
	frozen, err := FreezeRuntimeSnapshot(input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateRunWithSessionLock(ctx, workflow.CreateRunInput{
		ID: "existing-p42", WorkflowKey: "p42.accept-closed", SessionID: "session-existing-p42",
		QueryText: "existing", ImmutableInputJSON: json.RawMessage(`{"agent":"plan_agent","query":"existing"}`),
		RuntimeSnapshot: frozen.WorkflowFields(), BudgetLimitsJSON: json.RawMessage(`{}`),
		DeadlineAt: time.Now().Add(time.Hour), CreatedEvent: workflow.WorkflowEventInput{Type: workflow.EventRunCreated},
	}); err != nil {
		t.Fatal(err)
	}
	dynamic := p42DynamicValues("true")
	dynamic[GateAgentRuntimeShadowMode] = "false"
	dynamic[GateAgentRuntimeAcceptNewRuns] = "false"
	evaluator, err := NewGateEvaluator(allOpenP42StaticCaps(), func(context.Context, []string) (map[string]string, error) {
		return dynamic, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	worker, err := NewWorker(store, WorkerConfig{
		Owner: "worker-existing-p42", LeaseDuration: time.Minute,
		MinPollBackoff: time.Millisecond, MaxPollBackoff: 10 * time.Millisecond,
		RuntimeVersion: frozen.RuntimeVersion(), Gates: evaluator,
	})
	if err != nil {
		t.Fatal(err)
	}
	claimed, ok, err := worker.ClaimNext(context.Background())
	if err != nil || !ok || claimed.Run.ID != "existing-p42" {
		t.Fatalf("existing Run did not converge after accept_new_runs closed: claimed=%#v ok=%t err=%v", claimed, ok, err)
	}
}

func TestEffectiveGateRuntimeDisabledSkipsEffectReconciliationClaim(t *testing.T) {
	db := p12NewRuntimeDatabase(t, "p42_runtime_disabled_reconciliation")
	store, runCtx, runID, _ := p25UnknownEffectFixture(t, db, "p42-runtime-disabled", policy.EffectReconcilable)
	var run mysql.WorkflowRun
	if err := db.First(&run, "id = ?", runID).Error; err != nil {
		t.Fatal(err)
	}
	values := p42DynamicValues("true")
	values[GateAgentRuntimeEnabled] = "false"
	values[GateAgentRuntimeShadowMode] = "false"
	evaluator, err := NewGateEvaluator(allOpenP42StaticCaps(), func(context.Context, []string) (map[string]string, error) {
		return values, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	queryCalls := 0
	worker, err := NewWorker(store, WorkerConfig{
		Owner: "worker-p42-runtime-disabled", LeaseDuration: time.Minute,
		MinPollBackoff: time.Millisecond, MaxPollBackoff: 10 * time.Millisecond,
		RuntimeVersion: *run.RuntimeVersion, Gates: evaluator,
		ClaimNext: func(context.Context) (*workflow.ClaimedRun, bool, error) { return nil, false, nil },
		Execute: func(context.Context, *workflow.ClaimedRun) (RunExecutionResult, error) {
			return RunExecutionResult{}, nil
		},
		QueryEffectTargetState: func(context.Context, effects.ReconciliationTarget) (effects.TargetState, error) {
			queryCalls++
			return effects.TargetState{Known: true, Applied: true}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	worked, err := worker.RunOnce(runCtx)
	if err != nil || worked || queryCalls != 0 {
		t.Fatalf("disabled Runtime worked=%t query_calls=%d err=%v", worked, queryCalls, err)
	}
	var effect mysql.AgentEffect
	if err := db.First(&effect, "run_id = ?", runID).Error; err != nil {
		t.Fatal(err)
	}
	if effect.Status != workflow.EffectStatusUnknown {
		t.Fatalf("disabled Runtime claimed reconciliation Effect: status=%s", effect.Status)
	}
}

func allOpenP42StaticCaps() GateVector {
	return StaticGateCaps(&appconfig.Config{
		AgentRuntime: appconfig.AgentRuntime{
			Enabled: true, AcceptNewRuns: true, ShadowMode: true,
			L1Writes: true, L2Writes: true, AdminQueryDatabaseDebug: true,
		},
		MCP:           appconfig.MCPConfig{Enabled: true},
		Skill:         appconfig.SkillConfig{Enabled: true},
		Observability: appconfig.ObservabilityConfig{Langfuse: appconfig.LangfuseConfig{Enabled: true}},
	})
}

func p42DynamicValues(value string) map[string]string {
	result := make(map[string]string, len(CanonicalGateKeys()))
	for _, key := range CanonicalGateKeys() {
		result[key] = value
	}
	return result
}

func p42GateBools(value bool) map[string]bool {
	result := make(map[string]bool, len(CanonicalGateKeys()))
	for _, key := range CanonicalGateKeys() {
		result[key] = value
	}
	return result
}

func cloneP42BoolMap(input map[string]bool) map[string]bool {
	result := make(map[string]bool, len(input))
	for key, value := range input {
		result[key] = value
	}
	return result
}

func assertP42EffectiveGate(t *testing.T, key string, frozenValues, staticValues, dynamicValues map[string]bool, want bool) {
	t.Helper()
	input := p11SnapshotInput()
	input.FeatureGates = cloneP42BoolMap(frozenValues)
	frozen, err := FreezeRuntimeSnapshot(input)
	if err != nil {
		t.Fatal(err)
	}
	static, err := NewGateVector(cloneP42BoolMap(staticValues))
	if err != nil {
		t.Fatal(err)
	}
	dynamic := make(map[string]string, len(dynamicValues))
	for current, enabled := range dynamicValues {
		dynamic[current] = fmt.Sprintf("%t", enabled)
	}
	evaluator, err := NewGateEvaluator(static, func(context.Context, []string) (map[string]string, error) {
		return dynamic, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	effective, err := evaluator.Effective(context.Background(), frozen)
	if err != nil {
		t.Fatal(err)
	}
	if got := effective.Enabled(key); got != want {
		t.Fatalf("effective %s=%t want=%t frozen=%v static=%v dynamic=%v", key, got, want, frozenValues, staticValues, dynamicValues)
	}
}
