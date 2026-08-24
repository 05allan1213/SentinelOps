package risk_pipeline

import (
	"SentinelOps/internal/ai/models"
	"context"

	"github.com/cloudwego/eino/components/model"
)

// newRiskModel 创建风险评估智能体使用的 LLM 模型实例
// 使用 reasoning Profile，适合深度分析 CVE、评估攻击路径和影响范围。
// reasoning Profile 开启模型思考，适合风险评分和攻击路径分析。
// 返回支持工具调用的聊天模型接口，用于 ReAct Agent 的推理和工具调用
func newRiskModel(ctx context.Context) (model.ToolCallingChatModel, error) {
	return models.ChatReasoning(ctx)
}
