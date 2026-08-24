package chat_pipeline

import (
	"SentinelOps/internal/ai/models"
	"context"

	"github.com/cloudwego/eino/components/model"
)

// newChatModel 为 Chat Pipeline 提供驱动 ReAct 循环的 LLM 实例。
// 此处选用 Quick 版本（无深度思考），适合多轮工具调用场景：
//   - 响应快，减少每步 ReAct 循环的等待时间
//   - 支持 Function Calling，能输出结构化 Tool Call 让框架调度工具执行
//
// 若需要复杂推理（如长链规划），可换用 ChatReasoning（开启深度思考模式）。
func newChatModel(ctx context.Context) (cm model.ToolCallingChatModel, err error) {
	cm, err = models.ChatDefault(ctx)
	if err != nil {
		return nil, err
	}
	return cm, nil
}
