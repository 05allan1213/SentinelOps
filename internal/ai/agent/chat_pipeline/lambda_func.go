package chat_pipeline

import (
	"SentinelOps/internal/ai/prompt/memory"
	"SentinelOps/internal/ai/token"
	"context"
	"strings"
	"time"

	"github.com/cloudwego/eino/schema"
)

// newInputToRagLambda 是 RAG 支路的入口适配器（DAG 节点：InputToRag）。
// 职责：从 *UserMessage 中提取 Query 字符串，输出给 MilvusRetriever 作为向量检索的查询词。
// 类型转换：*UserMessage → string
// 之所以需要这一层，是因为 DAG 图的入口类型是 *UserMessage，
// 而 MilvusRetriever 只接受 string，两者之间需要一个类型适配节点。
func newInputToRagLambda(ctx context.Context, input *UserMessage, opts ...any) (output string, err error) {
	return input.Query, nil
}

// newInputToChatLambda 是对话历史支路的入口适配器（DAG 节点：InputToChat）。
// 职责：从 *UserMessage 中提取对话所需的全部变量，打包为 map 输出给 ChatTemplate 渲染 Prompt。
// 类型转换：*UserMessage → map[string]any
//
// 新增功能：Token 超限时自动压缩历史（语义摘要 + 保留最近 2 条，避免上下文窗口溢出）
//
// 输出 map 的 key 与 Prompt 模板占位符严格对应：
//
//	"content" → {content}  本轮用户提问，渲染为最后一条 UserMessage
//	"history" → {history}  历史消息列表（可能经过摘要压缩），由 MessagesPlaceholder 展开
//	"date"    → {date}     当前时间，注入 systemPrompt，让 LLM 感知实时时间
func newInputToChatLambda(ctx context.Context, input *UserMessage, opts ...any) (output map[string]any, err error) {
	history := input.History

	// Token 感知窗口：当历史超过阈值时，将旧对话压缩为摘要（保留最近 2 条维持当前对话连贯性）
	// 阈值 3000：systemPrompt(~500) + RAG文档(~1000) + 当前提问(~500) + 历史(3000) = 约 5000 tokens，
	// 对 Qwen 128K 上下文完全安全，同时给工具调用结果预留足够空间
	if token.EstimateMessages(history) > 3000 && len(history) >= 4 {
		// 摘要压缩：将最旧的 N-2 条消息压缩为一段简短摘要，保留最新 2 条（当前轮上下文）
		summarized, err := summarizeOldHistory(ctx, history[:len(history)-2])
		if err == nil && summarized != "" {
			// 摘要成功：用 SystemMessage 包装摘要，拼接最近 2 条原始消息
			summaryMsg := schema.SystemMessage("[历史对话摘要] " + summarized)
			history = append([]*schema.Message{summaryMsg}, history[len(history)-2:]...)
		}
		// 摘要失败时静默回退：继续使用原始历史（不影响主链路），只在日志中记录
	}

	return map[string]any{
		"content": input.Query,
		"history": history,
		"date":    time.Now().Format("2006-01-02 15:04:05"),
	}, nil
}

// summarizeOldHistory 调用 LLM 将旧对话压缩为一段不超过 200 字的摘要。
//
// 参数：
//   - ctx：上下文（含超时控制，建议 30s）
//   - oldMessages：需要压缩的旧对话（通常是最旧的 N-2 条消息）
//
// 返回值：
//   - string：摘要文本（保留关键信息、结论、用户意图），空字符串表示摘要失败
//   - error：LLM 调用错误（网络超时、模型拒绝等）
//
// 设计原理：
//   - 使用与 ReAct 相同的 Qwen3_7MaxDefault 模型（快速响应，单次调用无工具）
//   - 摘要 Prompt 引导模型提炼关键信息（不超过 200 字），丢弃无关细节
//   - 摘要失败时返回空字符串（调用方静默回退到原始历史，不影响主链路）
func summarizeOldHistory(ctx context.Context, oldMessages []*schema.Message) (string, error) {
	if len(oldMessages) == 0 {
		return "", nil
	}

	// 复用 Chat Pipeline 的 LLM 实例（Qwen3_7MaxDefault，快速响应）
	model, err := newChatModel(ctx)
	if err != nil {
		return "", err
	}

	// 拼接旧对话为文本（User: xxx\nAssistant: yyy\n...）
	var historyText strings.Builder
	for _, msg := range oldMessages {
		role := "Unknown"
		if msg.Role == schema.User {
			role = "User"
		} else if msg.Role == schema.Assistant {
			role = "Assistant"
		} else if msg.Role == schema.System {
			role = "System"
		}
		historyText.WriteString(role + ": " + msg.Content + "\n")
	}

	// 摘要 Prompt：System 注入角色上下文（安全领域记忆压缩器），User 提供具体压缩指令
	msgs := []*schema.Message{
		schema.SystemMessage(memory.SummarySystem),
		schema.UserMessage(memory.ChatInlineCompress + historyText.String()),
	}
	out, err := model.Generate(ctx, msgs)
	if err != nil {
		return "", err
	}

	return out.Content, nil
}
