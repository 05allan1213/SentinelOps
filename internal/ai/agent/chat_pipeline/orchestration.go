package chat_pipeline

import (
	"context"
	"sync"

	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

var (
	chatRunner  compose.Runnable[*UserMessage, *schema.Message]
	chatOnce    sync.Once
	chatInitErr error
)

// GetChatAgent 返回全局缓存的 Chat Agent（懒初始化，线程安全）。
//
// 进程生命周期内只执行一次 DAG 编译（g.Compile 包含拓扑排序、类型校验、执行计划生成），
// 所有并发请求复用同一个 Runnable 实例（Eino 框架保证 Invoke/Stream 各自创建独立执行上下文）。
//
// 数据流（两路并行，fan-in 后驱动 ReAct）：
//
//	               ┌─► InputToRag ──► MilvusRetriever ──┐
//	UserMessage ───┤                                     ├──► ChatTemplate ──► ReactAgent ──► END
//	               └─► InputToChat ──────────────────────┘
func GetChatAgent(ctx context.Context) (compose.Runnable[*UserMessage, *schema.Message], error) {
	chatOnce.Do(func() {
		chatRunner, chatInitErr = buildChatAgent(context.Background())
	})
	return chatRunner, chatInitErr
}

// buildChatAgent 构建 RAG + ReAct 对话 Agent 并编译为可执行 Runnable（内部函数，仅被 sync.Once 调用一次）。
func buildChatAgent(ctx context.Context) (r compose.Runnable[*UserMessage, *schema.Message], err error) {
	// 节点 ID 常量，用于 AddNode / AddEdge 时引用，避免字符串拼写错误
	const (
		InputToRag      = "InputToRag"
		ChatTemplate    = "ChatTemplate"
		ReactAgent      = "ReactAgent"
		MilvusRetriever = "MilvusRetriever"
		InputToChat     = "InputToChat"
	)

	// compose.NewGraph 创建一个泛型有向无环图（DAG）。
	// 泛型参数 [*UserMessage, *schema.Message] 声明了整张图的入口类型和出口类型。
	// 图本身只是拓扑描述（节点 + 边），不执行任何计算，执行发生在 Compile 之后的 Invoke/Stream 阶段。
	g := compose.NewGraph[*UserMessage, *schema.Message]()

	// ── 节点注册 ─────────────────────────────────────────────────────────────

	// AddLambdaNode 将一个普通函数包装为图节点。
	// InvokableLambdaWithOption 允许函数签名携带 opts 可变参数，框架在调用时透传 Callback 等选项。
	// newInputToRagLambda 实现：input.Query → string（纯数据提取，无 IO）。
	// 这一步的作用是做类型适配：图的入口是 *UserMessage，而 MilvusRetriever 只接收 string 查询词。
	_ = g.AddLambdaNode(InputToRag, compose.InvokableLambdaWithOption(newInputToRagLambda), compose.WithNodeName("UserMessageToRag"))

	// AddChatTemplateNode 注册一个 Prompt 渲染节点。
	// 底层使用 FString 格式，将占位符替换为实际值：
	//   {content}   ← InputToChat 输出的用户提问文本
	//   {history}   ← MessagesPlaceholder，直接展开为 []*schema.Message 消息列表
	//   {documents} ← MilvusRetriever 检索到的知识文档（由 OutputKey 命名后 fan-in 合并）
	//   {date}      ← InputToChat 注入的当前时间字符串
	// 渲染完成后输出标准 OpenAI Messages 格式：[SystemMessage, ...history, UserMessage]
	// 这是送入 LLM 的完整上下文，LLM 只认这个列表，不感知之前的任何代码逻辑。
	chatTemplateKeyOfChatTemplate, err := newChatTemplate(ctx)
	if err != nil {
		return nil, err
	}
	_ = g.AddChatTemplateNode(ChatTemplate, chatTemplateKeyOfChatTemplate)

	// AddLambdaNode 注册 ReAct Agent 节点。
	// ReAct（Reasoning + Acting）是一种让 LLM 交替执行"推理"和"工具调用"的范式：
	//   1. Thought  ── LLM 输出思考过程（不可见于用户）
	//   2. Action   ── LLM 决定调用哪个工具及参数（Tool Call）
	//   3. Observation ── 工具执行后将结果追加到 Messages，LLM 继续下一轮推理
	//   4. 循环直到 LLM 输出 Final Answer（不再调用工具），最多 MaxStep=25 轮
	// 当前注册的工具集：MySQL CRUD、当前时间、内部文档检索、事件/订阅/报告查询
	// compose.AnyLambda 将 ins.Generate（同步）和 ins.Stream（流式）同时封装，
	// 框架根据调用方使用 Invoke 还是 Stream 自动选择对应实现。
	reactAgentKeyOfLambda, err := newReactAgentLambda(ctx)
	if err != nil {
		return nil, err
	}
	_ = g.AddLambdaNode(ReactAgent, reactAgentKeyOfLambda, compose.WithNodeName("ReActAgent"))

	// AddRetrieverNode 注册向量检索节点。
	// 底层执行流程：
	//   1. 用豆包 Embedding 模型将查询字符串转为高维向量（float32 数组）
	//   2. 在 Milvus 集合的 "vector" 字段上执行 ANN（近似最近邻）搜索，TopK=1
	//   3. 返回余弦相似度最高的 1 条文档（含 id、content、metadata 字段）
	// WithOutputKey("documents")：
	//   Eino 在 fan-in（多条并行支路的数据汇聚到同一个后继节点）时将各前驱节点的输出按 key 合并为一个 map 传给后继节点。
	//   此处命名为 "documents"，ChatTemplate 渲染时即可通过 {documents} 占位符读取检索结果。
	milvusRetrieverKeyOfRetriever, err := newRetriever(ctx)
	if err != nil {
		return nil, err
	}
	_ = g.AddRetrieverNode(MilvusRetriever, milvusRetrieverKeyOfRetriever, compose.WithOutputKey("documents"))

	// newInputToChatLambda 将 *UserMessage 转换为 map[string]any：
	//   "content" → 用户本轮提问，填充 Prompt 的 {content}
	//   "history" → 历史消息列表（[]*schema.Message），展开到 {history} 占位符
	//   "date"    → 当前时间字符串，填充 Prompt 的 {date}，供 LLM 感知当前时间
	// 输出 map 的 key 必须与 ChatTemplate 模板中的占位符名称完全一致，否则渲染时找不到变量。
	_ = g.AddLambdaNode(InputToChat, compose.InvokableLambdaWithOption(newInputToChatLambda), compose.WithNodeName("UserMessageToChat"))

	// ── 边定义（数据流方向） ──────────────────────────────────────────────────
	// AddEdge(A, B) 表示"A 的输出作为 B 的输入"，框架据此在运行时调度节点执行顺序。
	// 同一节点有多条出边 → 并行 fan-out；同一节点有多条入边 → 需 fan-in 等待（由触发模式决定）。

	// RAG 支路：提取查询词 → 向量检索 → 文档结果送入 Prompt 渲染
	_ = g.AddEdge(compose.START, InputToRag)
	_ = g.AddEdge(InputToRag, MilvusRetriever)
	_ = g.AddEdge(MilvusRetriever, ChatTemplate)

	// 对话历史支路：提取 Query/History/时间 → 送入 Prompt 渲染（与 RAG 支路并行执行）
	// START 同时向 InputToRag 和 InputToChat 发出信号，两条支路在不同 goroutine 中并发运行。
	_ = g.AddEdge(compose.START, InputToChat)
	_ = g.AddEdge(InputToChat, ChatTemplate)

	// 主干：Prompt 渲染完成 → 驱动 ReAct 推理 → 输出最终回答
	_ = g.AddEdge(ChatTemplate, ReactAgent)
	_ = g.AddEdge(ReactAgent, compose.END)

	// ── 编译图 ────────────────────────────────────────────────────────────────
	// g.Compile 做两件事：
	//   1. 拓扑排序校验：检测环路、孤立节点、类型不匹配等问题，在启动时而非运行时暴露错误。
	//   2. 生成执行计划：根据边的依赖关系确定节点调度顺序，并行节点用 goroutine 并发驱动。
	//
	// WithNodeTriggerMode(AllPredecessor)：
	//   节点触发策略。ChatTemplate 有两个前驱（MilvusRetriever 和 InputToChat），
	//   AllPredecessor 要求所有前驱都输出后才触发该节点，相当于 fan-in 屏障（barrier，即多路汇聚点，等所有支路都到齐再继续）。
	//   若使用默认的 AnyPredecessor，任意一个前驱完成就会触发，另一路数据将被丢弃，
	//   导致 {documents} 或 {history} 缺失，Prompt 渲染出错。
	r, err = g.Compile(ctx, compose.WithGraphName("ChatAgent"), compose.WithNodeTriggerMode(compose.AllPredecessor))
	if err != nil {
		return nil, err
	}
	return r, err
}
