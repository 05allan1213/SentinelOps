package ops_pipeline

import (
	"context"
	"fmt"
	"sync"

	"SentinelOps/internal/ai/agent"
	"SentinelOps/internal/ai/agent/base"
	"SentinelOps/internal/ai/models"
	"SentinelOps/internal/ai/prompt/agents"
	"SentinelOps/internal/ai/runtime"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
)

const opsMaxIterations = 20

var opsTools = []string{
	"query_events", "trigger_ops", "update_event_status", "block_ip",
	"notify_dingtalk", "notify_wecom", "notify_email", "get_current_time",
}

// GetOpsAgent 运维 Agent 单例：基于事件分析结论执行通知/封禁/状态更新。
var GetOpsAgent = agent.NewSingletonAgent(agent.AgentConfig{
	GraphName:    "OpsAgent",
	SystemPrompt: agents.Ops,
	ModelFactory: models.ChatDefault,
	MaxStep:      opsMaxIterations,
	ToolNames:    opsTools,
})

// NewOpsAgent 构建迁移后的运维规划 ChatModelAgent；L1/L2 叶子 Tool 由 RuntimeHandler fail-closed。
func NewOpsAgent(ctx context.Context, m model.ToolCallingChatModel, handler *runtime.RuntimeHandler) (adk.Agent, error) {
	return base.NewSpecialistAgent(ctx, base.SpecialistConfig{
		Name: "OpsAgent", Description: "Call the Ops Agent to trigger automated incident response for a specific security event. Handles: IP blocking, multi-channel alert notifications (DingTalk/WeCom/Email), event status updates. Requires event_id in the query. Returns execution result.",
		Instruction: agents.Ops, Model: m, RuntimeHandler: handler, MaxIterations: opsMaxIterations,
		RetrievalOptions: base.RetrievalOptions{RewriteEnabled: false, SplitEnabled: false},
		ToolNames:        opsTools,
	})
}

var (
	opsDurableOnce  sync.Once
	opsDurableAgent adk.Agent
	opsDurableErr   error
)

// GetDurableOpsAgent 懒构建 ADK 运维 Agent；旧 ExecuteRun 路径和 P19 接线均保持独立。
func GetDurableOpsAgent(ctx context.Context) (adk.Agent, error) {
	opsDurableOnce.Do(func() {
		m, err := newOpsModel(ctx)
		if err != nil {
			opsDurableErr = err
			return
		}
		opsDurableAgent, opsDurableErr = NewOpsAgent(ctx, m, runtime.NewRuntimeHandler())
	})
	if opsDurableErr != nil {
		return nil, fmt.Errorf("ops ADK agent: %w", opsDurableErr)
	}
	return opsDurableAgent, nil
}
