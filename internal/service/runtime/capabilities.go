package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	v1 "SentinelOps/api/runtime/v1"
	"SentinelOps/internal/ai/agent/skill_pipeline"
	"SentinelOps/internal/ai/policy"
	airuntime "SentinelOps/internal/ai/runtime"
	mcptools "SentinelOps/internal/ai/tools/mcp"
	"SentinelOps/internal/dao/mysql"
)

// GetCapabilities returns the static, read-only capability catalog. Runtime
// worker state is joined only from persisted snapshots; process-local handles
// and configuration secrets never enter this DTO.
func (s *RuntimeService) GetCapabilities(ctx context.Context) (v1.CapabilitiesRes, error) {
	items := make([]v1.CapabilityDTO, 0)
	skillValidationErr := false
	mcpEnabled, skillEnabled := false, false
	if s != nil && s.Gates != nil {
		state, err := s.Gates.CurrentState(ctx)
		if err != nil {
			return v1.CapabilitiesRes{}, err
		}
		mcpEnabled, skillEnabled = state.CurrentEffective.Enabled(airuntime.GateMCPEnabled), state.CurrentEffective.Enabled(airuntime.GateSkillEnabled)
	}
	snapshotTools := map[string]airuntime.ToolSnapshot{}
	agentInventory := policy.DurableToolAgents()
	if s != nil {
		for _, tool := range s.Snapshot.Tools() {
			snapshotTools[tool.Name] = tool
		}
	}
	for _, source := range []struct {
		name  string
		names []string
	}{
		{"local", policy.RequiredDurableToolNames()},
		{"agent_tool", policy.DurableFrameworkToolNames()},
	} {
		for _, name := range source.names {
			entry, err := policy.LookupCatalog(name)
			if err != nil {
				return v1.CapabilitiesRes{}, err
			}
			revision, schemaHash := entry.Revision, entry.SchemaHash
			if snap, ok := snapshotTools[name]; ok {
				revision, schemaHash = snap.Revision, snap.SchemaHash
			}
			allowed := append([]string(nil), agentInventory[name]...)
			if source.name == "agent_tool" {
				allowed = []string{name}
			}
			items = append(items, v1.CapabilityDTO{Name: name, Source: source.name, Risk: string(entry.Risk), Revision: revision, SchemaHash: schemaHash, EffectType: string(entry.EffectType), RequiredGate: entry.RequiredGate, RequiredRole: string(entry.RequiredRole), AllowedAgents: allowed, ConfiguredState: "configured", ObservedWorkerState: "not_applicable", ResourceMeta: completeSafetyMeta()})
		}
	}
	if s != nil && s.Config != nil {
		if cfg, err := mcptools.FromAppConfig(s.Config); err == nil {
			for _, server := range cfg.Servers {
				state := "disabled"
				if server.Enabled && cfg.Enabled && mcpEnabled {
					state = "enabled"
				}
				items = append(items, v1.CapabilityDTO{Name: server.Name, Source: "mcp", Risk: string(policy.RiskL0), Revision: "v1", SchemaHash: "", AllowedAgents: []string{"mcp_agent"}, ConfiguredState: state, ObservedWorkerState: "unknown", ResourceMeta: unavailableObservedMeta()})
			}
		} else {
			return v1.CapabilitiesRes{}, err
		}
		if s.Config.Skill.Enabled {
			if skills, err := skill_pipeline.BuildConfiguredSkillSnapshots(ctx, s.Config); err == nil {
				for _, skill := range skills {
					configured := "disabled"
					if skillEnabled {
						configured = "enabled"
					}
					items = append(items, v1.CapabilityDTO{Name: skill.Name, Source: "skill", Risk: string(policy.RiskL0), Revision: "v1", SchemaHash: skill.ContentHash, AllowedAgents: []string{"skill_agent"}, ConfiguredState: configured, ObservedWorkerState: "unknown", ResourceMeta: unavailableObservedMeta()})
				}
			} else {
				skillValidationErr = true
			}
		}
	}
	if s != nil && len(s.Snapshot.Skills()) > 0 {
		seen := make(map[string]struct{})
		for _, item := range items {
			if item.Source == "skill" {
				seen[item.Name] = struct{}{}
			}
		}
		for _, skill := range s.Snapshot.Skills() {
			if _, ok := seen[skill.Name]; ok {
				continue
			}
			configured := "disabled"
			if skillEnabled {
				configured = "enabled"
			}
			items = append(items, v1.CapabilityDTO{Name: skill.Name, Source: "skill", Risk: string(policy.RiskL0), Revision: "v1", SchemaHash: skill.ContentHash, AllowedAgents: []string{"skill_agent"}, ConfiguredState: configured, ObservedWorkerState: "unknown", ResourceMeta: unavailableObservedMeta()})
		}
	}
	observed, observationErr := s.loadWorkerObservations(ctx)
	for i := range items {
		if items[i].Source != "mcp" && items[i].Source != "skill" {
			continue
		}
		if obs, ok := observed[observedKey{source: items[i].Source, name: items[i].Name}]; ok {
			items[i].ObservedWorkerState = obs.status
			items[i].LastObservedAt = obs.at
			items[i].ResourceMeta = completeSafetyMeta()
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Source != items[j].Source {
			return items[i].Source < items[j].Source
		}
		return items[i].Name < items[j].Name
	})
	meta := completeSafetyMeta()
	if observationErr != nil {
		reason := "worker_snapshot_unavailable"
		if strings.Contains(observationErr.Error(), "malformed") {
			reason = "worker_snapshot_malformed"
		}
		meta = v1.ResourceMeta{Availability: v1.AvailabilityUnavailable, DataQuality: v1.DataQualityUnknown, ReasonCode: reason, NotRun: true}
	} else if skillValidationErr {
		meta = v1.ResourceMeta{Availability: v1.AvailabilityUnavailable, DataQuality: v1.DataQualityUnknown, ReasonCode: "skill_validation_unavailable", NotRun: true}
	}
	if observationErr == nil && !skillValidationErr {
		for _, item := range items {
			if item.Availability == v1.AvailabilityUnavailable {
				meta = unavailableObservedMeta()
				break
			}
		}
	}
	return v1.CapabilitiesRes{Items: items, Page: v1.PageMeta{Page: 1, PageSize: len(items), Total: int64(len(items))}, ResourceMeta: meta}, nil
}

type observedKey struct{ source, name string }
type observedCapability struct {
	status string
	at     *time.Time
}

func (s *RuntimeService) loadWorkerObservations(ctx context.Context) (map[observedKey]observedCapability, error) {
	result := make(map[observedKey]observedCapability)
	if s == nil || s.Store == nil || s.Store.DB() == nil {
		return result, nil
	}
	var rows []mysql.RuntimeWorkerSnapshot
	if err := s.Store.DB().WithContext(ctx).Find(&rows).Error; err != nil {
		return result, err
	}
	for _, row := range rows {
		for source, raw := range map[string]*string{"mcp": row.ObservedMCPJSON, "skill": row.ObservedSkillJSON} {
			if raw == nil {
				continue
			}
			var comps []airuntime.ObservedRuntimeComponent
			if err := json.Unmarshal([]byte(*raw), &comps); err != nil {
				return result, fmt.Errorf("malformed %s worker snapshot: %w", source, err)
			}
			for _, comp := range comps {
				if strings.TrimSpace(comp.Name) != "" {
					result[observedKey{source: source, name: comp.Name}] = observedCapability{status: comp.Status, at: row.HeartbeatAt}
				}
			}
		}
	}
	return result, nil
}

func unavailableObservedMeta() v1.ResourceMeta {
	return v1.ResourceMeta{Availability: v1.AvailabilityUnavailable, DataQuality: v1.DataQualityUnknown, ReasonCode: "not_observed"}
}
