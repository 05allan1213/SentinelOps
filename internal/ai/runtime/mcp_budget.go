package runtime

import (
	"context"
	"fmt"

	mcptools "SentinelOps/internal/ai/tools/mcp"
	"SentinelOps/internal/ai/workflow"
)

// MCPBudgetHook 将 official MCP Tool call 接入 P28 唯一 durable reservation primitive。
type MCPBudgetHook struct {
	Attempt *AttemptContext
}

// ReserveMCP 在官方 endpoint 前预留 MCP 次数、并发和结果上界。
func (h *MCPBudgetHook) ReserveMCP(ctx context.Context, request mcptools.BudgetRequest) error {
	if h == nil || h.Attempt == nil {
		return ErrAttemptContextMissing
	}
	if request.ReservationIdentity == "" || request.Subject == "" {
		return fmt.Errorf("mcp reservation identity and subject are required")
	}
	budget, err := requireCallBudget(h.Attempt.Budget)
	if err != nil {
		return err
	}
	_, err = budget.ReserveCall(ctx, BudgetCall{
		ReservationIdentity: request.ReservationIdentity,
		Kind:                BudgetCallKindMCP,
		Subject:             request.Subject,
		Lease:               h.Attempt.Lease,
		TraceID:             h.Attempt.Trace.ID,
		Deadline:            h.Attempt.Deadline,
		Estimate: workflow.BaseBudgetEstimate{
			ResultChars: request.ResultChars, ResultBytes: request.ResultBytes, Concurrency: request.Concurrency,
		},
	})
	return err
}

// SettleMCP 结算同一 reservation，不维护本地 MCP 计数器。
func (h *MCPBudgetHook) SettleMCP(ctx context.Context, settlement mcptools.BudgetSettlement) error {
	if h == nil || h.Attempt == nil {
		return ErrAttemptContextMissing
	}
	budget, err := requireCallBudget(h.Attempt.Budget)
	if err != nil {
		return err
	}
	return budget.SettleCall(ctx, BudgetSettlement{
		ReservationIdentity: settlement.ReservationIdentity,
		Lease:               h.Attempt.Lease,
		TraceID:             h.Attempt.Trace.ID,
		Succeeded:           settlement.Succeeded,
		Actual: &workflow.BaseBudgetActual{
			ResultChars: settlement.ResultChars, ResultBytes: settlement.ResultBytes,
		},
		UsageQuality: "reliable",
	})
}

var _ mcptools.BudgetHook = (*MCPBudgetHook)(nil)
