package workflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"SentinelOps/internal/ai/policy"
	"SentinelOps/internal/dao/mysql"

	"gorm.io/gorm"
)

const OperationIdentityDomain = "sentinelops/runtime-operation/v1"

const (
	OperationActionResume  = "resume"
	OperationActionReplay  = "replay"
	OperationActionCancel  = "cancel"
	OperationActionRestore = "restore"

	OperationStatusAccepted  = "accepted"
	OperationStatusRunning   = "running"
	OperationStatusSucceeded = "succeeded"
	OperationStatusFailed    = "failed"
	OperationStatusCanceled  = "canceled"
	OperationStatusRejected  = "rejected"
)

var (
	ErrInvalidOperationInput      = errors.New("invalid operation input")
	ErrInvalidOperationTransition = errors.New("invalid operation event transition")
	compatibilityHashPattern      = regexp.MustCompile(`^[0-9a-f]{64}$`)
	operationIDPattern            = regexp.MustCompile(`^op-[0-9a-f]{64}$`)
)

// OperationRequestInput 是幂等冲突判断使用的精确请求材料；Reason 必须先校验原文。
type OperationRequestInput struct {
	RunID                     string
	Action                    string
	Reason                    string
	ExpectedGeneration        uint64
	ExpectedCompatibilityHash string
}

// Operation 从关联的 workflow Event 重建，不引入第二持久化模型或命令表。
type Operation struct {
	OperationID          string
	RunID                string
	Action               string
	Status               string
	Terminal             bool
	IdempotencyKeyDigest string
	RequestFingerprint   string
	ActorID              string
	Reason               string
	AcceptedAt           time.Time
	StartedAt            *time.Time
	FinishedAt           *time.Time
	AcceptedSeq          uint64
	StartedSeq           uint64
	CorrelationSeq       uint64
}

// OperationEventInput 是 canonical Operation Event 的类型化构造输入。
type OperationEventInput struct {
	Type                 string
	OperationID          string
	Action               string
	IdempotencyKeyDigest string
	RequestFingerprint   string
	ActorID              string
	Reason               string
	CorrelationSeq       uint64
	TraceID              string
}

// WorkflowEventInput 将 Operation 元数据转换为公共 Event 输入。
// operation ID/action 仅为兼容旧 mapper 镜像到安全 payload；原始 key/reason 不进入正文。
func (input OperationEventInput) WorkflowEventInput() (WorkflowEventInput, error) {
	if !isOperationEventType(input.Type) {
		return WorkflowEventInput{}, fmt.Errorf("%w: unknown operation event %q", ErrInvalidOperationInput, input.Type)
	}
	if strings.TrimSpace(input.OperationID) == "" || strings.TrimSpace(input.Action) == "" {
		return WorkflowEventInput{}, fmt.Errorf("%w: operation_id and action are required", ErrInvalidOperationInput)
	}
	if err := validateOperationMetadata(input.Type, &OperationEventMetadata{OperationID: input.OperationID, CommandAction: input.Action, IdempotencyKeyDigest: input.IdempotencyKeyDigest, RequestFingerprint: input.RequestFingerprint, ActorID: input.ActorID, Reason: input.Reason, CorrelationSeq: input.CorrelationSeq}); err != nil {
		return WorkflowEventInput{}, err
	}
	attrs := map[string]any{"operation_id": input.OperationID, "action": input.Action}
	return WorkflowEventInput{
		Type:    input.Type,
		TraceID: input.TraceID,
		Payload: EventPayload{Attributes: attrs},
		Operation: &OperationEventMetadata{
			OperationID: input.OperationID, CommandAction: input.Action,
			IdempotencyKeyDigest: input.IdempotencyKeyDigest,
			RequestFingerprint:   input.RequestFingerprint, ActorID: input.ActorID,
			Reason: input.Reason, CorrelationSeq: input.CorrelationSeq,
		},
	}, nil
}

func validateOperationMetadata(eventType string, metadata *OperationEventMetadata) error {
	if !isOperationEventType(eventType) {
		if metadata != nil {
			return fmt.Errorf("%w: non-operation event cannot carry operation metadata", ErrInvalidOperationInput)
		}
		return nil
	}
	if metadata == nil {
		return fmt.Errorf("%w: operation event requires metadata", ErrInvalidOperationInput)
	}
	if !operationIDPattern.MatchString(metadata.OperationID) {
		return fmt.Errorf("%w: operation_id must be op-hex64", ErrInvalidOperationInput)
	}
	switch metadata.CommandAction {
	case OperationActionResume, OperationActionReplay, OperationActionCancel, OperationActionRestore:
	default:
		return fmt.Errorf("%w: invalid operation action", ErrInvalidOperationInput)
	}
	for name, value := range map[string]string{"idempotency_key_digest": metadata.IdempotencyKeyDigest, "request_fingerprint": metadata.RequestFingerprint} {
		if value != "" && !compatibilityHashPattern.MatchString(value) {
			return fmt.Errorf("%w: %s must be lowercase hex64", ErrInvalidOperationInput, name)
		}
	}
	if len(metadata.ActorID) > 128 || !utf8.ValidString(metadata.ActorID) {
		return fmt.Errorf("%w: actor_id exceeds 128 bytes", ErrInvalidOperationInput)
	}
	if eventType == EventOperationAccepted {
		if metadata.IdempotencyKeyDigest == "" || metadata.RequestFingerprint == "" || strings.TrimSpace(metadata.ActorID) == "" {
			return fmt.Errorf("%w: accepted metadata is incomplete", ErrInvalidOperationInput)
		}
		if !utf8.ValidString(metadata.Reason) || len(metadata.Reason) < 1 || len(metadata.Reason) > 1000 || strings.TrimSpace(metadata.Reason) == "" {
			return fmt.Errorf("%w: accepted reason length must be 1..1000 bytes", ErrInvalidOperationInput)
		}
		if metadata.CorrelationSeq != 0 {
			return fmt.Errorf("%w: accepted event cannot carry correlation_seq", ErrInvalidOperationInput)
		}
	} else if metadata.IdempotencyKeyDigest != "" || metadata.RequestFingerprint != "" || metadata.ActorID != "" || metadata.Reason != "" {
		return fmt.Errorf("%w: non-accepted event carries acceptance metadata", ErrInvalidOperationInput)
	} else if metadata.CorrelationSeq == 0 {
		return fmt.Errorf("%w: lifecycle event requires correlation_seq", ErrInvalidOperationInput)
	}
	return nil
}

// OperationIdentity 计算稳定 Operation ID 和唯一可持久化的 key 材料。
// 内层摘要以原始 32 字节参与 domain-separated Hash。
func OperationIdentity(runID, idempotencyKey string) (string, string, error) {
	if strings.TrimSpace(runID) == "" || len(runID) > 64 || !utf8.ValidString(runID) {
		return "", "", fmt.Errorf("%w: invalid run_id", ErrInvalidOperationInput)
	}
	if len(idempotencyKey) < 16 || len(idempotencyKey) > 128 || !utf8.ValidString(idempotencyKey) {
		return "", "", fmt.Errorf("%w: idempotency key length must be 16..128 bytes", ErrInvalidOperationInput)
	}
	inner := sha256.Sum256([]byte(idempotencyKey))
	input := append([]byte(OperationIdentityDomain+"\x00"+runID+"\x00"), inner[:]...)
	outer := sha256.Sum256(input)
	return "op-" + hex.EncodeToString(outer[:]), hex.EncodeToString(inner[:]), nil
}

// OperationRequestFingerprint 对五个精确请求字段的 canonical JSON 做 SHA-256；Reason
// 在 Hash 前绝不脱敏。
func OperationRequestFingerprint(input OperationRequestInput) (string, error) {
	if err := validateOperationRequest(input); err != nil {
		return "", err
	}
	canonical, err := policy.CanonicalJSON(struct {
		RunID                     string `json:"run_id"`
		Action                    string `json:"action"`
		Reason                    string `json:"reason"`
		ExpectedGeneration        uint64 `json:"expected_generation"`
		ExpectedCompatibilityHash string `json:"expected_compatibility_hash"`
	}{input.RunID, input.Action, input.Reason, input.ExpectedGeneration, input.ExpectedCompatibilityHash})
	if err != nil {
		return "", fmt.Errorf("canonical operation request: %w", err)
	}
	digest := sha256.Sum256(canonical)
	return hex.EncodeToString(digest[:]), nil
}

func validateOperationRequest(input OperationRequestInput) error {
	if strings.TrimSpace(input.RunID) == "" || len(input.RunID) > 64 || !utf8.ValidString(input.RunID) {
		return fmt.Errorf("%w: invalid run_id", ErrInvalidOperationInput)
	}
	switch input.Action {
	case OperationActionResume, OperationActionReplay, OperationActionCancel, OperationActionRestore:
	default:
		return fmt.Errorf("%w: invalid action", ErrInvalidOperationInput)
	}
	if !utf8.ValidString(input.Reason) || len(input.Reason) < 1 || len(input.Reason) > 1000 || strings.TrimSpace(input.Reason) == "" {
		return fmt.Errorf("%w: reason length must be 1..1000 bytes", ErrInvalidOperationInput)
	}
	if input.Action != OperationActionCancel && input.ExpectedGeneration == 0 {
		return fmt.Errorf("%w: expected_generation must be positive", ErrInvalidOperationInput)
	}
	if input.Action != OperationActionCancel && !compatibilityHashPattern.MatchString(input.ExpectedCompatibilityHash) {
		return fmt.Errorf("%w: expected_compatibility_hash must be lowercase hex64", ErrInvalidOperationInput)
	}
	if input.Action == OperationActionCancel && input.ExpectedCompatibilityHash != "" && !compatibilityHashPattern.MatchString(input.ExpectedCompatibilityHash) {
		return fmt.Errorf("%w: expected_compatibility_hash must be lowercase hex64", ErrInvalidOperationInput)
	}
	return nil
}

func isOperationEventType(eventType string) bool {
	switch eventType {
	case EventOperationAccepted, EventOperationStarted, EventOperationSucceeded,
		EventOperationFailed, EventOperationCanceled, EventOperationRejected:
		return true
	default:
		return false
	}
}

// DeriveOperation 校验并按 seq 折叠 Operation Event 为单一状态。
func DeriveOperation(events []mysql.WorkflowEvent) (Operation, error) {
	if len(events) == 0 {
		return Operation{}, fmt.Errorf("%w: no events", ErrInvalidOperationTransition)
	}
	ordered := append([]mysql.WorkflowEvent(nil), events...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Seq < ordered[j].Seq })
	for index := 1; index < len(ordered); index++ {
		if ordered[index-1].Seq == ordered[index].Seq {
			return Operation{}, fmt.Errorf("%w: duplicate seq %d", ErrInvalidOperationTransition, ordered[index].Seq)
		}
	}
	var operation Operation
	for index, event := range ordered {
		if !isOperationEventType(event.EventType) || event.OperationID == nil || strings.TrimSpace(*event.OperationID) == "" || event.CommandAction == nil || strings.TrimSpace(*event.CommandAction) == "" {
			return Operation{}, fmt.Errorf("%w: malformed event at seq %d", ErrInvalidOperationTransition, event.Seq)
		}
		if index == 0 {
			operation.OperationID, operation.RunID, operation.Action = *event.OperationID, event.RunID, *event.CommandAction
		} else if *event.OperationID != operation.OperationID || event.RunID != operation.RunID || *event.CommandAction != operation.Action {
			return Operation{}, fmt.Errorf("%w: operation identity changed at seq %d", ErrInvalidOperationTransition, event.Seq)
		}
		metadata := &OperationEventMetadata{OperationID: *event.OperationID, CommandAction: *event.CommandAction}
		if event.IdempotencyKeyDigest != nil {
			metadata.IdempotencyKeyDigest = *event.IdempotencyKeyDigest
		}
		if event.RequestFingerprint != nil {
			metadata.RequestFingerprint = *event.RequestFingerprint
		}
		if event.ActorID != nil {
			metadata.ActorID = *event.ActorID
		}
		if event.ReasonRedacted != nil {
			metadata.Reason = *event.ReasonRedacted
		}
		if event.CorrelationSeq != nil {
			metadata.CorrelationSeq = *event.CorrelationSeq
		}
		if err := validateOperationMetadata(event.EventType, metadata); err != nil {
			return Operation{}, fmt.Errorf("%w at seq %d: %v", ErrInvalidOperationTransition, event.Seq, err)
		}
		if event.EventType != EventOperationAccepted && metadata.CorrelationSeq >= event.Seq {
			return Operation{}, fmt.Errorf("%w: correlation_seq %d must reference an earlier Run Event before seq %d", ErrInvalidOperationTransition, metadata.CorrelationSeq, event.Seq)
		}
		switch event.EventType {
		case EventOperationAccepted:
			if index != 0 {
				return Operation{}, fmt.Errorf("%w: accepted must be first", ErrInvalidOperationTransition)
			}
			operation.Status, operation.AcceptedSeq = OperationStatusAccepted, event.Seq
			if event.IdempotencyKeyDigest != nil {
				operation.IdempotencyKeyDigest = *event.IdempotencyKeyDigest
			}
			if event.RequestFingerprint != nil {
				operation.RequestFingerprint = *event.RequestFingerprint
			}
			if event.ActorID != nil {
				operation.ActorID = *event.ActorID
			}
			if event.ReasonRedacted != nil {
				operation.Reason = *event.ReasonRedacted
			}
			operation.AcceptedAt = event.CreatedAt
		case EventOperationStarted:
			if index == 0 || operation.Status != OperationStatusAccepted {
				return Operation{}, fmt.Errorf("%w: started requires accepted", ErrInvalidOperationTransition)
			}
			operation.Status, operation.StartedSeq = OperationStatusRunning, event.Seq
			t := event.CreatedAt
			operation.StartedAt = &t
		case EventOperationSucceeded, EventOperationFailed, EventOperationCanceled, EventOperationRejected:
			if index == 0 || (operation.Status != OperationStatusAccepted && operation.Status != OperationStatusRunning) || (event.EventType != EventOperationRejected && operation.Status != OperationStatusRunning) {
				return Operation{}, fmt.Errorf("%w: terminal event requires started", ErrInvalidOperationTransition)
			}
			switch event.EventType {
			case EventOperationSucceeded:
				operation.Status = OperationStatusSucceeded
			case EventOperationFailed:
				operation.Status = OperationStatusFailed
			case EventOperationCanceled:
				operation.Status = OperationStatusCanceled
			case EventOperationRejected:
				operation.Status = OperationStatusRejected
			}
			operation.Terminal = true
			t := event.CreatedAt
			operation.FinishedAt = &t
		}
		if operation.Terminal && index < len(ordered)-1 {
			return Operation{}, fmt.Errorf("%w: event after terminal", ErrInvalidOperationTransition)
		}
		if event.CorrelationSeq != nil {
			operation.CorrelationSeq = *event.CorrelationSeq
		}
	}
	if operation.Status == "" {
		return Operation{}, fmt.Errorf("%w: empty status", ErrInvalidOperationTransition)
	}
	return operation, nil
}

func (s *GORMStore) LoadOperation(ctx context.Context, operationID string) (Operation, error) {
	if strings.TrimSpace(operationID) == "" {
		return Operation{}, fmt.Errorf("%w: empty operation_id", ErrInvalidOperationInput)
	}
	var rows []mysql.WorkflowEvent
	if err := s.db.WithContext(ctx).Where("operation_id = ?", operationID).Order("seq ASC").Find(&rows).Error; err != nil {
		return Operation{}, fmt.Errorf("load operation events: %w", err)
	}
	if len(rows) == 0 {
		return Operation{}, gorm.ErrRecordNotFound
	}
	if err := s.authorizeRunScope(ctx, rows[0].RunID); err != nil {
		return Operation{}, err
	}
	return DeriveOperation(rows)
}

func (s *GORMStore) ListActiveOperationsForRun(ctx context.Context, runID string) ([]Operation, error) {
	if strings.TrimSpace(runID) == "" {
		return nil, fmt.Errorf("%w: empty run_id", ErrInvalidOperationInput)
	}
	if err := s.authorizeRunScope(ctx, runID); err != nil {
		return nil, err
	}
	var rows []mysql.WorkflowEvent
	if err := s.db.WithContext(ctx).Where("run_id = ? AND operation_id IS NOT NULL", runID).Order("operation_id ASC, seq ASC").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list operation events: %w", err)
	}
	grouped := make(map[string][]mysql.WorkflowEvent)
	order := make([]string, 0)
	for _, row := range rows {
		if row.OperationID == nil {
			continue
		}
		if _, ok := grouped[*row.OperationID]; !ok {
			order = append(order, *row.OperationID)
		}
		grouped[*row.OperationID] = append(grouped[*row.OperationID], row)
	}
	result := make([]Operation, 0, len(order))
	for _, id := range order {
		operation, err := DeriveOperation(grouped[id])
		if err != nil {
			return nil, err
		}
		if !operation.Terminal {
			result = append(result, operation)
		}
	}
	return result, nil
}
