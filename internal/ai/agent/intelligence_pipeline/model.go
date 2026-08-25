package intelligence_pipeline

import (
	"context"

	"SentinelOps/internal/ai/models"

	"github.com/cloudwego/eino/components/model"
)

// newIntelligenceModel 创建情报 Agent 使用的 default Profile 模型。
func newIntelligenceModel(ctx context.Context) (model.ToolCallingChatModel, error) {
	return models.ChatDefault(ctx)
}
