package ops_pipeline

import (
	"context"

	"SentinelOps/internal/ai/models"

	"github.com/cloudwego/eino/components/model"
)

// newOpsModel 创建运维 Agent 使用的 default Profile 模型。
func newOpsModel(ctx context.Context) (model.ToolCallingChatModel, error) {
	return models.ChatDefault(ctx)
}
