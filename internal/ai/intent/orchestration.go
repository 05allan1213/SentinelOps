// orchestration.go 编排层：使用 Eino compose.Graph 构建线性 Intent DAG（Router → Executor）。
// 本文件仅用于【标准模式（deep_thinking=false）】的意图路由流程。
// 深度思考模式下由 service/chat.ExecuteIntentDeepThink 直接调用 plan_pipeline，不使用此 DAG。
// Graph 为非单例，每次请求单独 Compile，保证节点状态隔离，避免并发请求间的状态污染。
package intent

import (
	"context"

	"github.com/cloudwego/eino/compose"
)

// Graph 节点名称常量，用于 AddLambdaNode 与 AddEdge
const (
	NodeRouter   = "Router"   // 意图识别节点，使用 LLM 对用户查询做意图识别，输出 RouterOutput
	NodeExecutor = "Executor" // 分发执行节点，消费 RouterOutput，按意图类型调用对应 SubAgent，输出 IntentOutput
)

// BuildIntentGraph 构建并编译 Intent 有向图：START → Router → Executor → END。
// 每次调用均新建 Graph 实例（非单例），编译后返回可 Invoke 的 Runnable，供 Intent.Execute 使用。
// Router 节点以 "IntentRouter" 命名，Executor 节点以 "IntentExecutor" 命名，便于 Eino 链路追踪定位。
func BuildIntentGraph(ctx context.Context) (compose.Runnable[*IntentInput, *IntentOutput], error) {
	g := compose.NewGraph[*IntentInput, *IntentOutput]()

	// Router 节点：调用 LLM 对用户查询做意图识别，将 IntentInput 转换为携带 IntentType 的 RouterOutput
	routerLambda, err := newRouterLambda(ctx)
	if err != nil {
		return nil, err
	}
	_ = g.AddLambdaNode(NodeRouter, routerLambda, compose.WithNodeName("IntentRouter"))

	// Executor 节点：消费 RouterOutput，按 IntentType 从 registry 取出对应 SubAgent 执行，输出 IntentOutput
	executorLambda := newExecutorLambda()
	_ = g.AddLambdaNode(NodeExecutor, executorLambda, compose.WithNodeName("IntentExecutor"))

	// 定义 DAG 边：入口 → Router → Executor → 出口
	_ = g.AddEdge(compose.START, NodeRouter)
	_ = g.AddEdge(NodeRouter, NodeExecutor)
	_ = g.AddEdge(NodeExecutor, compose.END)

	return g.Compile(ctx, compose.WithGraphName("IntentGraph"))
}
