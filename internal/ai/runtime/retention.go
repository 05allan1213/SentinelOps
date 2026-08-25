package runtime

import (
	"context"
	"errors"
	"fmt"
	"time"

	"SentinelOps/internal/ai/workflow"
	"SentinelOps/internal/dao/mysql"
)

// RetentionPolicy 保存当前经 admin 审计的物理保留期。
type RetentionPolicy struct {
	PayloadDays int
	AuditDays   int
}

// DefaultRetentionPolicy 返回 Spec 固定的 30/180 天默认值。
func DefaultRetentionPolicy() RetentionPolicy {
	return RetentionPolicy{PayloadDays: 30, AuditDays: 180}
}

// Validate 防止敏感 payload 比审计元数据保留更久或无界增长。
func (p RetentionPolicy) Validate() error {
	if p.PayloadDays <= 0 || p.PayloadDays > 3650 {
		return fmt.Errorf("payload retention days must be between 1 and 3650")
	}
	if p.AuditDays < p.PayloadDays || p.AuditDays > 3650 {
		return fmt.Errorf("audit retention days must be between payload retention and 3650")
	}
	return nil
}

// Cutoffs 从同一个当前时间计算 payload/audit 截止点。
func (p RetentionPolicy) Cutoffs(now time.Time) (time.Time, time.Time) {
	return now.AddDate(0, 0, -p.PayloadDays), now.AddDate(0, 0, -p.AuditDays)
}

type retentionWorkflowBackend interface {
	ClaimRetentionLease(context.Context, workflow.RetentionClaimInput) (*workflow.RetentionLease, bool, error)
	ReleaseRetentionLease(context.Context, workflow.RetentionReleaseInput) error
	PhysicalDeleteSensitivePayloads(context.Context, time.Time, int) (workflow.RetentionCleanupCounts, error)
	PhysicalDeleteAuditMetadata(context.Context, time.Time, int) (workflow.RetentionCleanupCounts, error)
}

type retentionTraceBackend interface {
	PhysicalDeleteTracePayloads(context.Context, time.Time, int) (int64, error)
	PhysicalDeleteTraceMetadata(context.Context, time.Time, int) (int64, error)
}

// RetentionConfig 只描述现有 Worker poll loop 中的有界批次和 lease 参数。
type RetentionConfig struct {
	Owner         string
	LeaseDuration time.Duration
	Interval      time.Duration
	BatchSize     int
	Policy        RetentionPolicy
	LoadPolicy    func(context.Context) (RetentionPolicy, error)
}

// RetentionResult 汇总一次持有 lease 的清理批次。
type RetentionResult struct {
	Workflow      workflow.RetentionCleanupCounts
	TracePayloads int64
	TraceMetadata int64
}

// RetentionCoordinator 只编排现有 GORMStore/TraceDAO，不拥有 SQL、模型或租约实现。
type RetentionCoordinator struct {
	workflow retentionWorkflowBackend
	trace    retentionTraceBackend
	config   RetentionConfig
	now      func() time.Time
}

// NewRetentionCoordinator 创建现有 Worker poll loop 的最薄 retention 编排器。
func NewRetentionCoordinator(workflowBackend retentionWorkflowBackend, traceBackend retentionTraceBackend, config RetentionConfig) (*RetentionCoordinator, error) {
	if workflowBackend == nil || traceBackend == nil {
		return nil, fmt.Errorf("workflow Store and TraceDAO are required for retention")
	}
	if config.Owner == "" || config.LeaseDuration < time.Millisecond || config.LeaseDuration > 24*time.Hour {
		return nil, fmt.Errorf("valid retention owner and lease duration are required")
	}
	if config.Interval <= 0 || config.BatchSize <= 0 || config.BatchSize > 1000 {
		return nil, fmt.Errorf("retention interval and batch size are invalid")
	}
	if config.LoadPolicy == nil {
		if err := config.Policy.Validate(); err != nil {
			return nil, err
		}
	}
	return &RetentionCoordinator{workflow: workflowBackend, trace: traceBackend, config: config, now: time.Now}, nil
}

// RunOnce 尝试认领一次 generation-fenced 清理租约；未认领时不做任何删除。
func (c *RetentionCoordinator) RunOnce(ctx context.Context) (worked bool, result RetentionResult, returnErr error) {
	if c == nil {
		return false, RetentionResult{}, nil
	}
	lease, ok, err := c.workflow.ClaimRetentionLease(ctx, workflow.RetentionClaimInput{
		Owner: c.config.Owner, LeaseDuration: c.config.LeaseDuration,
	})
	if err != nil || !ok {
		return false, RetentionResult{}, err
	}
	worked = true
	now := c.now().UTC()
	defer func() {
		releaseErr := c.workflow.ReleaseRetentionLease(context.WithoutCancel(ctx), workflow.RetentionReleaseInput{
			Token: lease.Token, NextRunAt: now.Add(c.config.Interval),
		})
		returnErr = errors.Join(returnErr, releaseErr)
	}()
	policy := c.config.Policy
	if c.config.LoadPolicy != nil {
		policy, err = c.config.LoadPolicy(ctx)
		if err != nil {
			return worked, result, err
		}
	}
	if err := policy.Validate(); err != nil {
		return worked, result, err
	}
	payloadCutoff, auditCutoff := policy.Cutoffs(now)
	result.Workflow, err = c.workflow.PhysicalDeleteSensitivePayloads(ctx, payloadCutoff, c.config.BatchSize)
	if err != nil {
		return worked, result, err
	}
	result.TracePayloads, err = c.trace.PhysicalDeleteTracePayloads(ctx, payloadCutoff, c.config.BatchSize)
	if err != nil {
		return worked, result, err
	}
	// Trace metadata 依赖 workflow_runs 的终态时间关联，必须先于 Run 审计行删除。
	result.TraceMetadata, err = c.trace.PhysicalDeleteTraceMetadata(ctx, auditCutoff, c.config.BatchSize)
	if err != nil {
		return worked, result, err
	}
	auditCounts, err := c.workflow.PhysicalDeleteAuditMetadata(ctx, auditCutoff, c.config.BatchSize)
	if err != nil {
		return worked, result, err
	}
	result.Workflow.Runs += auditCounts.Runs
	result.Workflow.Events += auditCounts.Events
	result.Workflow.Checkpoints += auditCounts.Checkpoints
	result.Workflow.Approvals += auditCounts.Approvals
	result.Workflow.Effects += auditCounts.Effects
	return worked, result, nil
}

func terminalRetentionAnchor(run mysql.WorkflowRun) (time.Time, bool) {
	if workflow.RunOccupiesSession(run.Status) || run.FinishedAt == nil || run.FinishedAt.IsZero() {
		return time.Time{}, false
	}
	return *run.FinishedAt, true
}
