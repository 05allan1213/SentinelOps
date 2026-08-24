package report_pipeline

import (
	"SentinelOps/internal/ai/models"
	"context"

	"github.com/cloudwego/eino/components/model"
)

// newReportModel 创建报告生成智能体使用的 LLM 模型实例
// 使用 Qwen3.7 Max reasoning profile 模型（深度推理版），适合需要生成结构完整、内容丰富的长篇报告场景
// reasoning Profile 开启模型思考，适合结构完整的长篇报告。
// 返回支持工具调用的聊天模型接口，用于 ReAct Agent 的推理和工具调用
func newReportModel(ctx context.Context) (model.ToolCallingChatModel, error) {
	return models.ChatReasoning(ctx)
}
