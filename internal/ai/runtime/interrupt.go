package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"SentinelOps/internal/ai/effects"
	"SentinelOps/internal/ai/policy"
	"SentinelOps/internal/ai/workflow"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

const approvalTTL = 30 * time.Minute

var (
	// ErrTransactionalEffectEndpointUnsupported 表示 Mutation Tool 没有使用现有同步 Invokable endpoint。
	ErrTransactionalEffectEndpointUnsupported = errors.New("transactional Effect requires an invokable Tool endpoint")
)

type approvedTransactionalCall struct {
	Request effects.TransactionalRequest
}

// ApprovalInterruptInfo 是 StatefulInterrupt 对外可见且不含原参数的最小说明。
type ApprovalInterruptInfo struct {
	ApprovalID   string           `json:"approval_id"`
	ProposalHash string           `json:"proposal_hash"`
	ToolName     string           `json:"tool_name"`
	RiskLevel    policy.RiskLevel `json:"risk_level"`
}

// ApprovalInterruptState 保存 Handler 恢复所需的规范 Proposal 与原始 SecretRef 参数。
// 字段均为导出值，供 Eino 官方 Checkpoint gob 序列化；项目不读取 opaque blob。
type ApprovalInterruptState struct {
	ApprovalID               string
	ProposalHash             string
	ProposalCanonicalJSON    string
	ArgumentsJSON            string
	ToolName                 string
	ToolRevision             string
	ToolSchemaHash           string
	RiskLevel                policy.RiskLevel
	PolicyHash               string
	RuntimeCompatibilityHash string
	EffectSteps              []string
}

// ApprovalResumeData 只把数据库决定送回中断点；Handler 不把它当授权真值。
type ApprovalResumeData struct {
	ApprovalID string `json:"approval_id"`
	Decision   string `json:"decision"`
}

func init() {
	schema.Register[ApprovalInterruptInfo]()
	schema.Register[ApprovalInterruptState]()
}

func (h *RuntimeHandler) handleApprovalToolCall(
	ctx context.Context,
	toolContext *adk.ToolContext,
	rawArguments string,
) (*approvedTransactionalCall, bool, error) {
	if toolContext == nil {
		return nil, false, nil
	}
	entry, err := policy.LookupCatalog(toolContext.Name)
	if err != nil || entry.Risk == policy.RiskL0 {
		return nil, false, nil
	}
	if h == nil || h.approvalStore == nil || h.effects == nil {
		return nil, true, mutationDisabledError(entry)
	}
	attempt, _, err := validateRuntimeCallContext(ctx)
	if err != nil {
		return nil, true, err
	}
	if toolContext.CallID == "" {
		return nil, true, fmt.Errorf("mutation Tool call ID is required")
	}
	if err := validateToolSnapshot(attempt.Snapshot, toolContext.Name); err != nil {
		return nil, true, err
	}
	wasInterrupted, hasState, restored := tool.GetInterruptState[ApprovalInterruptState](ctx)
	isTarget, _, _ := tool.GetResumeContext[ApprovalResumeData](ctx)
	if wasInterrupted && isTarget {
		if !hasState || restored.ToolName != entry.Name || restored.ToolRevision != entry.Revision ||
			restored.ToolSchemaHash != entry.SchemaHash || restored.RiskLevel != entry.Risk ||
			restored.PolicyHash != attempt.Snapshot.PolicyHash() || restored.RuntimeCompatibilityHash != attempt.Run.RuntimeCompatibilityHash ||
			!sameEffectSteps(restored.EffectSteps, entry.EffectSteps) {
			return nil, true, workflow.ErrApprovalInvalidated
		}
		canonicalArguments, err := policy.CanonicalToolArgumentsJSON([]byte(restored.ArgumentsJSON))
		if err != nil || string(canonicalArguments) != restored.ArgumentsJSON {
			return nil, true, workflow.ErrApprovalInvalidated
		}
		gateName := "agent_runtime.l1_writes"
		if entry.Risk == policy.RiskL2 {
			gateName = "agent_runtime.l2_writes"
		}
		_, err = h.approvalStore.AuthorizeApprovalResume(ctx, workflow.AuthorizeApprovalResumeInput{
			Lease: attempt.Lease, ApprovalID: restored.ApprovalID, ProposalHash: restored.ProposalHash,
			ToolName: restored.ToolName, ToolRevision: restored.ToolRevision, ToolSchemaHash: restored.ToolSchemaHash,
			PolicyHash: restored.PolicyHash, RuntimeCompatibilityHash: restored.RuntimeCompatibilityHash,
			ExplicitTarget: true, GateAllowed: attempt.Snapshot.FeatureGate(gateName),
		})
		if err != nil {
			return nil, true, err
		}
		if entry.EffectType != policy.EffectTransactionalDB {
			return nil, true, mutationDisabledError(entry)
		}
		return &approvedTransactionalCall{Request: effects.TransactionalRequest{
			Lease: attempt.Lease, ApprovalID: restored.ApprovalID, ProposalHash: restored.ProposalHash,
			ToolCallIDObserved: toolContext.CallID, ToolName: restored.ToolName,
			ToolRevision: restored.ToolRevision, ToolSchemaHash: restored.ToolSchemaHash,
			ArgumentsJSON: restored.ArgumentsJSON, PolicyHash: restored.PolicyHash,
			RuntimeCompatibilityHash: restored.RuntimeCompatibilityHash,
			EffectSteps:              append([]string(nil), restored.EffectSteps...), Attempt: attempt.Run.Attempt,
			TraceID: attempt.Trace.ID, GateAllowed: attempt.Snapshot.FeatureGate(gateName),
		}}, true, nil
	}
	arguments, canonicalArguments, err := decodeMutationArguments(rawArguments)
	if err != nil {
		return nil, true, err
	}
	proposal := policy.Proposal{
		ToolName: entry.Name, ToolRevision: entry.Revision, ToolSchemaHash: entry.SchemaHash,
		RiskLevel: entry.Risk, ArgumentsWithSecretRefs: arguments,
		TargetScope: map[string]any{"user_id": attempt.Scope.UserID, "all": attempt.Scope.All},
		PolicyHash:  attempt.Snapshot.PolicyHash(), RuntimeCompatibilityHash: attempt.Run.RuntimeCompatibilityHash,
	}
	frozen, err := policy.FreezeProposal(proposal)
	if err != nil {
		return nil, true, err
	}
	approvalID, err := policy.ApprovalID(attempt.Run.ID, frozen.Hash())
	if err != nil {
		return nil, true, err
	}
	state := ApprovalInterruptState{
		ApprovalID: approvalID, ProposalHash: frozen.Hash(), ProposalCanonicalJSON: string(frozen.CanonicalJSON()),
		ArgumentsJSON: string(canonicalArguments), ToolName: entry.Name, ToolRevision: entry.Revision,
		ToolSchemaHash: entry.SchemaHash, RiskLevel: entry.Risk, PolicyHash: attempt.Snapshot.PolicyHash(),
		RuntimeCompatibilityHash: attempt.Run.RuntimeCompatibilityHash, EffectSteps: append([]string(nil), entry.EffectSteps...),
	}
	info := ApprovalInterruptInfo{ApprovalID: approvalID, ProposalHash: frozen.Hash(), ToolName: entry.Name, RiskLevel: entry.Risk}

	if wasInterrupted {
		if !hasState || !sameApprovalInterruptState(restored, state) {
			return nil, true, workflow.ErrApprovalInvalidated
		}
		status, err := h.prepareInterruptedApproval(ctx, attempt, toolContext, entry, frozen, arguments)
		if err != nil {
			return nil, true, err
		}
		if err := approvalReinterruptError(status); err != nil {
			return nil, true, err
		}
		return nil, true, tool.StatefulInterrupt(ctx, info, state)
	}

	status, err := h.prepareInterruptedApproval(ctx, attempt, toolContext, entry, frozen, arguments)
	if err != nil {
		return nil, true, err
	}
	if err := approvalReinterruptError(status); err != nil {
		return nil, true, err
	}
	return nil, true, tool.StatefulInterrupt(ctx, info, state)
}

func (h *RuntimeHandler) prepareInterruptedApproval(
	ctx context.Context,
	attempt *AttemptContext,
	toolContext *adk.ToolContext,
	entry policy.CatalogEntry,
	frozen policy.FrozenProposal,
	arguments any,
) (string, error) {
	redacted, err := policy.NewRedactor().RedactJSON(arguments)
	if err != nil {
		return "", fmt.Errorf("redact Approval Proposal: %w", err)
	}
	approval, err := h.approvalStore.PrepareApproval(ctx, workflow.PrepareApprovalInput{
		Lease: attempt.Lease, ToolCallIDObserved: toolContext.CallID, ToolName: entry.Name,
		ToolRevision: entry.Revision, ToolSchemaHash: entry.SchemaHash, RiskLevel: entry.Risk,
		ProposalJSONRedacted: string(redacted), ProposalHash: frozen.Hash(), PolicyHash: attempt.Snapshot.PolicyHash(),
		RuntimeCompatibilityHash: attempt.Run.RuntimeCompatibilityHash, ExpiresAt: time.Now().Add(approvalTTL),
	})
	if err != nil {
		return "", err
	}
	return approval.Status, nil
}

func approvalReinterruptError(status string) error {
	switch status {
	case workflow.ApprovalStatusPreparing:
		return nil
	case workflow.ApprovalStatusRejected:
		return workflow.ErrApprovalRejected
	case workflow.ApprovalStatusExpired:
		return workflow.ErrApprovalExpired
	case workflow.ApprovalStatusInvalidated:
		return workflow.ErrApprovalInvalidated
	default:
		return workflow.ErrApprovalResumeTargetRequired
	}
}

func decodeMutationArguments(raw string) (any, []byte, error) {
	decoder := json.NewDecoder(bytes.NewBufferString(raw))
	decoder.UseNumber()
	var arguments any
	if err := decoder.Decode(&arguments); err != nil {
		return nil, nil, fmt.Errorf("decode Mutation Tool arguments: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, nil, fmt.Errorf("decode Mutation Tool arguments: trailing JSON value")
	}
	if _, ok := arguments.(map[string]any); !ok {
		return nil, nil, fmt.Errorf("mutation Tool arguments must be a JSON object")
	}
	canonical, err := policy.CanonicalJSON(arguments)
	if err != nil {
		return nil, nil, err
	}
	return arguments, canonical, nil
}

func sameApprovalInterruptState(left, right ApprovalInterruptState) bool {
	if left.ApprovalID != right.ApprovalID || left.ProposalHash != right.ProposalHash ||
		left.ProposalCanonicalJSON != right.ProposalCanonicalJSON || left.ArgumentsJSON != right.ArgumentsJSON ||
		left.ToolName != right.ToolName || left.ToolRevision != right.ToolRevision || left.ToolSchemaHash != right.ToolSchemaHash ||
		left.RiskLevel != right.RiskLevel || left.PolicyHash != right.PolicyHash ||
		left.RuntimeCompatibilityHash != right.RuntimeCompatibilityHash || len(left.EffectSteps) != len(right.EffectSteps) {
		return false
	}
	for index := range left.EffectSteps {
		if left.EffectSteps[index] != right.EffectSteps[index] {
			return false
		}
	}
	return true
}

func sameEffectSteps(left, right []string) bool {
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

func mutationDisabledError(entry policy.CatalogEntry) error {
	return &policy.ToolPolicyError{Code: policy.PolicyMutationDisabled, ToolName: entry.Name, Risk: entry.Risk}
}
