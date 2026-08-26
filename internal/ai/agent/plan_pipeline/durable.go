package plan_pipeline

import (
	"context"
	"fmt"

	airuntime "SentinelOps/internal/ai/runtime"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/prebuilt/planexecute"
)

// NewDurablePlanAgent 复用官方 Plan/Executor/Replanner，并把唯一 RuntimeHandler
// 同时挂到 Executor 的 AgentTool 与内层专业 ChatModelAgent。
func NewDurablePlanAgent(ctx context.Context, handler *airuntime.RuntimeHandler) (adk.Agent, error) {
	if handler == nil {
		return nil, fmt.Errorf("durable Plan RuntimeHandler is required")
	}
	planner, err := NewPlannerWithRuntimeHandler(ctx, handler)
	if err != nil {
		return nil, fmt.Errorf("build durable Planner: %w", err)
	}
	executor, err := NewExecutorWithRuntimeHandler(ctx, handler)
	if err != nil {
		return nil, fmt.Errorf("build durable Executor: %w", err)
	}
	replanner, err := NewRePlanAgentWithRuntimeHandler(ctx, handler)
	if err != nil {
		return nil, fmt.Errorf("build durable Replanner: %w", err)
	}
	return planexecute.New(ctx, &planexecute.Config{
		Planner: planner, Executor: executor, Replanner: replanner, MaxIterations: 20,
	})
}
