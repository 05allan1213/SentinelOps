package solve_pipeline

import (
	"SentinelOps/internal/ai/models"
	"context"

	"github.com/cloudwego/eino/components/model"
)

// newSolveModel 创建解决方案生成 Agent 使用的 LLM 模型实例。
// 使用 Qwen3.7 Max reasoning profile（深度推理版）：单事件分析需要多步推导攻击路径和修复路径，
// reasoning Profile 开启模型思考，适合多步处置方案。
func newSolveModel(ctx context.Context) (model.ToolCallingChatModel, error) {
	return models.ChatReasoning(ctx)
}
