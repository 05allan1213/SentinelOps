// Package effects 把 RuntimeHandler 已授权的 Mutation 交给 workflow 原子 primitive。
// 本包不持有 Tool/Action Registry，也不按 Tool Name 分派。
package effects

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

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
	GateCheck                func(context.Context) (bool, error)
	Deadline                 time.Time
	LeaseSafetyMargin        time.Duration
}

var (
	// ErrEffectGateClosed 表示 frozen 或当前 Gate 已关闭，原 endpoint 未被调用。
	ErrEffectGateClosed = errors.New("durable Effect Gate is closed")
)

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
		return workflow.TransitionEffectResult{}, fmt.Errorf("effect Executor is not initialized")
	}
	input, err := buildTransitionInput(request)
	if err != nil {
		return workflow.TransitionEffectResult{}, err
	}
	if endpoint == nil {
		return workflow.TransitionEffectResult{}, fmt.Errorf("original Tool endpoint callback is required")
	}
	if err := requireEffectGate(ctx, request); err != nil {
		return workflow.TransitionEffectResult{}, err
	}
	effectKey, err := policy.EffectKey(request.Lease.RunID, request.ProposalHash, workflow.EffectStepPrimary)
	if err != nil {
		return workflow.TransitionEffectResult{}, err
	}
	routedEndpoint := func(callbackCtx context.Context) (string, error) {
		return invokeEffectEndpoint(withExecutionMetadata(callbackCtx, ExecutionMetadata{
			EffectKey: effectKey, EffectStep: workflow.EffectStepPrimary, EffectRole: workflow.EffectRolePrimary,
			EffectType: policy.EffectTransactionalDB,
		}), request, endpoint)
	}
	return e.store.TransitionEffectWithEvent(ctx, input, workflow.TransactionalEffectCallback(routedEndpoint))
}

// Execute 按 Catalog DAG 执行同一原 endpoint；derived step 通过 ctx metadata 进入原业务实现。
func (e *Executor) Execute(ctx context.Context, request TransactionalRequest, endpoint Endpoint) (workflow.TransitionEffectResult, error) {
	if e == nil || e.store == nil {
		return workflow.TransitionEffectResult{}, fmt.Errorf("effect Executor is not initialized")
	}
	if endpoint == nil {
		return workflow.TransitionEffectResult{}, fmt.Errorf("original Tool endpoint callback is required")
	}
	if err := requireEffectGate(ctx, request); err != nil {
		return workflow.TransitionEffectResult{}, err
	}
	entry, execution, dag, err := buildExternalExecution(request)
	if err != nil {
		return workflow.TransitionEffectResult{}, err
	}
	var primary workflow.TransitionEffectResult
	if entry.EffectType == policy.EffectTransactionalDB {
		primary, err = e.ExecuteTransactional(ctx, request, endpoint)
		if err != nil {
			return workflow.TransitionEffectResult{}, err
		}
	} else {
		if _, err = e.store.EnsureExternalEffectDAG(ctx, execution); err != nil {
			return workflow.TransitionEffectResult{}, err
		}
	}
	for index, step := range dag {
		if index == 0 && entry.EffectType == policy.EffectTransactionalDB {
			continue
		}
		stepResult, stepErr := e.executeExternalStep(ctx, request, execution, step, primary.Response, endpoint)
		if stepErr != nil {
			return workflow.TransitionEffectResult{}, stepErr
		}
		if index == 0 {
			primary = stepResult
		}
	}
	return primary, nil
}

func (e *Executor) executeExternalStep(
	ctx context.Context,
	request TransactionalRequest,
	execution workflow.ExternalEffectExecutionInput,
	step DAGStep,
	primaryResponse string,
	endpoint Endpoint,
) (workflow.TransitionEffectResult, error) {
	if err := requireEffectGate(ctx, request); err != nil {
		return workflow.TransitionEffectResult{}, err
	}
	margin := request.LeaseSafetyMargin
	if margin <= 0 {
		margin = 5 * time.Second
	}
	started, err := e.store.StartExternalEffect(ctx, workflow.StartExternalEffectInput{
		Execution: execution, EffectStep: step.Step, AttemptDeadline: request.Deadline, LeaseSafetyMargin: margin,
	})
	if err != nil {
		return workflow.TransitionEffectResult{}, err
	}
	if started.Reused {
		return workflow.TransitionEffectResult{Effect: started.Effect, Response: started.Response, Reused: true}, nil
	}
	callCtx, cancel := context.WithDeadline(ctx, started.CallDeadline)
	defer cancel()
	callCtx = withExecutionMetadata(callCtx, ExecutionMetadata{
		EffectKey: step.Key, EffectStep: step.Step, EffectRole: step.Role,
		ParentEffectID: step.ParentKey, EffectType: step.Type, PrimaryResponse: primaryResponse,
	})
	response, endpointErr := invokeEffectEndpoint(callCtx, request, endpoint)
	if endpointErr != nil && errors.Is(endpointErr, ErrEffectGateClosed) {
		endpointErr = NewInvocationError(InvocationSafeNotSent, false, map[string]any{"phase": "gate_recheck"}, endpointErr)
	}
	outcome := classifyInvocation(response, endpointErr)
	evidence, evidenceErr := encodeInvocationEvidence(outcome)
	if evidenceErr != nil {
		outcome = InvocationResult{Class: InvocationUnknown, Err: errors.Join(endpointErr, evidenceErr)}
	}
	finish := workflow.FinishExternalEffectInput{
		Execution: execution, EffectID: started.Effect.ID, ExpectedVersion: started.Effect.Version,
		ResponseRedacted: outcome.Response, ExternalReference: outcome.ExternalReference,
		EvidenceRedacted: evidence,
	}
	if outcome.Err != nil {
		finish.LastErrorRedacted = policy.NewRedactor().RedactText(outcome.Err.Error())
	}
	switch outcome.Class {
	case InvocationSucceeded:
		committed, finishErr := e.store.FinishExternalEffectSucceeded(ctx, finish)
		if finishErr == nil {
			return committed, nil
		}
		finish.LastErrorRedacted = "external endpoint succeeded but ledger commit is unknown"
		finish.EvidenceRedacted, _ = encodeEvidence(map[string]any{
			"classification": string(InvocationUnknown), "retryable": false,
			"endpoint": "succeeded", "ledger_commit": "unknown",
		})
		parkErr := e.store.MarkExternalEffectUnknownAndPark(ctx, finish)
		return workflow.TransitionEffectResult{}, errors.Join(finishErr, parkErr)
	case InvocationSafeNotSent, InvocationDefiniteFailure:
		if err := e.store.FinishExternalEffectFailed(ctx, finish); err != nil {
			return workflow.TransitionEffectResult{}, errors.Join(outcome.Err, err)
		}
		return workflow.TransitionEffectResult{}, outcome.Err
	default:
		if err := e.store.MarkExternalEffectUnknownAndPark(ctx, finish); err != nil {
			return workflow.TransitionEffectResult{}, errors.Join(outcome.Err, err)
		}
		return workflow.TransitionEffectResult{}, outcome.Err
	}
}

func requireEffectGate(ctx context.Context, request TransactionalRequest) error {
	if !request.GateAllowed {
		return ErrEffectGateClosed
	}
	if request.GateCheck == nil {
		return nil
	}
	allowed, err := request.GateCheck(ctx)
	if err != nil {
		return fmt.Errorf("recheck durable Effect Gate: %w", err)
	}
	if !allowed {
		return ErrEffectGateClosed
	}
	return nil
}

func invokeEffectEndpoint(ctx context.Context, request TransactionalRequest, endpoint Endpoint) (string, error) {
	if err := requireEffectGate(ctx, request); err != nil {
		return "", err
	}
	return endpoint(ctx)
}

func callEffectEndpoint(ctx context.Context, request TransactionalRequest, endpoint Endpoint) error {
	_, err := invokeEffectEndpoint(ctx, request, endpoint)
	return err
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

func buildExternalExecution(request TransactionalRequest) (policy.CatalogEntry, workflow.ExternalEffectExecutionInput, []DAGStep, error) {
	entry, err := policy.LookupCatalog(request.ToolName)
	if err != nil {
		return policy.CatalogEntry{}, workflow.ExternalEffectExecutionInput{}, nil, err
	}
	if entry.Risk == policy.RiskL0 || entry.Audience != policy.AudienceDurable || entry.EffectType == policy.EffectNone {
		return policy.CatalogEntry{}, workflow.ExternalEffectExecutionInput{}, nil, fmt.Errorf("tool %q has no durable Effect", request.ToolName)
	}
	if entry.Revision != request.ToolRevision || entry.SchemaHash != request.ToolSchemaHash || !sameSteps(entry.EffectSteps, request.EffectSteps) {
		return policy.CatalogEntry{}, workflow.ExternalEffectExecutionInput{}, nil, workflow.ErrEffectIdentityMismatch
	}
	canonicalArguments, decoded, err := canonicalArgumentsObject(request.ArgumentsJSON)
	if err != nil {
		return policy.CatalogEntry{}, workflow.ExternalEffectExecutionInput{}, nil, err
	}
	redacted, err := policy.NewRedactor().RedactJSON(decoded)
	if err != nil {
		return policy.CatalogEntry{}, workflow.ExternalEffectExecutionInput{}, nil, fmt.Errorf("redact external Effect request: %w", err)
	}
	targetDigest := sha256.Sum256(canonicalArguments)
	dag, err := buildDAG(request.Lease.RunID, request.ProposalHash, entry)
	if err != nil {
		return policy.CatalogEntry{}, workflow.ExternalEffectExecutionInput{}, nil, err
	}
	derived := make([]workflow.DerivedEffectInput, 0, len(dag)-1)
	for _, step := range dag[1:] {
		derived = append(derived, workflow.DerivedEffectInput{Step: step.Step, EffectType: string(step.Type)})
	}
	execution := workflow.ExternalEffectExecutionInput{
		Lease: request.Lease, ApprovalID: request.ApprovalID, ProposalHash: request.ProposalHash,
		ToolCallIDObserved: request.ToolCallIDObserved, ToolName: request.ToolName,
		ToolRevision: request.ToolRevision, ToolSchemaHash: request.ToolSchemaHash,
		TargetHash: hex.EncodeToString(targetDigest[:]), RequestRedacted: string(redacted),
		PolicyHash: request.PolicyHash, RuntimeCompatibilityHash: request.RuntimeCompatibilityHash,
		EffectType: string(entry.EffectType), Attempt: request.Attempt, TraceID: request.TraceID,
		GateAllowed: request.GateAllowed, Derived: derived,
	}
	return entry, execution, dag, nil
}

func encodeEvidence(value any) (string, error) {
	if value == nil {
		return "", nil
	}
	redacted, err := policy.NewRedactor().RedactJSON(value)
	if err != nil {
		return "", err
	}
	canonical, err := policy.CanonicalToolArgumentsJSON(redacted)
	if err != nil {
		return "", fmt.Errorf("encode external Effect evidence: %w", err)
	}
	return string(canonical), nil
}

func encodeInvocationEvidence(outcome InvocationResult) (string, error) {
	evidence := map[string]any{
		"classification": string(outcome.Class),
		"retryable":      outcome.Class == InvocationSafeNotSent && outcome.Retryable,
	}
	if outcome.Evidence != nil {
		evidence["details"] = outcome.Evidence
	}
	return encodeEvidence(evidence)
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
