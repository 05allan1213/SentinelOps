// Package eval 提供无官方 Eval Runner 时的最薄生产 Runtime 评估适配。
package eval

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"strings"

	"SentinelOps/internal/ai/policy"
	"SentinelOps/internal/config"

	"gopkg.in/yaml.v3"
)

// CaseSchema 是 P39 Eval Case 的稳定版本标识；Dataset 版本化由 P40 负责。
const CaseSchema = "sentinelops/eval-case/v1"

// EvalCase 描述一次通过生产 API 执行的最小评估输入和确定性期望。
type EvalCase struct {
	ID        string     `json:"id" yaml:"id"`
	SessionID string     `json:"session_id,omitempty" yaml:"session_id,omitempty"`
	Query     string     `json:"query" yaml:"query"`
	Agent     string     `json:"agent,omitempty" yaml:"agent,omitempty"`
	Expected  Expected   `json:"expected,omitempty" yaml:"expected,omitempty"`
	Forbidden Forbidden  `json:"forbidden,omitempty" yaml:"forbidden,omitempty"`
	Budget    EvalBudget `json:"budget,omitempty" yaml:"budget,omitempty"`
}

// Expected 保存当前 Case 必须满足的确定性事实。
type Expected struct {
	Statuses      []string `json:"statuses,omitempty" yaml:"statuses,omitempty"`
	Tools         []string `json:"tools,omitempty" yaml:"tools,omitempty"`
	RecoveryMode  string   `json:"recovery_mode,omitempty" yaml:"recovery_mode,omitempty"`
	EvidenceValid bool     `json:"evidence_valid,omitempty" yaml:"evidence_valid,omitempty"`
	RBACDenied    bool     `json:"rbac_denied,omitempty" yaml:"rbac_denied,omitempty"`
	Approval      bool     `json:"approval,omitempty" yaml:"approval,omitempty"`
	Effect        bool     `json:"effect,omitempty" yaml:"effect,omitempty"`
}

// Forbidden 保存当前 Case 必须不存在的确定性行为。
type Forbidden struct {
	Tools           []string `json:"tools,omitempty" yaml:"tools,omitempty"`
	RBACWrite       bool     `json:"rbac_write,omitempty" yaml:"rbac_write,omitempty"`
	SecretLeak      bool     `json:"secret_leak,omitempty" yaml:"secret_leak,omitempty"`
	DuplicateEffect bool     `json:"duplicate_effect,omitempty" yaml:"duplicate_effect,omitempty"`
}

// RunHandle 是生产 API 创建 Run 后返回的不可变身份。
type RunHandle struct {
	RunID     string `json:"run_id"`
	SessionID string `json:"session_id,omitempty"`
	Status    string `json:"status,omitempty"`
}

// ProductionRuntime 是生产 API 的最小提交边界；它不暴露 Agent、Runner 或 Store。
type ProductionRuntime interface {
	Submit(ctx context.Context, item EvalCase) (RunHandle, error)
}

// RunTruth 是从 MySQL Workflow、Trace、Approval、Effect 和 Evidence 读取的评估事实。
type RunTruth struct {
	RunID           string
	Status          string
	ParkReason      string
	RecoveryMode    string
	ReplanCount     int
	Trace           TraceTruth
	ToolCalls       []ToolCallTruth
	Evidence        EvidenceTruth
	RBACDenied      bool
	Approval        bool
	Effect          bool
	DuplicateEffect bool
	SecretLeak      bool
	InvalidEvidence bool
	BudgetExceeded  bool
}

// TraceTruth 复用已有 agent_trace_runs 的审计指标，不复制 Dashboard KPI。
type TraceTruth struct {
	Complete        bool
	LatencyMs       int64
	InputTokens     int
	CachedTokens    int
	OutputTokens    int
	ReasoningTokens int
	CostCNY         float64
}

// ToolCallTruth 描述 durable Event 中一个 Tool 调用及其结果。
type ToolCallTruth struct {
	Name         string
	Success      bool
	SuccessKnown bool
}

// EvidenceTruth 描述已有 Evidence 引用的确定性校验结果。
type EvidenceTruth struct {
	Valid      bool
	Cited      bool
	References []string
}

// CaseResult 是一份不含 Secret/模型输入原文的最小评估报告。
type CaseResult struct {
	EvalRunID           string   `json:"eval_run_id"`
	CaseID              string   `json:"case_id"`
	RunID               string   `json:"run_id"`
	Passed              bool     `json:"passed"`
	ExecutionSuccess    bool     `json:"execution_success"`
	Status              string   `json:"status"`
	ActualTools         []string `json:"actual_tools,omitempty"`
	ForbiddenTools      []string `json:"forbidden_tools,omitempty"`
	RecoveryMode        string   `json:"recovery_mode,omitempty"`
	ToolCallSuccessRate float64  `json:"tool_call_success_rate"`
	ReplanCount         int      `json:"replan_count"`
	EvidenceValid       bool     `json:"evidence_valid"`
	RBACDenied          bool     `json:"rbac_denied"`
	ApprovalObserved    bool     `json:"approval_observed"`
	EffectObserved      bool     `json:"effect_observed"`
	DuplicateEffect     bool     `json:"duplicate_effect"`
	SecretLeak          bool     `json:"secret_leak"`
	InvalidEvidence     bool     `json:"invalid_evidence"`
	BudgetExceeded      bool     `json:"budget_exceeded"`
	ToolCallCount       int      `json:"tool_call_count"`
	SuccessfulToolCalls int      `json:"successful_tool_calls"`
	LatencyMs           int64    `json:"latency_ms"`
	InputTokens         int      `json:"input_tokens"`
	CachedTokens        int      `json:"cached_tokens"`
	OutputTokens        int      `json:"output_tokens"`
	ReasoningTokens     int      `json:"reasoning_tokens"`
	CostCNY             float64  `json:"cost_cny"`
	Failures            []string `json:"failures,omitempty"`
}

type caseDocument struct {
	Schema string     `json:"schema" yaml:"schema"`
	Cases  []EvalCase `json:"cases" yaml:"cases"`
}

// Validate 校验 Case 的最小形状和不允许进入 Dataset 的明显 Secret 片段。
func (c EvalCase) Validate() error {
	if strings.TrimSpace(c.ID) == "" {
		return fmt.Errorf("eval case id is required")
	}
	if strings.TrimSpace(c.Query) == "" {
		return fmt.Errorf("eval case query is required")
	}
	for _, field := range []struct {
		name  string
		value string
	}{
		{name: "id", value: c.ID},
		{name: "session_id", value: c.SessionID},
		{name: "query", value: c.Query},
		{name: "agent", value: c.Agent},
	} {
		if looksLikeSecret(field.value) {
			return fmt.Errorf("eval case %s appears to contain secret material", field.name)
		}
	}
	if len(c.Expected.Statuses) == 0 {
		return fmt.Errorf("at least one expected terminal status is required")
	}
	if c.Budget.MaxLatencyMS < 0 || c.Budget.MaxTotalTokens < 0 || c.Budget.MaxCostCNY < 0 {
		return fmt.Errorf("eval case budget limits must not be negative")
	}
	seenStatuses := make(map[string]struct{}, len(c.Expected.Statuses))
	for _, status := range c.Expected.Statuses {
		if status != strings.TrimSpace(status) || !isCaseTerminalStatus(status) {
			return fmt.Errorf("unsupported expected terminal status %q", status)
		}
		if _, exists := seenStatuses[status]; exists {
			return fmt.Errorf("duplicate expected terminal status %q", status)
		}
		seenStatuses[status] = struct{}{}
	}
	if mode := c.Expected.RecoveryMode; mode != "" && mode != "resume" && mode != "replay" && mode != "parked" {
		return fmt.Errorf("unsupported expected recovery mode %q", mode)
	}
	expectedTools := make(map[string]struct{}, len(c.Expected.Tools))
	for _, value := range c.Expected.Tools {
		if value != strings.TrimSpace(value) || value == "" {
			return fmt.Errorf("tool names must not be empty or padded")
		}
		if looksLikeSecret(value) {
			return fmt.Errorf("tool name appears to contain secret material")
		}
		if _, exists := expectedTools[value]; exists {
			return fmt.Errorf("duplicate expected tool %q", value)
		}
		expectedTools[value] = struct{}{}
	}
	forbiddenTools := make(map[string]struct{}, len(c.Forbidden.Tools))
	for _, value := range c.Forbidden.Tools {
		if value != strings.TrimSpace(value) || value == "" {
			return fmt.Errorf("tool names must not be empty or padded")
		}
		if looksLikeSecret(value) {
			return fmt.Errorf("tool name appears to contain secret material")
		}
		if _, exists := forbiddenTools[value]; exists {
			return fmt.Errorf("duplicate forbidden tool %q", value)
		}
		if _, exists := expectedTools[value]; exists {
			return fmt.Errorf("tool %q cannot be both expected and forbidden", value)
		}
		forbiddenTools[value] = struct{}{}
	}
	return nil
}

func isCaseTerminalStatus(status string) bool {
	switch strings.TrimSpace(status) {
	case "success", "succeeded", "failed", "canceled", "parked":
		return true
	default:
		return false
	}
}

// LoadCases 严格读取 YAML/JSON Case 列表，未知字段按安全配置错误拒绝。
func LoadCases(reader io.Reader) ([]EvalCase, error) {
	if reader == nil {
		return nil, fmt.Errorf("case reader is required")
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("read eval cases: %w", err)
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, fmt.Errorf("eval cases are empty")
	}
	var root yaml.Node
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&root); err != nil {
		return nil, fmt.Errorf("decode eval cases: %w", err)
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); err != io.EOF {
		if err != nil {
			return nil, fmt.Errorf("decode eval cases: %w", err)
		}
		return nil, fmt.Errorf("multiple eval case documents are not supported")
	}
	if len(root.Content) != 1 {
		return nil, fmt.Errorf("eval cases have an invalid document root")
	}
	switch root.Content[0].Kind {
	case yaml.MappingNode:
		var document caseDocument
		if err := decodeStrict(data, &document); err != nil {
			return nil, fmt.Errorf("decode eval cases: %w", err)
		}
		if document.Schema != "" && document.Schema != CaseSchema {
			return nil, fmt.Errorf("unsupported eval case schema %q", document.Schema)
		}
		return validateCases(document.Cases)
	case yaml.SequenceNode:
		var cases []EvalCase
		if err := decodeStrict(data, &cases); err != nil {
			return nil, fmt.Errorf("decode eval cases: %w", err)
		}
		return validateCases(cases)
	default:
		return nil, fmt.Errorf("eval cases must be a list or a schema-wrapped document")
	}
}

func decodeStrict(data []byte, target any) error {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	return decoder.Decode(target)
}

func validateCases(cases []EvalCase) ([]EvalCase, error) {
	if len(cases) == 0 {
		return nil, fmt.Errorf("eval cases are empty")
	}
	seen := make(map[string]struct{}, len(cases))
	for index := range cases {
		if err := cases[index].Validate(); err != nil {
			return nil, fmt.Errorf("case %d: %w", index, err)
		}
		if _, ok := seen[cases[index].ID]; ok {
			return nil, fmt.Errorf("duplicate eval case id %q", cases[index].ID)
		}
		seen[cases[index].ID] = struct{}{}
	}
	return cases, nil
}

func looksLikeSecret(value string) bool {
	redactor := policy.NewRedactor()
	if redactor.RedactText(value) != value {
		return true
	}
	var decoded any
	if json.Unmarshal([]byte(value), &decoded) != nil {
		return false
	}
	redacted, err := redactor.Redact(decoded)
	if err != nil {
		return true
	}
	return structuredSecretChanged(decoded, redacted)
}

func structuredSecretChanged(original, redacted any) bool {
	switch value := original.(type) {
	case map[string]any:
		masked, ok := redacted.(map[string]any)
		if !ok || len(masked) != len(value) {
			return true
		}
		for key, child := range value {
			if structuredSecretChanged(child, masked[key]) {
				return true
			}
		}
		return false
	case []any:
		masked, ok := redacted.([]any)
		if !ok || len(masked) != len(value) {
			return true
		}
		for index := range value {
			if structuredSecretChanged(value[index], masked[index]) {
				return true
			}
		}
		return false
	case string:
		masked, ok := redacted.(string)
		if !ok {
			return true
		}
		if masked == value {
			return false
		}
		return config.SecretRef(value).Validate() != nil
	default:
		return !reflect.DeepEqual(original, redacted)
	}
}

// MarshalJSON 保持报告和 Case 的稳定 JSON 形状，同时避免误导入内部字段。
func (c EvalCase) MarshalJSON() ([]byte, error) {
	type alias EvalCase
	return json.Marshal(alias(c))
}
