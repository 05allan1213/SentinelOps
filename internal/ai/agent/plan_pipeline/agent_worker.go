// agent_worker.go 五个 Worker 工具包装器：将各专业 Agent 封装为 Supervisor 可调用的工具。
//
// 设计原则（Supervisor-Worker 架构）：
//   - Plan Agent（Supervisor）负责规划和编排
//   - 每个 Worker 工具封装对应专业 Agent 的完整能力（含 RAG pipeline）
//   - Worker 通过 SessionIdCtxKey 取当前会话历史，注入 enrichedQuery 传递上下文
//   - Worker 输出超过 maxWorkerOutputRunes 时截断，防止 Executor 上下文溢出
package plan_pipeline

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"SentinelOps/internal/ai/agent/base"
	"SentinelOps/internal/ai/agent/event_analysis_pipeline"
	"SentinelOps/internal/ai/agent/intelligence_pipeline"
	"SentinelOps/internal/ai/agent/ops_pipeline"
	"SentinelOps/internal/ai/agent/report_pipeline"
	"SentinelOps/internal/ai/agent/risk_pipeline"
	"SentinelOps/internal/ai/agent/solve_pipeline"
	"SentinelOps/internal/ai/cache"
	"SentinelOps/internal/ai/memory"
	aitrace "SentinelOps/internal/ai/trace"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/schema"
)

// SessionIdCtxKey 是在 context 中传递 sessionId 的键类型。
//
//	ExecuteDeepThink → BuildPlanAgent → adk.Runner.Query → Executor → Worker.Invoke → buildWorkerContext
//
// 保证 context.Value 能正确匹配（Go 的 context key 以类型+值双重匹配）。
type SessionIdCtxKey struct{}

// maxWorkerOutputRunes Worker 工具返回值的最大字符数（Unicode rune）。
// 超过此限制时截断，防止 Executor prompt 上下文溢出影响后续步骤推理质量。
const maxWorkerOutputRunes = 2000

// WorkerInput Worker 工具的统一输入结构。
type WorkerInput struct {
	Query string `json:"query" jsonschema:"description=要执行的任务描述，需清晰明确，越详细越好"`
}

// buildWorkerContext 从进程内存单例取当前会话历史，构建上下文前缀字符串。
// 只取最近 3 条消息，每条内容截断到 200 字，避免注入过多 token。
// 与 event_subagent.go / solve_subagent.go 使用完全相同的 GetSessionMemory 模式。
func buildWorkerContext(ctx context.Context) string {
	sessionId, ok := ctx.Value(SessionIdCtxKey{}).(string)
	if !ok || sessionId == "" {
		return ""
	}

	mem := cache.GetSessionMemory(sessionId)
	history := cache.BuildHistoryWithSummary(mem.GetRecentMessages(), mem.GetLongTermSummary())

	if len(history) == 0 {
		return ""
	}

	// 只取最近 3 条
	start := len(history) - 3
	if start < 0 {
		start = 0
	}
	recentHistory := history[start:]

	var sb strings.Builder
	for _, msg := range recentHistory {
		role := "用户"
		if msg.Role == schema.Assistant {
			role = "助手"
		}
		content := msg.Content
		runes := []rune(content)
		if len(runes) > 200 {
			content = string(runes[:200]) + "..."
		}
		if content != "" {
			sb.WriteString(fmt.Sprintf("[%s]: %s\n", role, content))
		}
	}

	historyStr := ""
	if sb.Len() > 0 {
		historyStr = "【对话上下文】\n" + sb.String() + "\n"
	}

	// 注入用户偏好（放在历史上下文之前，让 LLM 优先感知）
	userID, _ := ctx.Value(memory.UserIdCtxKey{}).(string)
	if pref := memory.GetPreference(ctx, userID); pref != nil {
		if hint := pref.FormatPromptHint(); hint != "" {
			result := hint + historyStr
			fmt.Printf("[buildWorkerContext] enrichedQuery:\n%s\n", result)
			return result
		}
	}
	return historyStr
}

// truncateWorkerOutput 截断 Worker 输出，防止 Executor 上下文溢出。
func truncateWorkerOutput(s string) string {
	runes := []rune(s)
	if len(runes) <= maxWorkerOutputRunes {
		return s
	}
	return string(runes[:maxWorkerOutputRunes]) + "\n[内容已截断，以上为关键摘要]"
}

// isolateCtx 为 Worker 工具创建完全隔离的 context。
//
// 问题根因：Worker 工具在 Executor ReAct Graph 的 ctx 链上运行，
// 内部专业 Agent（另一个 Eino Graph）执行时会向 ctx 写入自己的 compose.stateKey{}，
// 覆盖外层 Executor 的 *adk.State，导致 Executor 的 onToolEnd 回调里
// popToolGenAction → compose.ProcessState 取到类型不匹配的 state，触发 panic("impossible")。
//
// 解决方案：从 context.Background() 派生全新 ctx，彻底隔离 compose 内部 state，
// 只保留业务需要的 sessionId（用于会话历史）和 GoFrame 请求 context 值。
func isolateCtx(ctx context.Context) context.Context {
	sessionId, _ := ctx.Value(SessionIdCtxKey{}).(string)
	userID, _ := ctx.Value(memory.UserIdCtxKey{}).(string)
	isolated := context.Background()
	if sessionId != "" {
		isolated = context.WithValue(isolated, SessionIdCtxKey{}, sessionId)
	}
	if userID != "" {
		isolated = context.WithValue(isolated, memory.UserIdCtxKey{}, userID)
	}
	return isolated
}

// workerStream 通用 Worker 流式接收辅助：接收 StreamReader 并累积内容，最终截断返回。
func workerStream(stream *schema.StreamReader[*schema.Message]) (string, error) {
	defer stream.Close()
	var sb strings.Builder
	for {
		chunk, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", err
		}
		if chunk.Content != "" {
			sb.WriteString(chunk.Content)
		}
	}
	return truncateWorkerOutput(sb.String()), nil
}

// NewEventAnalysisWorker 创建事件分析 Worker 工具。
// 封装 event_analysis_pipeline（含 RAG + ReAct + 事件/订阅/报告查询工具）。
func NewEventAnalysisWorker() tool.InvokableTool {
	t, err := utils.InferOptionableTool(
		"event_analysis_agent",
		"Call the Event Analysis Agent to query, analyze and correlate security events. Handles: recent events listing, CVE analysis, severity distribution, event timeline, subscription status, threat correlation. Returns structured analysis results.",
		func(ctx context.Context, input *WorkerInput, _ ...tool.Option) (string, error) {
			contextPrefix := buildWorkerContext(ctx)
			enrichedQuery := contextPrefix + "当前任务：" + input.Query

			sessionId, _ := ctx.Value(SessionIdCtxKey{}).(string)
			mem := cache.GetSessionMemory(sessionId)
			msg := &base.UserMessage{
				ID:      sessionId,
				Query:   enrichedQuery,
				History: cache.BuildHistoryWithSummary(mem.GetRecentMessages(), mem.GetLongTermSummary()),
			}

			runner, initErr := event_analysis_pipeline.GetEventAnalysisAgent(ctx)
			if initErr != nil {
				return "", fmt.Errorf("event analysis agent init: %w", initErr)
			}

			// isolateCtx 隔离 Eino Graph state，防止内部 Agent 污染外层 Executor ReAct Graph
			spanCtx, spanID := aitrace.StartSpan(isolateCtx(ctx), aitrace.NodeTypeAgent, "EventAnalysisAgent")
			stream, streamErr := runner.Stream(spanCtx, msg)
			if streamErr != nil {
				aitrace.FinishSpan(spanCtx, spanID, streamErr, nil)
				return "", fmt.Errorf("event analysis stream: %w", streamErr)
			}
			result, err := workerStream(stream)
			aitrace.FinishSpan(spanCtx, spanID, err, nil)
			return result, err
		},
	)
	if err != nil {
		panic(err)
	}
	return t
}

// NewReportWorker 创建报告生成 Worker 工具。
// 封装 report_pipeline（含 RAG + ReAct + 报告模板/事件/报告查询及创建工具）。
func NewReportWorker() tool.InvokableTool {
	t, err := utils.InferOptionableTool(
		"report_agent",
		"Call the Report Agent to generate structured security reports (weekly/monthly/custom). Handles: creating new reports, querying existing reports, fetching report templates, summarizing event trends. Returns report content or creation confirmation.",
		func(ctx context.Context, input *WorkerInput, _ ...tool.Option) (string, error) {
			contextPrefix := buildWorkerContext(ctx)
			enrichedQuery := contextPrefix + "当前任务：" + input.Query

			sessionId, _ := ctx.Value(SessionIdCtxKey{}).(string)
			mem := cache.GetSessionMemory(sessionId)
			msg := &report_pipeline.UserMessage{
				ID:      sessionId,
				Query:   enrichedQuery,
				History: cache.BuildHistoryWithSummary(mem.GetRecentMessages(), mem.GetLongTermSummary()),
			}

			runner, initErr := report_pipeline.GetReportAgent(ctx)
			if initErr != nil {
				return "", fmt.Errorf("report agent init: %w", initErr)
			}

			// AGENT span：使内部 LLM/Tool/Retriever 节点归属到此 Agent 下，而非 Executor 的 TOOL 节点
			spanCtx, spanID := aitrace.StartSpan(isolateCtx(ctx), aitrace.NodeTypeAgent, "ReportAgent")
			stream, streamErr := runner.Stream(spanCtx, msg)
			if streamErr != nil {
				aitrace.FinishSpan(spanCtx, spanID, streamErr, nil)
				return "", fmt.Errorf("report agent stream: %w", streamErr)
			}
			result, err := workerStream(stream)
			aitrace.FinishSpan(spanCtx, spanID, err, nil)
			return result, err
		},
	)
	if err != nil {
		panic(err)
	}
	return t
}

// NewRiskAssessmentWorker 创建风险评估 Worker 工具。
// 封装 risk_pipeline（含 RAG + ReAct + 事件/文档/相似事件查询工具）。
func NewRiskAssessmentWorker() tool.InvokableTool {
	t, err := utils.InferOptionableTool(
		"risk_assessment_agent",
		"Call the Risk Assessment Agent to evaluate CVE severity, attack paths, and impact scope. Handles: CVE risk scoring, vulnerability assessment, CVSS analysis, attack surface analysis, mitigation priority ranking. Returns structured risk assessment.",
		func(ctx context.Context, input *WorkerInput, _ ...tool.Option) (string, error) {
			contextPrefix := buildWorkerContext(ctx)
			enrichedQuery := contextPrefix + "当前任务：" + input.Query

			sessionId, _ := ctx.Value(SessionIdCtxKey{}).(string)
			mem := cache.GetSessionMemory(sessionId)
			msg := &risk_pipeline.UserMessage{
				ID:      sessionId,
				Query:   enrichedQuery,
				History: cache.BuildHistoryWithSummary(mem.GetRecentMessages(), mem.GetLongTermSummary()),
			}

			runner, initErr := risk_pipeline.GetRiskAgent(ctx)
			if initErr != nil {
				return "", fmt.Errorf("risk agent init: %w", initErr)
			}

			// AGENT span：使内部 LLM/Tool/Retriever 节点归属到此 Agent 下，而非 Executor 的 TOOL 节点
			spanCtx, spanID := aitrace.StartSpan(isolateCtx(ctx), aitrace.NodeTypeAgent, "RiskAssessmentAgent")
			stream, streamErr := runner.Stream(spanCtx, msg)
			if streamErr != nil {
				aitrace.FinishSpan(spanCtx, spanID, streamErr, nil)
				return "", fmt.Errorf("risk agent stream: %w", streamErr)
			}
			result, err := workerStream(stream)
			aitrace.FinishSpan(spanCtx, spanID, err, nil)
			return result, err
		},
	)
	if err != nil {
		panic(err)
	}
	return t
}

// NewSolveWorker 创建应急响应 Worker 工具。
// 封装 solve_pipeline（含 RAG + ReAct + 相似事件检索 + 内部知识库查询）。
func NewSolveWorker() tool.InvokableTool {
	t, err := utils.InferOptionableTool(
		"solve_agent",
		"Call the Solve Agent to generate emergency response plans for specific security incidents. Handles: incident containment steps, patch recommendations, remediation procedures, recovery guidance for a single event. Returns structured three-phase response plan.",
		func(ctx context.Context, input *WorkerInput, _ ...tool.Option) (string, error) {
			contextPrefix := buildWorkerContext(ctx)
			enrichedQuery := contextPrefix + "当前任务：" + input.Query

			sessionId, _ := ctx.Value(SessionIdCtxKey{}).(string)
			mem := cache.GetSessionMemory(sessionId)
			msg := &solve_pipeline.UserMessage{
				ID:      sessionId,
				Query:   enrichedQuery,
				History: cache.BuildHistoryWithSummary(mem.GetRecentMessages(), mem.GetLongTermSummary()),
			}

			runner, initErr := solve_pipeline.GetSolveAgent(ctx)
			if initErr != nil {
				return "", fmt.Errorf("solve agent init: %w", initErr)
			}

			// AGENT span：使内部 LLM/Tool/Retriever 节点归属到此 Agent 下，而非 Executor 的 TOOL 节点
			spanCtx, spanID := aitrace.StartSpan(isolateCtx(ctx), aitrace.NodeTypeAgent, "SolveAgent")
			stream, streamErr := runner.Stream(spanCtx, msg)
			if streamErr != nil {
				aitrace.FinishSpan(spanCtx, spanID, streamErr, nil)
				return "", fmt.Errorf("solve agent stream: %w", streamErr)
			}
			result, err := workerStream(stream)
			aitrace.FinishSpan(spanCtx, spanID, err, nil)
			return result, err
		},
	)
	if err != nil {
		panic(err)
	}
	return t
}

// NewOpsWorker 创建智能运维 Worker 工具。
// 封装 ops_pipeline（事件分析 + 运维响应：封禁/通知/状态更新）。
func NewOpsWorker() tool.InvokableTool {
	t, err := utils.InferOptionableTool(
		"ops_agent",
		"Call the Ops Agent to trigger automated incident response for a specific security event. Handles: IP blocking, multi-channel alert notifications (DingTalk/WeCom/Email), event status updates. Requires event_id in the query. Returns execution result.",
		func(ctx context.Context, input *WorkerInput, _ ...tool.Option) (string, error) {
			contextPrefix := buildWorkerContext(ctx)
			enrichedQuery := contextPrefix + "当前任务：" + input.Query

			spanCtx, spanID := aitrace.StartSpan(isolateCtx(ctx), aitrace.NodeTypeAgent, "OpsAgent")
			result, err := ops_pipeline.RunWithQuery(spanCtx, enrichedQuery)
			aitrace.FinishSpan(spanCtx, spanID, err, nil)
			if err != nil {
				return "", fmt.Errorf("ops agent: %w", err)
			}
			return truncateWorkerOutput(result), nil
		},
	)
	if err != nil {
		panic(err)
	}
	return t
}

// 封装 intelligence_pipeline（含 RAG + ReAct + web_search / save_intelligence）
// 封装 intelligence_pipeline（含 RAG + ReAct + web_search / save_intelligence）。
// 适用于 Plan Agent 规划步骤中需要"联网搜索最新情报"的场景。
func NewIntelligenceWorker() tool.InvokableTool {
	t, err := utils.InferOptionableTool(
		"intelligence_agent",
		"Call the Intelligence Agent to search and analyze the latest threat intelligence from the internet. Handles: CVE details lookup, vulnerability advisories, exploit PoC status, threat actor profiling, malicious IP/domain reputation. Automatically saves findings to the local knowledge base. Returns structured threat intelligence report.",
		func(ctx context.Context, input *WorkerInput, _ ...tool.Option) (string, error) {
			contextPrefix := buildWorkerContext(ctx)
			enrichedQuery := contextPrefix + "当前任务：" + input.Query

			sessionId, _ := ctx.Value(SessionIdCtxKey{}).(string)
			mem := cache.GetSessionMemory(sessionId)
			msg := &intelligence_pipeline.UserMessage{
				ID:      sessionId,
				Query:   enrichedQuery,
				History: cache.BuildHistoryWithSummary(mem.GetRecentMessages(), mem.GetLongTermSummary()),
			}

			runner, initErr := intelligence_pipeline.GetIntelligenceAgent(ctx)
			if initErr != nil {
				return "", fmt.Errorf("intelligence agent init: %w", initErr)
			}

			// AGENT span：使内部 LLM/Tool/Retriever 节点归属到此 Agent 下，而非 Executor 的 TOOL 节点
			spanCtx, spanID := aitrace.StartSpan(isolateCtx(ctx), aitrace.NodeTypeAgent, "IntelligenceAgent")
			stream, streamErr := runner.Stream(spanCtx, msg)
			if streamErr != nil {
				aitrace.FinishSpan(spanCtx, spanID, streamErr, nil)
				return "", fmt.Errorf("intelligence agent stream: %w", streamErr)
			}
			result, err := workerStream(stream)
			aitrace.FinishSpan(spanCtx, spanID, err, nil)
			return result, err
		},
	)
	if err != nil {
		panic(err)
	}
	return t
}
