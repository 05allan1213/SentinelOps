package runtime

import (
	"context"
	"math"
	"sort"
	"strings"

	v1 "SentinelOps/api/runtime/v1"
	airuntime "SentinelOps/internal/ai/runtime"
	"SentinelOps/internal/dao/mysql"
	"SentinelOps/internal/service/rageval"
)

const (
	// EvalSuiteAgent is the Runtime Eval namespace. It is intentionally not
	// backed by the legacy RAG trace dashboard.
	EvalSuiteAgent = "agent_eval"
	// EvalSuiteRAG is the legacy retrieval-quality namespace.
	EvalSuiteRAG = "rag_eval"
)

// EvalFilter is the read-only filter for the frozen Runtime Eval route. Page
// values are retained at this boundary for the public contract; the currently
// available optional source is a single aggregate dashboard.
type EvalFilter struct {
	Suite    string
	Page     int
	PageSize int
}

// GetEval returns either the explicitly requested RAG diagnostic aggregate or
// an honest Agent Eval not-run result. There is no versioned Agent Eval source
// in this repository, so durable Runtime traces must never be promoted to an
// Agent Eval pass by this method.
func (s *RuntimeService) GetEval(ctx context.Context, filter EvalFilter) (v1.EvalRes, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	suite := normalizeEvalSuite(filter.Suite)
	if suite == EvalSuiteRAG {
		metrics, err := rageval.GetDashboard(ctx, "24h")
		if err != nil {
			meta := ragEvalUnavailableMeta()
			item := v1.EvalDTO{Suite: EvalSuiteRAG, ResourceMeta: meta}
			return v1.EvalRes{Item: item, ResourceMeta: meta}, nil
		}
		item := mapRAGEvalMetrics(metrics)
		return v1.EvalRes{Item: item, ResourceMeta: item.ResourceMeta}, nil
	}

	meta := agentEvalNotRunMeta()
	item := v1.EvalDTO{Suite: EvalSuiteAgent, ResourceMeta: meta}
	return v1.EvalRes{Item: item, ResourceMeta: meta}, nil
}

// normalizeEvalSuite keeps the only two meaningful namespaces explicit. An
// unknown suite remains an Agent Eval request and therefore cannot receive a
// RAG-backed result by accident.
func normalizeEvalSuite(suite string) string {
	switch strings.ToLower(strings.TrimSpace(suite)) {
	case EvalSuiteRAG, "rag", "rag-eval":
		return EvalSuiteRAG
	default:
		return EvalSuiteAgent
	}
}

func agentEvalNotRunMeta() v1.ResourceMeta {
	return v1.ResourceMeta{
		Availability: v1.AvailabilityUnavailable,
		DataQuality:  v1.DataQualityUnknown,
		ReasonCode:   "eval_not_executed",
		NotRun:       true,
	}
}

func ragEvalUnavailableMeta() v1.ResourceMeta {
	return v1.ResourceMeta{
		Availability: v1.AvailabilityUnavailable,
		DataQuality:  v1.DataQualityUnknown,
		ReasonCode:   "rag_eval_unavailable",
		NotRun:       true,
	}
}

// mapRAGEvalMetrics deliberately leaves Agent Runtime-only fields unset. The
// success/failure counts describe the existing RAG dashboard aggregate and
// deterministic/LLM results are explicitly not applicable to that source.
func mapRAGEvalMetrics(metrics *rageval.DashboardMetrics) v1.EvalDTO {
	meta := v1.ResourceMeta{Availability: v1.AvailabilityAvailable, DataQuality: v1.DataQualityComplete}
	item := v1.EvalDTO{
		Suite:                   EvalSuiteRAG,
		DeterministicGateResult: "not_applicable",
		LLMJudgeResult:          "not_applicable",
		ResourceMeta:            meta,
	}
	if metrics == nil {
		item.ResourceMeta = ragEvalUnavailableMeta()
		return item
	}

	total := metrics.TotalRuns
	if total < 0 || total > int64(maxInt()) || math.IsNaN(metrics.SuccessRate) || math.IsInf(metrics.SuccessRate, 0) || metrics.SuccessRate < 0 || metrics.SuccessRate > 1 {
		item.ResourceMeta = v1.ResourceMeta{Availability: v1.AvailabilityPartial, DataQuality: v1.DataQualityUnknown, ReasonCode: "malformed_rag_eval"}
		return item
	}
	passed := int(math.Round(metrics.SuccessRate * float64(total)))
	if passed < 0 {
		passed = 0
	}
	if passed > int(total) {
		passed = int(total)
	}
	item.CaseCount = int(total)
	item.Passed = passed
	item.Failed = int(total) - passed
	return item
}

func maxInt() int {
	return int(^uint(0) >> 1)
}

// GetRelease projects only current Runtime identity, current effective Gates,
// and persisted Worker observations. No rollout controller or mutable release
// operation is exposed here.
func (s *RuntimeService) GetRelease(ctx context.Context) (v1.ReleaseRes, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	item := v1.ReleaseDTO{RuntimeVersion: airuntime.CurrentRuntimeVersion()}

	if s != nil && s.Gates != nil {
		state, err := s.Gates.CurrentState(ctx)
		if err != nil {
			return v1.ReleaseRes{}, err
		}
		item.GateVector = state.CurrentEffective.Map()
	}
	if s != nil && s.Store != nil && s.Store.DB() != nil {
		rows, err := mysql.NewGORMStore(s.Store.DB()).ListRuntimeWorkerSnapshots(ctx)
		if err != nil {
			return v1.ReleaseRes{}, err
		}
		item.ObservedWorkerVersions = observedWorkerVersions(rows)
	}

	// There is no durable rollout/gray/rollback source in the current schema.
	// Keep both nullable fields empty and independently communicate that no
	// release execution was observed through the frozen top-level metadata.
	meta := v1.ResourceMeta{
		Availability: v1.AvailabilityUnavailable,
		DataQuality:  v1.DataQualityUnknown,
		ReasonCode:   "not_observed",
		NotRun:       true,
	}
	item.ResourceMeta = meta
	return v1.ReleaseRes{Item: item, ResourceMeta: meta}, nil
}

func observedWorkerVersions(rows []mysql.RuntimeWorkerSnapshot) []string {
	versions := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		if row.RuntimeVersion == nil {
			continue
		}
		version := strings.TrimSpace(*row.RuntimeVersion)
		if version != "" {
			versions[version] = struct{}{}
		}
	}
	result := make([]string, 0, len(versions))
	for version := range versions {
		result = append(result, version)
	}
	sort.Strings(result)
	return result
}
