// Package effects 把 RuntimeHandler 已授权的 Mutation 交给 workflow 原子 primitive。
// 本包不持有 Tool/Action Registry，不按 Tool Name 分派，也不执行外部副作用。
package effects

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"SentinelOps/internal/ai/policy"
	"SentinelOps/internal/ai/workflow"
)

// Executor 是 RuntimeHandler 与唯一 workflow.GORMStore 之间的薄 Effect 接线。
type Executor struct {
	store *workflow.GORMStore
}

// TransactionalRequest 保存 Handler 从 exact Approval Resume 重建的冻结事实。
type TransactionalRequest struct {
	Lease                    workflow.LeaseToken
	ApprovalID               string
	ProposalHash             string
	ToolCallIDObserved       string
	ToolName                 string
	ToolRevision             string
	ToolSchemaHash           string
	ArgumentsJSON            string
	PolicyHash               string
	RuntimeCompatibilityHash string
	EffectSteps              []string
	Attempt                  uint
	TraceID                  string
	GateAllowed              bool
}

// Endpoint 是 RuntimeHandler 传入的原 Eino Tool endpoint callback。
type Endpoint func(context.Context) (string, error)

// NewExecutor 原位复用 GORMStore 创建无 Registry 的薄 Executor。
func NewExecutor(store *workflow.GORMStore) (*Executor, error) {
	if store == nil {
		return nil, fmt.Errorf("workflow GORMStore is required for Effect Executor")
	}
	return &Executor{store: store}, nil
}

// ExecuteTransactional 只允许 Catalog 已登记的 transactional_db Primary。
func (e *Executor) ExecuteTransactional(ctx context.Context, request TransactionalRequest, endpoint Endpoint) (workflow.TransitionEffectResult, error) {
	if e == nil || e.store == nil {
		return workflow.TransitionEffectResult{}, fmt.Errorf("Effect Executor is not initialized")
	}
	input, err := buildTransitionInput(request)
	if err != nil {
		return workflow.TransitionEffectResult{}, err
	}
	if endpoint == nil {
		return workflow.TransitionEffectResult{}, fmt.Errorf("original Tool endpoint callback is required")
	}
	return e.store.TransitionEffectWithEvent(ctx, input, workflow.TransactionalEffectCallback(endpoint))
}

func buildTransitionInput(request TransactionalRequest) (workflow.TransitionEffectInput, error) {
	entry, err := policy.LookupCatalog(request.ToolName)
	if err != nil {
		return workflow.TransitionEffectInput{}, err
	}
	if entry.EffectType != policy.EffectTransactionalDB || entry.Risk != policy.RiskL1 || entry.Audience != policy.AudienceDurable {
		return workflow.TransitionEffectInput{}, fmt.Errorf("tool %q is not a P23 transactional_db mutation", request.ToolName)
	}
	if entry.Revision != request.ToolRevision || entry.SchemaHash != request.ToolSchemaHash || !sameSteps(entry.EffectSteps, request.EffectSteps) {
		return workflow.TransitionEffectInput{}, workflow.ErrEffectIdentityMismatch
	}
	canonicalArguments, decoded, err := canonicalArgumentsObject(request.ArgumentsJSON)
	if err != nil {
		return workflow.TransitionEffectInput{}, err
	}
	redacted, err := policy.NewRedactor().RedactJSON(decoded)
	if err != nil {
		return workflow.TransitionEffectInput{}, fmt.Errorf("redact transactional Effect request: %w", err)
	}
	targetDigest := sha256.Sum256(canonicalArguments)
	derived := make([]workflow.DerivedEffectInput, 0, len(entry.EffectSteps)-1)
	for _, step := range entry.EffectSteps[1:] {
		// P23 唯一 transactional_db 后继是可核对的 MySQL→Milvus 索引；
		// 执行、deadline 与 reconciliation 仍留在 P24/P25。
		derived = append(derived, workflow.DerivedEffectInput{Step: step, EffectType: string(policy.EffectReconcilable)})
	}
	return workflow.TransitionEffectInput{
		Lease: request.Lease, ApprovalID: request.ApprovalID, ProposalHash: request.ProposalHash,
		ToolCallIDObserved: request.ToolCallIDObserved, ToolName: request.ToolName,
		ToolRevision: request.ToolRevision, ToolSchemaHash: request.ToolSchemaHash,
		TargetHash: hex.EncodeToString(targetDigest[:]), RequestRedacted: string(redacted),
		PolicyHash: request.PolicyHash, RuntimeCompatibilityHash: request.RuntimeCompatibilityHash,
		EffectType: string(policy.EffectTransactionalDB), Attempt: request.Attempt,
		TraceID: request.TraceID, GateAllowed: request.GateAllowed, Derived: derived,
	}, nil
}

func canonicalArgumentsObject(raw string) ([]byte, any, error) {
	canonical, err := policy.CanonicalToolArgumentsJSON([]byte(raw))
	if err != nil {
		return nil, nil, fmt.Errorf("decode transactional Effect arguments: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(canonical))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, nil, fmt.Errorf("decode transactional Effect arguments: %w", err)
	}
	return canonical, value, nil
}

func sameSteps(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
