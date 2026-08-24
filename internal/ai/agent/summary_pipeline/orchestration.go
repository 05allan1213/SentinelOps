package summary_pipeline

import (
	"context"
	"sync"

	"github.com/cloudwego/eino/compose"
)

var (
	summaryRunner  compose.Runnable[*SummaryInput, *SummaryOutput]
	summaryOnce    sync.Once
	summaryInitErr error
)

// GetSummaryAgent 返回全局缓存的摘要 Agent（懒初始化，线程安全）
//
// 设计模式：单例模式 + 懒加载
//   - 进程生命周期内只执行一次 DAG 编译（g.Compile 包含拓扑排序、类型校验、执行计划生成）
//   - 所有并发请求复用同一个 Runnable 实例（Eino 框架保证 Invoke 各自创建独立执行上下文）
//
// 数据流（4 节点流水线）：
//
//	SummaryInput → FormatMessages → SummaryTemplate → SummaryModel → ExtractKeyFacts → SummaryOutput
//
// 性能优化：
//   - DAG 编译耗时约 50-100ms，懒加载避免启动时阻塞
//   - 单例模式避免重复编译，节省内存和 CPU
func GetSummaryAgent(ctx context.Context) (compose.Runnable[*SummaryInput, *SummaryOutput], error) {
	summaryOnce.Do(func() {
		summaryRunner, summaryInitErr = buildSummaryAgent(context.Background())
	})
	return summaryRunner, summaryInitErr
}

// buildSummaryAgent 构建摘要 Agent 并编译为可执行 Runnable（内部函数，仅被 sync.Once 调用一次）
//
// DAG 架构设计：
//
//	节点 1：FormatMessages  - 格式化消息列表为文本（Lambda）
//	节点 2：SummaryTemplate - 渲染摘要 Prompt（ChatTemplate）
//	节点 3：SummaryModel    - 调用 LLM 生成摘要（ChatModel）
//	节点 4：ExtractSummary  - 提取摘要（Lambda）
//
// 类型流转：
//
//	*SummaryInput → map[string]any → []*schema.Message → *schema.Message → *SummaryOutput
func buildSummaryAgent(ctx context.Context) (r compose.Runnable[*SummaryInput, *SummaryOutput], err error) {
	// 节点 ID 常量，用于 AddNode / AddEdge 时引用，避免字符串拼写错误
	const (
		FormatMessages  = "FormatMessages"
		SummaryTemplate = "SummaryTemplate"
		SummaryModel    = "SummaryModel"
		ExtractSummary  = "ExtractSummary"
	)

	// compose.NewGraph 创建一个泛型有向无环图（DAG）
	// 泛型参数 [*SummaryInput, *SummaryOutput] 声明了整张图的入口类型和出口类型
	// 图本身只是拓扑描述（节点 + 边），不执行任何计算，执行发生在 Compile 之后的 Invoke 阶段
	g := compose.NewGraph[*SummaryInput, *SummaryOutput]()

	// ── 节点注册 ─────────────────────────────────────────────────────────────

	// 节点 1：格式化消息列表为文本
	// AddLambdaNode 将一个普通函数包装为图节点
	// InvokableLambdaWithOption 允许函数签名携带 opts 可变参数，框架在调用时透传 Callback 等选项
	// formatMessagesLambda 实现：[]*schema.Message → string（纯数据转换，无 IO）
	// 输入：*SummaryInput{SessionID, Messages}
	// 输出：map[string]any{"conversation": string}
	_ = g.AddLambdaNode(FormatMessages, compose.InvokableLambdaWithOption(formatMessagesLambda),
		compose.WithNodeName("FormatMessages"))

	// 节点 2：渲染摘要 Prompt
	// AddChatTemplateNode 注册一个 Prompt 渲染节点
	// 底层使用 FString 格式，将占位符 {conversation} 替换为实际对话文本
	// 渲染完成后输出标准 OpenAI Messages 格式：[SystemMessage, UserMessage]
	// 输入：map[string]any{"conversation": string}
	// 输出：[]*schema.Message（System + User）
	summaryTemplate, err := newSummaryTemplate(ctx)
	if err != nil {
		return nil, err
	}
	_ = g.AddChatTemplateNode(SummaryTemplate, summaryTemplate)

	// 节点 3：调用 LLM 生成摘要
	// AddChatModelNode 注册一个 LLM 调用节点
	// 底层调用 Routing 选择的 default Profile 模型。
	// 输入：[]*schema.Message（System + User）
	// 输出：*schema.Message（LLM 的回复，包含总结和关键信息）
	summaryModel, err := newSummaryModel(ctx)
	if err != nil {
		return nil, err
	}
	_ = g.AddChatModelNode(SummaryModel, summaryModel)

	// 节点 4：提取摘要
	// AddLambdaNode 注册一个解析函数
	// extractSummaryLambda 实现：解析 LLM 输出，提取结构化数据
	// 输入：*schema.Message（LLM 的回复）
	// 输出：*SummaryOutput{Summary}
	_ = g.AddLambdaNode(ExtractSummary, compose.InvokableLambdaWithOption(extractSummaryLambda),
		compose.WithNodeName("ExtractSummary"))

	// ── 边定义（数据流方向） ──────────────────────────────────────────────────
	// AddEdge(A, B) 表示"A 的输出作为 B 的输入"，框架据此在运行时调度节点执行顺序
	// 当前 DAG 是线性流水线（无并行分支），执行顺序严格按照边的定义

	_ = g.AddEdge(compose.START, FormatMessages)
	_ = g.AddEdge(FormatMessages, SummaryTemplate)
	_ = g.AddEdge(SummaryTemplate, SummaryModel)
	_ = g.AddEdge(SummaryModel, ExtractSummary)
	_ = g.AddEdge(ExtractSummary, compose.END)

	// ── 编译图 ────────────────────────────────────────────────────────────────
	// g.Compile 做两件事：
	//   1. 拓扑排序校验：检测环路、孤立节点、类型不匹配等问题，在启动时而非运行时暴露错误
	//   2. 生成执行计划：根据边的依赖关系确定节点调度顺序
	//
	// WithGraphName("SummaryAgent") 的作用：
	//   为 DAG 命名，用于日志追踪和性能监控
	r, err = g.Compile(ctx, compose.WithGraphName("SummaryAgent"))
	if err != nil {
		return nil, err
	}
	return r, nil
}
