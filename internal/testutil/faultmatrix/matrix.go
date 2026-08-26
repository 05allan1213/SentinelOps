// Package faultmatrix 提供 P41 声明式故障矩阵的测试可见校验。
package faultmatrix

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	SchemaVersion = "sentinelops/fault-matrix/v1"
	ResumePass    = "RESUME PASS"
	ReplayPass    = "REPLAY PASS"
	Parked        = "PARKED AS DESIGNED"
)

// Matrix 是故障注入矩阵文件的严格结构。
type Matrix struct {
	Schema     string     `yaml:"schema"`
	Version    int        `yaml:"version"`
	Outcomes   []string   `yaml:"outcomes"`
	Invariants Invariants `yaml:"invariants"`
	Cases      []Case     `yaml:"cases"`
}

// Invariants 是所有代表性恢复 Case 共用的身份与安全不变量。
type Invariants struct {
	RunIDUnchanged       bool `yaml:"run_id_unchanged"`
	AttemptRotates       bool `yaml:"attempt_rotates"`
	TraceIDRotates       bool `yaml:"trace_id_rotates"`
	ContextPreserved     bool `yaml:"context_preserved"`
	BudgetPreserved      bool `yaml:"budget_preserved"`
	SnapshotPreserved    bool `yaml:"runtime_snapshot_preserved"`
	MutationDeduplicated bool `yaml:"mutation_deduplicated"`
}

// Case 描述一个可确定性注入的故障点及其唯一允许结果。
type Case struct {
	ID              string   `yaml:"id"`
	FaultPoint      string   `yaml:"fault_point"`
	Injection       string   `yaml:"injection"`
	ExpectedOutcome string   `yaml:"expected_outcome"`
	RecoveryMode    string   `yaml:"recovery_mode"`
	RunIDUnchanged  bool     `yaml:"run_id_unchanged"`
	AttemptRotates  bool     `yaml:"attempt_rotates"`
	TraceIDRotates  bool     `yaml:"trace_id_rotates"`
	Assertions      []string `yaml:"assertions"`
	Requires        []string `yaml:"requires"`
}

// Load 以 KnownFields 模式读取矩阵，拒绝拼写漂移和未声明字段。
func Load(path string) (Matrix, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Matrix{}, fmt.Errorf("read fault matrix: %w", err)
	}
	var matrix Matrix
	decoder := yaml.NewDecoder(strings.NewReader(string(data)))
	decoder.KnownFields(true)
	if err := decoder.Decode(&matrix); err != nil {
		return Matrix{}, fmt.Errorf("decode fault matrix: %w", err)
	}
	return matrix, nil
}

// Validate 检查版本、唯一 ID、允许结果和恢复身份不变量。
func (m Matrix) Validate() error {
	if m.Schema != SchemaVersion || m.Version != 1 {
		return fmt.Errorf("schema/version=%q/%d", m.Schema, m.Version)
	}
	allowed := map[string]bool{ResumePass: true, ReplayPass: true, Parked: true}
	if len(m.Outcomes) != len(allowed) {
		return fmt.Errorf("outcomes=%v", m.Outcomes)
	}
	declared := make(map[string]struct{}, len(m.Outcomes))
	for _, outcome := range m.Outcomes {
		if !allowed[outcome] {
			return fmt.Errorf("unsupported outcome %q", outcome)
		}
		if _, ok := declared[outcome]; ok {
			return fmt.Errorf("duplicate outcome %q", outcome)
		}
		declared[outcome] = struct{}{}
	}
	if !m.Invariants.RunIDUnchanged || !m.Invariants.AttemptRotates || !m.Invariants.TraceIDRotates ||
		!m.Invariants.ContextPreserved || !m.Invariants.BudgetPreserved || !m.Invariants.SnapshotPreserved ||
		!m.Invariants.MutationDeduplicated {
		return fmt.Errorf("global recovery invariants are not all enabled")
	}
	seen := make(map[string]struct{}, len(m.Cases))
	expectedByMode := map[string]string{"resume": ResumePass, "replay": ReplayPass, "parked": Parked}
	for _, item := range m.Cases {
		if strings.TrimSpace(item.ID) == "" || strings.TrimSpace(item.FaultPoint) == "" || strings.TrimSpace(item.Injection) == "" {
			return fmt.Errorf("case %q has incomplete identity/injection", item.ID)
		}
		if _, ok := seen[item.ID]; ok {
			return fmt.Errorf("duplicate case %q", item.ID)
		}
		seen[item.ID] = struct{}{}
		if !allowed[item.ExpectedOutcome] {
			return fmt.Errorf("case %q has unsupported outcome %q", item.ID, item.ExpectedOutcome)
		}
		if expected, ok := expectedByMode[item.RecoveryMode]; !ok || expected != item.ExpectedOutcome {
			return fmt.Errorf("case %q has recovery mode/outcome %q/%q", item.ID, item.RecoveryMode, item.ExpectedOutcome)
		}
		if !item.RunIDUnchanged || !item.AttemptRotates || !item.TraceIDRotates || len(item.Assertions) == 0 || len(item.Requires) == 0 {
			return fmt.Errorf("case %q is missing recovery assertions", item.ID)
		}
	}
	return nil
}

// Case 返回稳定 ID 对应的矩阵项。
func (m Matrix) Case(id string) (Case, bool) {
	for _, item := range m.Cases {
		if item.ID == id {
			return item, true
		}
	}
	return Case{}, false
}

// OutcomeForRecovery 把既有 selector 模式映射为 P41 报告标签。
func OutcomeForRecovery(mode, parkReason string) string {
	if mode == "parked" || strings.TrimSpace(parkReason) != "" {
		return Parked
	}
	if mode == "resume" {
		return ResumePass
	}
	if mode == "replay" {
		return ReplayPass
	}
	return ""
}

// OutcomeForInvocation 把现有 Effect invocation 分类映射为 P41 报告标签。
func OutcomeForInvocation(class string) string {
	switch class {
	case "succeeded":
		return ResumePass
	case "safe_not_sent":
		return ReplayPass
	default:
		return Parked
	}
}
