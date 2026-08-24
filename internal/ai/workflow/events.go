package workflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"unicode/utf8"

	"SentinelOps/internal/ai/policy"
)

const (
	// EventPayloadVersion 是 workflow_events 当前唯一支持的 payload envelope 版本。
	EventPayloadVersion uint = 1
	// EventEnvelopeSchema 标识 Event payload 的稳定结构版本。
	EventEnvelopeSchema = "sentinelops/workflow-event/v1"
	// MaxEventTextBytes 限制 Event 内单个文本字段，正文必须改存摘要、引用或 Trace。
	MaxEventTextBytes    = 4096
	maxEventPayloadBytes = 16 * 1024
)

const (
	EventRunCreated          = "run.created"
	EventRunClaimed          = "run.claimed"
	EventRunResumed          = "run.resumed"
	EventRunReplayed         = "run.replayed"
	EventRunReconciling      = "run.reconciling"
	EventRunCompleted        = "run.completed"
	EventRunFailed           = "run.failed"
	EventRunParked           = "run.parked"
	EventAgentPlan           = "agent.plan"
	EventAgentReplan         = "agent.replan"
	EventAgentToolCall       = "agent.tool_call"
	EventAgentToolResult     = "agent.tool_result"
	EventAgentInterrupted    = "agent.interrupted"
	EventApprovalPreparing   = "approval.preparing"
	EventApprovalRequested   = "approval.requested"
	EventApprovalDecided     = "approval.decided"
	EventApprovalExpired     = "approval.expired"
	EventApprovalInvalidated = "approval.invalidated"
	EventEffectStarted       = "effect.started"
	EventEffectSucceeded     = "effect.succeeded"
	EventEffectFailed        = "effect.failed"
	EventEffectUnknown       = "effect.unknown"
	EventEffectReconciling   = "effect.reconciling"
	EventEffectResolved      = "effect.resolved"
	EventBudgetReserved      = "budget.reserved"
	EventBudgetSettled       = "budget.settled"
	EventBudgetExhausted     = "budget.exhausted"
	EventBudgetUsageUnknown  = "budget.usage_unknown"
	EventEvidenceRetrieved   = "evidence.retrieved"
	EventEvidenceCited       = "evidence.cited"
	EventTraceFlushed        = "trace.flushed"
	EventTraceIncomplete     = "trace.incomplete"
)

var eventCatalog = []string{
	EventRunCreated, EventRunClaimed, EventRunResumed, EventRunReplayed,
	EventRunReconciling, EventRunCompleted, EventRunFailed, EventRunParked,
	EventAgentPlan, EventAgentReplan, EventAgentToolCall, EventAgentToolResult, EventAgentInterrupted,
	EventApprovalPreparing, EventApprovalRequested, EventApprovalDecided, EventApprovalExpired, EventApprovalInvalidated,
	EventEffectStarted, EventEffectSucceeded, EventEffectFailed, EventEffectUnknown, EventEffectReconciling, EventEffectResolved,
	EventBudgetReserved, EventBudgetSettled, EventBudgetExhausted, EventBudgetUsageUnknown,
	EventEvidenceRetrieved, EventEvidenceCited, EventTraceFlushed, EventTraceIncomplete,
}

var (
	// ErrUnknownEventType 表示调用方试图写入 catalog 之外的 durable Event。
	ErrUnknownEventType = errors.New("unknown workflow event type")
	// ErrEventPayloadTooLarge 表示 Event 携带了应改存摘要、引用或 Trace 的正文。
	ErrEventPayloadTooLarge = errors.New("workflow event payload is too large")
)

// EventPayload 是 durable Event 允许持久化的有界结构化内容。
type EventPayload struct {
	Summary    string         `json:"summary,omitempty"`
	Reference  string         `json:"reference,omitempty"`
	Attributes map[string]any `json:"-"`
}

// WorkflowEventInput 是 Store primitive 写入 Event 所需的调用方输入。
type WorkflowEventInput struct {
	Type    string
	Payload EventPayload
	TraceID string
}

type eventEnvelope struct {
	Schema    string         `json:"schema"`
	Summary   string         `json:"summary,omitempty"`
	Reference string         `json:"reference,omitempty"`
	Data      map[string]any `json:"data,omitempty"`
}

// VersionedEventCatalog 返回只读副本，后续单元只能消费这些 canonical name。
func VersionedEventCatalog() []string {
	result := make([]string, len(eventCatalog))
	copy(result, eventCatalog)
	return result
}

func marshalDurableEvent(input WorkflowEventInput) (string, error) {
	if !isCatalogEvent(input.Type) {
		return "", fmt.Errorf("%w: %q", ErrUnknownEventType, input.Type)
	}
	if len(input.TraceID) > 64 {
		return "", fmt.Errorf("event trace id exceeds 64 bytes")
	}
	redactor := policy.NewRedactor()
	if redactor.RedactText(input.TraceID) != input.TraceID {
		return "", fmt.Errorf("event trace id contains sensitive material")
	}
	redacted, err := redactor.Redact(eventEnvelope{
		Schema:    EventEnvelopeSchema,
		Summary:   input.Payload.Summary,
		Reference: input.Payload.Reference,
		Data:      input.Payload.Attributes,
	})
	if err != nil {
		return "", fmt.Errorf("脱敏 workflow Event: %w", err)
	}
	if err := validateEventTextBounds(reflect.ValueOf(redacted)); err != nil {
		return "", err
	}
	payload, err := policy.CanonicalJSON(redacted)
	if err != nil {
		return "", fmt.Errorf("序列化 workflow Event envelope: %w", err)
	}
	if len(payload) > maxEventPayloadBytes {
		return "", fmt.Errorf("%w: envelope bytes=%d limit=%d", ErrEventPayloadTooLarge, len(payload), maxEventPayloadBytes)
	}
	return string(payload), nil
}

func isCatalogEvent(eventType string) bool {
	for _, candidate := range eventCatalog {
		if eventType == candidate {
			return true
		}
	}
	return false
}

func validateEventTextBounds(value reflect.Value) error {
	if !value.IsValid() {
		return nil
	}
	for value.Kind() == reflect.Interface || value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return nil
		}
		value = value.Elem()
	}
	switch value.Kind() {
	case reflect.String:
		text := value.String()
		if !utf8.ValidString(text) {
			return fmt.Errorf("event text is not valid UTF-8")
		}
		if len(text) > MaxEventTextBytes {
			return fmt.Errorf("%w: text bytes=%d limit=%d", ErrEventPayloadTooLarge, len(text), MaxEventTextBytes)
		}
	case reflect.Map:
		iterator := value.MapRange()
		for iterator.Next() {
			if err := validateEventTextBounds(iterator.Key()); err != nil {
				return err
			}
			if err := validateEventTextBounds(iterator.Value()); err != nil {
				return err
			}
		}
	case reflect.Slice, reflect.Array:
		for index := 0; index < value.Len(); index++ {
			if err := validateEventTextBounds(value.Index(index)); err != nil {
				return err
			}
		}
	case reflect.Struct:
		for index := 0; index < value.NumField(); index++ {
			if value.Type().Field(index).PkgPath == "" {
				if err := validateEventTextBounds(value.Field(index)); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func redactPersistentPayload(value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return value, nil
	}
	redactor := policy.NewRedactor()
	if !json.Valid([]byte(value)) {
		return redactor.RedactText(value), nil
	}
	var decoded any
	if err := json.Unmarshal([]byte(value), &decoded); err != nil {
		return "", fmt.Errorf("解析持久化 JSON: %w", err)
	}
	redacted, err := redactor.RedactJSON(decoded)
	if err != nil {
		return "", fmt.Errorf("脱敏持久化 JSON: %w", err)
	}
	return string(redacted), nil
}
