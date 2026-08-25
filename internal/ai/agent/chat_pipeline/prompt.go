package chat_pipeline

import (
	"context"

	"SentinelOps/internal/ai/evidence"
	"SentinelOps/internal/ai/prompt/agents"

	"github.com/cloudwego/eino/components/prompt"
	"github.com/cloudwego/eino/schema"
)

// ChatTemplateConfig 定义 Prompt 模板的渲染配置。
//   - FormatType：占位符语法，FString 表示使用 {变量名} 格式。
//   - Templates：消息模板列表，按顺序组成最终送给 LLM 的 Messages 数组。
type ChatTemplateConfig struct {
	FormatType schema.FormatType
	Templates  []schema.MessagesTemplate
}

// newChatTemplate 构建 Prompt 渲染节点，定义送给 LLM 的完整消息结构。
//
// 渲染后 LLM 收到的 Messages 顺序如下：
//
//	[0] SystemMessage(systemPrompt) ── role:system，注入角色设定、工具使用规则和当前时间（{date}）
//	[1] UserMessage({documents})   ── role:user，不受信任 Evidence 数据边界
//	[2] ...history（展开）          ── role:user/assistant 交替，多轮对话历史，让模型感知上下文
//	[3] UserMessage("{content}")    ── role:user，本轮用户提问
//
// 占位符替换由 InputToChat Lambda 提供的 map 驱动：
//
//	{content}   ← input.Query        本轮提问文本
//	{history}   ← input.History      历史消息列表（MessagesPlaceholder 直接展开为多条 Message）
//	{date}      ← time.Now()         当前时间，嵌在 systemPrompt 内
//	{documents} ← EvidencePrompt    RAG 检索结果，作为不受信任 User 数据消息
//
// 注意：{date} 位于 systemPrompt 字符串内部；{documents} 由独立 User 数据消息承载，不进入 System 边界；
// {history} 是 MessagesPlaceholder，不做字符串替换，而是将整个 []*schema.Message 列表展开插入。
func newChatTemplate(ctx context.Context) (ctp prompt.ChatTemplate, err error) {
	config := &ChatTemplateConfig{
		FormatType: schema.FString, // 占位符语法：{变量名}
		Templates: []schema.MessagesTemplate{
			// role:system ── 模型行为约束和当前时间；Evidence 单独作为 User 数据消息注入
			schema.SystemMessage(evidence.SafeInstruction(agents.Chat)),
			// role:user ── 检索结果是不受信任的数据，不能提升为 System/Developer 指令
			schema.UserMessage("{documents}"),
			// role:user/assistant ── 展开历史消息列表，false 表示历史为空时不报错
			schema.MessagesPlaceholder("history", false),
			// role:user ── 本轮用户提问，始终放在消息列表末尾，符合 LLM 对话惯例
			schema.UserMessage("{content}"),
		},
	}
	// prompt.FromMessages 将模板列表编译为 ChatTemplate 实例，
	// 后续 DAG 每次运行时调用其 Format 方法，传入变量 map 完成渲染。
	ctp = prompt.FromMessages(config.FormatType, config.Templates...)
	return ctp, nil
}
