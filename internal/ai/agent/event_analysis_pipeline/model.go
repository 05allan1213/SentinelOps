package event_analysis_pipeline

import (
	"SentinelOps/internal/ai/models"
	"context"

	"github.com/cloudwego/eino/components/model"
)

// newEventModel 创建事件分析智能体使用的 LLM 模型实例
// 使用 Qwen3.7 Max default profile 模型，低延迟特性适合实时事件分析场景
// 返回支持工具调用的聊天模型接口，用于 ReAct Agent 的推理和工具调用
func newEventModel(ctx context.Context) (model.ToolCallingChatModel, error) {
	return models.ChatDefault(ctx)
}
