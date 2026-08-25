package effects

import (
	"context"
	"encoding/json"
	"fmt"

	"SentinelOps/internal/ai/policy"
	"SentinelOps/internal/ai/workflow"
)

// ReconciliationTarget 是现有 action/Indexer 查询目标状态所需的稳定事实。
type ReconciliationTarget struct {
	EffectKey       string
	EffectStep      string
	EffectRole      string
	EffectType      policy.EffectType
	ToolName        string
	Parameters      map[string]string
	PrimaryResponse string
}

// TargetState 是只读查询或同 idempotency key 重试后得到的证据化目标事实。
type TargetState struct {
	Known             bool
	Applied           bool
	Response          string
	ExternalReference string
	Evidence          any
}

// TargetStateQuery 复用调用方已有的 action Registry 或 Indexer，不持有第二套分发表。
type TargetStateQuery func(context.Context, ReconciliationTarget) (TargetState, error)

// Reconciler 是现有 Effect Executor 的管理/Worker 薄接线；状态真值仍由
// workflow.GORMStore 持有，不创建 Queue、Store 或 Registry。
type Reconciler struct {
	store *workflow.GORMStore
}

func NewReconciler(store *workflow.GORMStore) (*Reconciler, error) {
	if store == nil {
		return nil, workflow.ErrDurablePrimitiveRequired
	}
	return &Reconciler{store: store}, nil
}

func (r *Reconciler) ReapExpired(ctx context.Context, limit int) (int64, error) {
	if r == nil || r.store == nil {
		return 0, workflow.ErrDurablePrimitiveRequired
	}
	return r.store.ReconcileExpiredRunningEffects(ctx, limit)
}

func (r *Reconciler) Claim(ctx context.Context, input workflow.ReconciliationClaimInput) (*workflow.ReconciliationClaim, bool, error) {
	if r == nil || r.store == nil {
		return nil, false, workflow.ErrDurablePrimitiveRequired
	}
	return r.store.ClaimEffectReconciliation(ctx, input)
}

func (r *Reconciler) Resolve(ctx context.Context, input workflow.ResolveEffectInput) error {
	if r == nil || r.store == nil {
		return workflow.ErrDurablePrimitiveRequired
	}
	return r.store.ResolveEffect(ctx, input)
}

// Query 将一次 target-state 查询映射为 fenced resolution；查询失败按 still_unknown 收敛。
func (r *Reconciler) Query(ctx context.Context, claim *workflow.ReconciliationClaim, query TargetStateQuery, resolvedBy string) (workflow.ResolveEffectInput, error) {
	if r == nil || r.store == nil {
		return workflow.ResolveEffectInput{}, workflow.ErrDurablePrimitiveRequired
	}
	if claim == nil || query == nil || resolvedBy == "" || claim.Effect.RequestRedacted == nil {
		return workflow.ResolveEffectInput{}, fmt.Errorf("reconciliation claim, query, actor, and request are required")
	}
	parameters := make(map[string]string)
	queryErr := json.Unmarshal([]byte(*claim.Effect.RequestRedacted), &parameters)
	state := TargetState{}
	if queryErr == nil {
		parentID := ""
		if claim.Effect.ParentEffectID != nil {
			parentID = *claim.Effect.ParentEffectID
		}
		queryCtx := withExecutionMetadata(ctx, ExecutionMetadata{
			EffectKey: claim.Effect.IdempotencyKey, EffectStep: claim.Effect.EffectStep,
			EffectRole: claim.Effect.EffectRole, ParentEffectID: parentID,
			EffectType: policy.EffectType(claim.Effect.EffectType), PrimaryResponse: claim.PrimaryResponse,
		})
		state, queryErr = query(queryCtx, ReconciliationTarget{
			EffectKey: claim.Effect.IdempotencyKey, EffectStep: claim.Effect.EffectStep,
			EffectRole: claim.Effect.EffectRole, EffectType: policy.EffectType(claim.Effect.EffectType),
			ToolName: claim.Effect.ToolName, Parameters: parameters, PrimaryResponse: claim.PrimaryResponse,
		})
	}
	resolution := workflow.EffectResolutionStillUnknown
	if queryErr == nil && state.Known {
		resolution = workflow.EffectResolutionNotExecuted
		if state.Applied {
			resolution = workflow.EffectResolutionExecuted
		}
	}
	evidence := map[string]any{
		"classification": "target_state", "known": state.Known, "applied": state.Applied,
	}
	if state.Evidence != nil {
		evidence["details"] = state.Evidence
	}
	if queryErr != nil {
		evidence["error"] = policy.NewRedactor().RedactText(queryErr.Error())
	}
	encodedEvidence, err := encodeEvidence(evidence)
	if err != nil {
		return workflow.ResolveEffectInput{}, err
	}
	return workflow.ResolveEffectInput{
		Token: claim.Token, EffectID: claim.Effect.ID, ExpectedVersion: claim.Effect.Version,
		Resolution: resolution, ResponseRedacted: state.Response,
		ExternalReference: state.ExternalReference, EvidenceRedacted: encodedEvidence, ResolvedBy: resolvedBy,
	}, nil
}
