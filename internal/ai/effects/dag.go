package effects

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"SentinelOps/internal/ai/policy"
	"SentinelOps/internal/ai/workflow"
)

var (
	// ErrExternalDeadlineUnsafe 表示 lease 安全余量内不能再启动外部调用。
	ErrExternalDeadlineUnsafe = workflow.ErrExternalDeadlineUnsafe
)

// DAGStep 是 Catalog 固定的稳定 Effect 节点，不保存 endpoint 或业务实现。
type DAGStep struct {
	Key       string
	Role      string
	Step      string
	ParentKey string
	Type      policy.EffectType
}

// ExecutionMetadata 是原 Tool endpoint 从 ctx 读取的当前稳定 Effect step。
type ExecutionMetadata struct {
	EffectKey       string
	EffectStep      string
	EffectRole      string
	ParentEffectID  string
	EffectType      policy.EffectType
	PrimaryResponse string
}

type executionMetadataContextKey struct{}

// InvocationClass 明确外部 endpoint 是否可证明未发送、确定失败或结果未知。
type InvocationClass string

const (
	InvocationSucceeded       InvocationClass = "succeeded"
	InvocationSafeNotSent     InvocationClass = "safe_not_sent"
	InvocationDefiniteFailure InvocationClass = "definite_failure"
	InvocationUnknown         InvocationClass = "unknown"
)

// InvocationResult 是外部 endpoint 返回后的 fail-closed 分类。
type InvocationResult struct {
	Class             InvocationClass
	Response          string
	ExternalReference string
	Evidence          any
	Retryable         bool
	Err               error
}

// InvocationError 由现有 action 在掌握发送边界时返回；普通 error 一律归为 unknown。
type InvocationError struct {
	Class             InvocationClass
	Retryable         bool
	Evidence          any
	ExternalReference string
	Err               error
}

func (e *InvocationError) Error() string {
	if e == nil || e.Err == nil {
		return "external Effect invocation failed"
	}
	return e.Err.Error()
}

func (e *InvocationError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// NewInvocationError 创建显式外部发送结果；unknown 和 definite failure 永不可自动重试。
func NewInvocationError(class InvocationClass, retryable bool, evidence any, err error) error {
	if err == nil {
		err = errors.New("external Effect invocation failed")
	}
	if class != InvocationSafeNotSent {
		retryable = false
	}
	return &InvocationError{Class: class, Retryable: retryable, Evidence: evidence, Err: err}
}

// ExecutionMetadataFromContext 读取当前 step；原 action 不按 Tool Name 重新分派。
func ExecutionMetadataFromContext(ctx context.Context) (ExecutionMetadata, error) {
	metadata, ok := ctx.Value(executionMetadataContextKey{}).(ExecutionMetadata)
	if !ok || metadata.EffectKey == "" || metadata.EffectStep == "" || metadata.EffectRole == "" || metadata.EffectType == policy.EffectNone {
		return ExecutionMetadata{}, fmt.Errorf("effect execution metadata is missing")
	}
	return metadata, nil
}

func withExecutionMetadata(ctx context.Context, metadata ExecutionMetadata) context.Context {
	return context.WithValue(ctx, executionMetadataContextKey{}, metadata)
}

func buildDAG(runID, proposalHash string, entry policy.CatalogEntry) ([]DAGStep, error) {
	if len(entry.EffectSteps) == 0 || entry.EffectSteps[0] != workflow.EffectStepPrimary || entry.EffectType == policy.EffectNone {
		return nil, fmt.Errorf("catalog entry %q has no Effect DAG", entry.Name)
	}
	primaryKey, err := policy.EffectKey(runID, proposalHash, workflow.EffectStepPrimary)
	if err != nil {
		return nil, err
	}
	result := []DAGStep{{Key: primaryKey, Role: workflow.EffectRolePrimary, Step: workflow.EffectStepPrimary, Type: entry.EffectType}}
	for _, step := range entry.EffectSteps[1:] {
		key, err := policy.EffectKey(runID, proposalHash, step)
		if err != nil {
			return nil, err
		}
		result = append(result, DAGStep{
			Key: key, Role: workflow.EffectRoleDerived, Step: step, ParentKey: primaryKey,
			Type: policy.EffectReconcilable,
		})
	}
	return result, nil
}

func boundedExternalDeadline(now, attemptDeadline, leaseUntil time.Time, safetyMargin time.Duration) (time.Time, error) {
	return workflow.BoundExternalEffectDeadline(now, attemptDeadline, leaseUntil, safetyMargin)
}

func classifyInvocation(response string, err error) InvocationResult {
	if err == nil {
		result := InvocationResult{Class: InvocationSucceeded, Response: response}
		var output map[string]any
		if json.Unmarshal([]byte(response), &output) == nil {
			if reference, ok := output["external_reference"].(string); ok {
				result.ExternalReference = reference
			}
		}
		return result
	}
	var invocationErr *InvocationError
	if errors.As(err, &invocationErr) {
		class := invocationErr.Class
		if class != InvocationSafeNotSent && class != InvocationDefiniteFailure && class != InvocationUnknown {
			class = InvocationUnknown
		}
		return InvocationResult{
			Class: class, Response: response, ExternalReference: invocationErr.ExternalReference,
			Evidence: invocationErr.Evidence, Retryable: class == InvocationSafeNotSent && invocationErr.Retryable, Err: err,
		}
	}
	return InvocationResult{Class: InvocationUnknown, Response: response, Err: err}
}
