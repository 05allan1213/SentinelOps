package summary_pipeline

import (
	"SentinelOps/internal/ai/models"
	"context"

	"github.com/cloudwego/eino/components/model"
)

// newSummaryModel 为摘要 Agent 提供 LLM 实例
//
// 模型选择：Qwen3.7 Max default profile
//   - 响应快：首 Token 延迟低，适合异步摘要场景（不阻塞主链路）
//   - 成本低：相比 Think 版本，成本降低 50%，适合高频调用
//   - 能力足：对于摘要任务，Quick 版本的推理能力已足够
func newSummaryModel(ctx context.Context) (cm model.ToolCallingChatModel, err error) {
	cm, err = models.ChatDefault(ctx)
	if err != nil {
		return nil, err
	}
	return cm, nil
}
