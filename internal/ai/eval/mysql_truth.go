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

// WaitForApproval 只读等待同一 Run 已发布的 pending Approval，并返回公开决策所需 CAS 字段。
func (r *MySQLTruthReader) WaitForApproval(ctx context.Context, runID string) (ApprovalTruth, error) {
	if err := r.validateScenarioProbe(runID); err != nil {
		return ApprovalTruth{}, err
	}
	for {
		var approval mysql.AgentApproval
		result := r.DB.WithContext(ctx).
			Where("run_id = ? AND status = ? AND published_at IS NOT NULL", strings.TrimSpace(runID), workflow.ApprovalStatusPending).
			Order("created_at ASC").First(&approval)
		if result.Error == nil {
			return ApprovalTruth{
				ID: approval.ID, RunID: approval.RunID, ProposalHash: approval.ProposalHash,
				Version: approval.Version, RequestedBy: approval.RequestedBy,
			}, nil
		}
		if !errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return ApprovalTruth{}, fmt.Errorf("read pending Agent Approval: %w", result.Error)
		}
		if err := r.failIfTerminal(ctx, runID, "Approval"); err != nil {
			return ApprovalTruth{}, err
		}
		if err := r.waitPoll(ctx); err != nil {
			return ApprovalTruth{}, err
		}
	}
}

// WaitForCheckpoint 只读等待至少一个当前 Run 的 committed Checkpoint。
func (r *MySQLTruthReader) WaitForCheckpoint(ctx context.Context, runID string) (AttemptTruth, error) {
	return r.waitForAttempt(ctx, runID, "Checkpoint", func(_ *mysql.WorkflowRun, checkpoints int64) bool {
		return checkpoints > 0
	})
}

// RemoveCheckpoints 是依赖注入场景的受控破坏操作：删除指定 Run 的已提交
// Checkpoint，使真实运行时在恢复时按 CheckpointMissing 进入 PARKED。
// 只允许作用于 Eval 在 throwaway 数据库中创建的 Run，不触碰其他 Run。
func (r *MySQLTruthReader) RemoveCheckpoints(ctx context.Context, runID string) error {
	if r == nil || r.DB == nil {
		return fmt.Errorf("mysql truth reader is not initialized")
	}
	if strings.TrimSpace(runID) == "" {
		return fmt.Errorf("run id is required")
	}
	if err := r.DB.WithContext(ctx).Where("run_id = ?", strings.TrimSpace(runID)).
		Delete(&mysql.WorkflowCheckpoint{}).Error; err != nil {
		return fmt.Errorf("remove scenario checkpoints: %w", err)
	}
	return nil
}

// WaitForRunningWithoutCheckpoint 等待 Worker 已认领 Run 且尚未提交 Checkpoint。
func (r *MySQLTruthReader) WaitForRunningWithoutCheckpoint(ctx context.Context, runID string) (AttemptTruth, error) {
	return r.waitForAttempt(ctx, runID, "pre-Checkpoint running Attempt", func(run *mysql.WorkflowRun, checkpoints int64) bool {
		return run.Status == workflow.RunStatusRunning && run.LeaseOwner != nil && run.Attempt > 0 && checkpoints == 0
	})
}

// WaitForDependencyCall 等待指定外部依赖对应的真实 Tool 调用事件。
func (r *MySQLTruthReader) WaitForDependencyCall(ctx context.Context, runID, dependency string) (AttemptTruth, error) {
	if err := r.validateScenarioProbe(runID); err != nil {
		return AttemptTruth{}, err
	}
	toolName := dependencyToolName(dependency)
	for {
		var events []mysql.WorkflowEvent
		if err := r.DB.WithContext(ctx).
			Where("run_id = ? AND event_type = ?", strings.TrimSpace(runID), workflow.EventAgentToolCall).
			Order("seq ASC").Find(&events).Error; err != nil {
			return AttemptTruth{}, fmt.Errorf("read dependency Tool calls: %w", err)
		}
		for _, event := range events {
			data := eventData(event.Payload)
			if stringValue(data, "tool_name", "tool") == toolName {
				return r.readAttemptTruth(ctx, runID)
			}
		}
		if err := r.failIfTerminal(ctx, runID, "dependency Tool call"); err != nil {
			return AttemptTruth{}, err
		}
		if err := r.waitPoll(ctx); err != nil {
			return AttemptTruth{}, err
		}
	}
}

func (r *MySQLTruthReader) waitForAttempt(
	ctx context.Context,
	runID, stage string,
	ready func(*mysql.WorkflowRun, int64) bool,
) (AttemptTruth, error) {
	if err := r.validateScenarioProbe(runID); err != nil {
		return AttemptTruth{}, err
	}
	for {
		truth, run, err := r.readAttempt(ctx, runID)
		if err == nil {
			if ready(run, truth.CheckpointCount) {
				return truth, nil
			}
			if isEvalTerminal(run.Status) {
				return AttemptTruth{}, fmt.Errorf("run reached terminal status before %s was observed", stage)
			}
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return AttemptTruth{}, err
		}
		if err := r.waitPoll(ctx); err != nil {
			return AttemptTruth{}, err
		}
	}
}

func (r *MySQLTruthReader) readAttemptTruth(ctx context.Context, runID string) (AttemptTruth, error) {
	truth, _, err := r.readAttempt(ctx, runID)
	return truth, err
}

func (r *MySQLTruthReader) readAttempt(ctx context.Context, runID string) (AttemptTruth, *mysql.WorkflowRun, error) {
	var run mysql.WorkflowRun
	if err := r.DB.WithContext(ctx).Where("id = ?", strings.TrimSpace(runID)).First(&run).Error; err != nil {
		return AttemptTruth{}, nil, err
	}
	var checkpointCount int64
	if err := r.DB.WithContext(ctx).Model(&mysql.WorkflowCheckpoint{}).
		Where("run_id = ? AND committed_at IS NOT NULL", run.ID).Count(&checkpointCount).Error; err != nil {
		return AttemptTruth{}, nil, fmt.Errorf("count committed Workflow Checkpoints: %w", err)
	}
	truth := AttemptTruth{
		RunID: run.ID, Status: run.Status, Attempt: run.Attempt,
		LeaseGeneration: run.LeaseGeneration, CheckpointCount: checkpointCount,
	}
	if run.LeaseOwner != nil {
		truth.LeaseOwner = *run.LeaseOwner
	}
	return truth, &run, nil
}

func (r *MySQLTruthReader) failIfTerminal(ctx context.Context, runID, stage string) error {
	var run mysql.WorkflowRun
	result := r.DB.WithContext(ctx).Select("id", "status", "attempt", "max_attempts").Where("id = ?", strings.TrimSpace(runID)).First(&run)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil
		}
		return fmt.Errorf("read workflow Run while waiting for %s: %w", stage, result.Error)
	}
	if isEvalTerminal(run.Status) {
		return fmt.Errorf("run reached terminal status before %s was observed", stage)
	}
	if run.Status == workflow.RunStatusPending && run.Attempt >= run.MaxAttempts {
		return fmt.Errorf("run reached terminal status before %s was observed", stage)
	}
	return nil
}

func (r *MySQLTruthReader) validateScenarioProbe(runID string) error {
	if r == nil || r.DB == nil {
		return fmt.Errorf("mysql truth reader is not initialized")
	}
	if strings.TrimSpace(runID) == "" {
		return fmt.Errorf("run id is required")
	}
	return nil
}

func (r *MySQLTruthReader) waitPoll(ctx context.Context) error {
	interval := r.Interval
	if interval <= 0 {
		interval = 250 * time.Millisecond
	}
	timer := time.NewTimer(interval)
	select {
	case <-ctx.Done():
		timer.Stop()
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func dependencyToolName(dependency string) string {
	switch strings.TrimSpace(dependency) {
	case "mcp":
		return "mcp_agent"
	case "skill":
		return "skill_agent"
	case "knowledge", "rag":
		return "query_internal_docs"
	default:
		return strings.TrimSpace(dependency)
	}
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
			// The worker records a replay selection for the first execution so
			// that the Runner can start from an empty checkpoint.  That is not a
			// user-visible recovery.  Only later attempts represent replay
			// recovery in Eval truth.
			if eventAttempt(data) > 1 {
				truth.RecoveryMode = "replay"
			}
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

func eventAttempt(values map[string]any) int {
	if values == nil {
		return 0
	}
	switch value := values["attempt"].(type) {
	case float64:
		return int(value)
	case json.Number:
		attempt, err := value.Int64()
		if err == nil {
			return int(attempt)
		}
	}
	return 0
}
