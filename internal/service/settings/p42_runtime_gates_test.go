package settingssvc

import (
	"context"
	"errors"
	"testing"

	"SentinelOps/internal/ai/policy"
	airuntime "SentinelOps/internal/ai/runtime"
	appconfig "SentinelOps/internal/config"
)

func TestEffectiveGateUpdateRequiresExactCanonicalVector(t *testing.T) {
	complete := make(map[string]bool, len(airuntime.CanonicalGateKeys()))
	for _, key := range airuntime.CanonicalGateKeys() {
		complete[key] = false
	}
	if _, err := ValidateRuntimeGateUpdate(complete); err != nil {
		t.Fatalf("complete canonical vector rejected: %v", err)
	}
	missing := cloneP42GateMap(complete)
	delete(missing, airuntime.GateSkillEnabled)
	if _, err := ValidateRuntimeGateUpdate(missing); err == nil {
		t.Fatal("missing runtime Gate was accepted")
	}
	alias := cloneP42GateMap(complete)
	delete(alias, airuntime.GateLangfuseEnabled)
	alias["observability.langfuse.enabled"] = false
	if _, err := ValidateRuntimeGateUpdate(alias); err == nil {
		t.Fatal("obsolete Langfuse alias was accepted")
	}
}

func TestEffectiveGateSettingsRejectNonAdmin(t *testing.T) {
	ctx := policy.WithIdentity(context.Background(), policy.Identity{
		UserID: "operator-p42", Role: policy.RoleOperator, Scope: policy.Scope{UserID: "operator-p42"},
	})
	_, err := GetRuntimeGates(ctx, &appconfig.Config{})
	if !errors.Is(err, policy.ErrForbidden) {
		t.Fatalf("GetRuntimeGates error=%v, want forbidden", err)
	}
}

func cloneP42GateMap(input map[string]bool) map[string]bool {
	result := make(map[string]bool, len(input))
	for key, value := range input {
		result[key] = value
	}
	return result
}
