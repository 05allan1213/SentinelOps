package eval

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"SentinelOps/internal/ai/evidence"
	"SentinelOps/internal/ai/policy"
	aitrace "SentinelOps/internal/ai/trace"
	"SentinelOps/internal/ai/workflow"
	"SentinelOps/internal/dao/mysql"

	"gorm.io/gorm"
)

// MySQLTruthReader 从现有 durable/Trace 表读取事实；所有操作均为只读查询。
type MySQLTruthReader struct {
	DB       *gorm.DB
	Interval time.Duration
}

// Wait 等待 Run 到达可评估终态，再读取同一 Run 的 Event、Trace、Approval 和 Effect。
func (r *MySQLTruthReader) Wait(ctx context.Context, runID string) (RunTruth, error) {
	if r == nil || r.DB == nil {
		return RunTruth{}, fmt.Errorf("MySQL truth reader is not initialized")
	}
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return RunTruth{}, fmt.Errorf("run id is required")
	}
	interval := r.Interval
	if interval <= 0 {
		interval = 250 * time.Millisecond
	}
	for {
		var run mysql.WorkflowRun
		result := r.DB.WithContext(ctx).Where("id = ?", runID).First(&run)
		if result.Error == nil {
			if isEvalTerminal(run.Status) {
				return r.readTruth(ctx, &run)
			}
		} else if result.Error != nil && !errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return RunTruth{}, fmt.Errorf("read workflow Run: %w", result.Error)
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return RunTruth{}, ctx.Err()
		case <-timer.C:
		}
	}
}

func (r *MySQLTruthReader) readTruth(ctx context.Context, run *mysql.WorkflowRun) (RunTruth, error) {
	truth := RunTruth{RunID: run.ID, Status: run.Status, SecretLeak: workflowRunContainsSecret(run)}
	if run.ParkReason != nil {
		truth.ParkReason = *run.ParkReason
	}
	var events []mysql.WorkflowEvent
	if err := r.DB.WithContext(ctx).Where("run_id = ?", run.ID).Order("seq ASC").Find(&events).Error; err != nil {
		return RunTruth{}, fmt.Errorf("read workflow Events: %w", err)
	}
	projectEvents(&truth, events)
	var approvals []mysql.AgentApproval
	if err := r.DB.WithContext(ctx).Where("run_id = ?", run.ID).Order("created_at ASC").Find(&approvals).Error; err != nil {
		return RunTruth{}, fmt.Errorf("read Agent Approvals: %w", err)
	}
	for _, approval := range approvals {
		truth.SecretLeak = truth.SecretLeak || approvalContainsSecret(approval)
		if approval.Status == workflow.ApprovalStatusApproved {
			truth.Approval = true
		}
	}
	var effects []mysql.AgentEffect
	if err := r.DB.WithContext(ctx).Where("run_id = ?", run.ID).Order("created_at ASC").Find(&effects).Error; err != nil {
		return RunTruth{}, fmt.Errorf("read Agent Effects: %w", err)
	}
	seenKeys := make(map[string]struct{}, len(effects))
	for _, effect := range effects {
		truth.SecretLeak = truth.SecretLeak || effectContainsSecret(effect)
		if effect.Status == workflow.EffectStatusSucceeded {
			truth.Effect = true
		}
		if effect.IdempotencyKey != "" {
			if _, exists := seenKeys[effect.IdempotencyKey]; exists {
				truth.DuplicateEffect = true
			}
			seenKeys[effect.IdempotencyKey] = struct{}{}
		}
	}

	if strings.TrimSpace(run.UserID) == "" {
		return RunTruth{}, fmt.Errorf("workflow Run owner is missing")
	}
	traceContext := policy.WithIdentity(ctx, policy.Identity{
		UserID: run.UserID,
		Role:   policy.RoleViewer,
		Scope:  policy.Scope{UserID: run.UserID},
	})
	traceDAO := mysql.NewTraceDAOWithDB(r.DB)
	trace, traceErr := traceDAO.GetRunByWorkflowRunID(traceContext, run.ID)
	if traceErr == nil {
		truth.Trace = projectTraceTruth(run.TraceQuality, *trace)
		truth.SecretLeak = truth.SecretLeak || traceRunContainsSecret(*trace)
		nodes, err := traceDAO.ListNodesByTraceID(traceContext, trace.TraceID)
		if err != nil {
			return RunTruth{}, fmt.Errorf("read Trace nodes: %w", err)
		}
		if calls := projectTraceToolCalls(nodes); len(calls) > 0 {
			truth.ToolCalls = mergeToolCalls(truth.ToolCalls, calls)
		}
		truth.SecretLeak = truth.SecretLeak || traceNodesContainSecret(nodes)
	} else if !errors.Is(traceErr, gorm.ErrRecordNotFound) {
		return RunTruth{}, fmt.Errorf("read Trace: %w", traceErr)
	}
	return truth, nil
}

func isEvalTerminal(status string) bool {
	return isCaseTerminalStatus(status)
}

func projectEvents(truth *RunTruth, events []mysql.WorkflowEvent) {
	invalidEvidence := false
	for _, event := range events {
		if looksLikeSecret(event.Payload) {
			truth.SecretLeak = true
		}
		data := eventData(event.Payload)
		switch event.EventType {
		case workflow.EventRunResumed:
			truth.RecoveryMode = "resume"
		case workflow.EventRunReplayed:
			truth.RecoveryMode = "replay"
		case workflow.EventRunParked:
			if stringValue(data, "mode") == "parked" {
				truth.RecoveryMode = "parked"
			}
		case workflow.EventAgentReplan:
			truth.ReplanCount++
		case workflow.EventAgentToolCall:
			name := stringValue(data, "tool_name", "tool")
			if name != "" {
				truth.ToolCalls = append(truth.ToolCalls, ToolCallTruth{Name: name})
			}
		case workflow.EventAgentToolResult:
			name := stringValue(data, "tool_name", "tool")
			success := false
			successKnown := false
			if value, ok := data["success"].(bool); ok {
				success = value
				successKnown = true
			}
			if status, ok := data["status"].(string); ok {
				switch strings.ToLower(strings.TrimSpace(status)) {
				case "failed", "error":
					success = false
					successKnown = true
				case "ok", "success", "succeeded":
					if !successKnown {
						success = true
					}
					successKnown = true
				}
			}
			matched := false
			for index := len(truth.ToolCalls) - 1; index >= 0; index-- {
				if !truth.ToolCalls[index].SuccessKnown && (name == "" || truth.ToolCalls[index].Name == name) {
					truth.ToolCalls[index].Success = success
					truth.ToolCalls[index].SuccessKnown = successKnown
					matched = true
					break
				}
			}
			if !matched && name != "" {
				truth.ToolCalls = append(truth.ToolCalls, ToolCallTruth{Name: name, Success: success, SuccessKnown: successKnown})
			}
		case workflow.EventEvidenceRetrieved, workflow.EventEvidenceCited:
			references := stringSliceValue(data, "evidence_ids")
			if reference := stringValue(data, "reference", "evidence_id", "document_id"); reference != "" {
				references = append(references, reference)
			}
			truth.Evidence.References = append(truth.Evidence.References, references...)
			if event.EventType == workflow.EventEvidenceCited {
				truth.Evidence.Cited = true
				if len(references) == 0 {
					invalidEvidence = true
				}
			}
			for _, reference := range references {
				if !isCanonicalEvidenceID(reference) {
					invalidEvidence = true
				}
			}
			if valid, ok := data["valid"].(bool); ok && !valid {
				invalidEvidence = true
			}
			truth.Evidence.Valid = len(truth.Evidence.References) > 0 && !invalidEvidence
			truth.InvalidEvidence = invalidEvidence
		}
		code := strings.ToUpper(stringValue(data, "error_code", "code"))
		if strings.Contains(code, "FORBIDDEN") || strings.Contains(code, "POLICY_MUTATION_DISABLED") {
			truth.RBACDenied = true
		}
	}
}

func isCanonicalEvidenceID(reference string) bool {
	citations := evidence.ExtractCitations(evidence.CitationMarker(reference))
	return reference == strings.ToLower(reference) && len(citations) == 1 && citations[0].EvidenceID == reference
}

func workflowRunContainsSecret(run *mysql.WorkflowRun) bool {
	if run == nil {
		return false
	}
	return stringsContainSecret(
		run.QueryText, optionalString(run.ImmutableInputJSON), optionalString(run.ContextSnapshotJSON),
		optionalString(run.ModelSnapshot), optionalString(run.ToolSnapshot), optionalString(run.SkillSnapshot),
		optionalString(run.FeatureSnapshot), run.InputPayload, run.OutputPayload, run.ErrorMessage,
	)
}

func approvalContainsSecret(approval mysql.AgentApproval) bool {
	return stringsContainSecret(approval.ProposalJSONRedacted, optionalString(approval.DecisionReason))
}

func effectContainsSecret(effect mysql.AgentEffect) bool {
	return stringsContainSecret(
		optionalString(effect.RequestRedacted), optionalString(effect.ResponseRedacted),
		optionalString(effect.ExternalReference), optionalString(effect.ResolutionEvidenceRedacted),
		optionalString(effect.LastError),
	)
}

func traceRunContainsSecret(trace mysql.TraceRun) bool {
	return stringsContainSecret(trace.QueryText, trace.ErrorMessage, trace.Tags)
}

func traceNodesContainSecret(nodes []mysql.TraceNode) bool {
	for _, node := range nodes {
		if stringsContainSecret(
			node.ErrorMessage, node.PromptText, node.CompletionText, node.QueryText,
			node.RetrievedDocs, node.Metadata,
		) {
			return true
		}
	}
	return false
}

func stringsContainSecret(values ...string) bool {
	for _, value := range values {
		if looksLikeSecret(value) {
			return true
		}
	}
	return false
}

func optionalString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func projectTraceTruth(runQuality string, trace mysql.TraceRun) TraceTruth {
	var tags map[string]any
	_ = json.Unmarshal([]byte(trace.Tags), &tags)
	traceQuality, _ := tags["trace_quality"].(string)
	return TraceTruth{
		Complete:        runQuality == workflow.TraceQualityComplete && traceQuality == workflow.TraceQualityComplete,
		LatencyMs:       trace.DurationMs,
		InputTokens:     trace.TotalInputTokens,
		CachedTokens:    trace.CachedInputTokens,
		OutputTokens:    trace.TotalOutputTokens,
		ReasoningTokens: trace.ReasoningTokens,
		CostCNY:         trace.EstimatedCostCNY,
	}
}

func projectTraceToolCalls(nodes []mysql.TraceNode) []ToolCallTruth {
	result := make([]ToolCallTruth, 0)
	for _, node := range nodes {
		if node.NodeType != aitrace.NodeTypeTool {
			continue
		}
		name := strings.TrimSpace(node.NodeName)
		var metadata map[string]any
		if json.Unmarshal([]byte(node.Metadata), &metadata) == nil {
			if value := stringValue(metadata, "tool_name"); value != "" {
				name = value
			}
		}
		if name == "" {
			name = "unknown_tool"
		}
		result = append(result, ToolCallTruth{Name: name, Success: node.Status == aitrace.StatusSuccess, SuccessKnown: true})
	}
	return result
}

func mergeToolCalls(eventCalls, traceCalls []ToolCallTruth) []ToolCallTruth {
	if len(traceCalls) == 0 {
		return eventCalls
	}
	result := append([]ToolCallTruth(nil), traceCalls...)
	traceIndexes := make(map[string][]int, len(traceCalls))
	for index, call := range traceCalls {
		traceIndexes[call.Name] = append(traceIndexes[call.Name], index)
	}
	eventCounts := make(map[string]int, len(eventCalls))
	for _, call := range eventCalls {
		occurrence := eventCounts[call.Name]
		eventCounts[call.Name] = occurrence + 1
		if indexes := traceIndexes[call.Name]; occurrence < len(indexes) {
			index := indexes[occurrence]
			if call.SuccessKnown {
				result[index].Success = result[index].Success && call.Success
			}
			continue
		}
		call.Success = false
		result = append(result, call)
	}
	return result
}

func eventData(payload string) map[string]any {
	var envelope struct {
		Data map[string]any `json:"data"`
	}
	if json.Unmarshal([]byte(payload), &envelope) == nil && envelope.Data != nil {
		return envelope.Data
	}
	return nil
}

func stringValue(values map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := values[key].(string); ok {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func stringSliceValue(values map[string]any, key string) []string {
	valuesRaw, ok := values[key].([]any)
	if !ok {
		return nil
	}
	result := make([]string, 0, len(valuesRaw))
	for _, value := range valuesRaw {
		if item, ok := value.(string); ok && strings.TrimSpace(item) != "" {
			result = append(result, strings.TrimSpace(item))
		}
	}
	return result
}
