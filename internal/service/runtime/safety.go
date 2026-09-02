package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sort"

	v1 "SentinelOps/api/runtime/v1"
	"SentinelOps/internal/ai/policy"
)

// GetSafety reads one GateEvaluator snapshot and exposes static, dynamic and
// effective closures. It is deliberately read-only and fail-closed when the
// evaluator is not configured.
func (s *RuntimeService) GetSafety(ctx context.Context) (v1.SafetyRes, error) {
	if s == nil || s.Gates == nil {
		meta := v1.ResourceMeta{Availability: v1.AvailabilityUnavailable, DataQuality: v1.DataQualityUnknown, ReasonCode: "not_observed", NotRun: true}
		return v1.SafetyRes{Item: v1.SafetyDTO{ResourceMeta: meta}, ResourceMeta: meta}, nil
	}
	state, err := s.Gates.CurrentState(ctx)
	if err != nil {
		return v1.SafetyRes{}, err
	}
	keys := func(vector map[string]bool) []string {
		result := make([]string, 0)
		for key, enabled := range vector {
			if enabled {
				result = append(result, key)
			}
		}
		sort.Strings(result)
		return result
	}
	static, dynamic, effective := keys(state.StaticCaps.Map()), keys(state.DynamicCaps.Map()), keys(state.CurrentEffective.Map())
	canonical, _ := policy.CanonicalJSON(policy.CatalogEntries())
	digest := sha256.Sum256(canonical)
	meta := completeSafetyMeta()
	auditAvailable := s.GateAuditAvailable != nil && s.GateAuditAvailable(ctx)
	if !auditAvailable {
		meta = v1.ResourceMeta{Availability: v1.AvailabilityUnavailable, DataQuality: v1.DataQualityUnknown, ReasonCode: "not_observed", NotRun: true}
	}
	item := v1.SafetyDTO{StaticCaps: static, DynamicCaps: dynamic, CurrentEffective: effective, ShadowMode: state.CurrentEffective.Enabled("agent_runtime.shadow_mode"), PolicyHash: hex.EncodeToString(digest[:]), CatalogRevision: "v1", GateAuditAvailable: auditAvailable, ResourceMeta: meta}
	return v1.SafetyRes{Item: item, ResourceMeta: meta}, nil
}
