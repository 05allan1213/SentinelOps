// Package intent executor.go 执行器节点：接收 RouterOutput，按 IntentType 分发到对应 SubAgent。
//
// 执行逻辑：
//
//  1. 从注册表获取 IntentType 对应的 SubAgent 并执行。
//  2. SubAgent 未注册时降级为 IntentChat（兜底，补充 Router 层的第一重降级）。
//
// 注：plan 意图已从标准路由中移除（不在 Router prompt 中声明）。深度思考模式下
// 由 chatsvc.ExecuteIntentDeepThink 直接调用 plan_pipeline，不经过此 Executor。
package intent

import (
	"context"
	"fmt"
	"sync/atomic"

	"SentinelOps/internal/ai/intent/core"
	"SentinelOps/internal/ai/memory"
	aitrace "SentinelOps/internal/ai/trace"

	"github.com/cloudwego/eino/compose"
	"github.com/gogf/gf/v2/frame/g"
)

// taskIDCounter 全局任务 ID 自增计数器。
// 使用 atomic.Int64 保证并发安全：多个并发请求同时调用 generateTaskID 时不会产生竞态。
var taskIDCounter atomic.Int64

// newExecutorLambda 构建 Executor Lambda 节点。
//
// 执行流程：
//
//	RouterOutput → 从注册表获取 SubAgent
//	                ↓
//	           SubAgent 存在？
//	             ├── YES → 构建 Task → agent.Execute → 封装 IntentOutput
//	             └── NO  → 降级为 IntentChat SubAgent → 同上
func newExecutorLambda() *compose.Lambda {
	lambda := compose.InvokableLambda(func(ctx context.Context, input *RouterOutput) (*IntentOutput, error) {

		// ── 从注册表获取 SubAgent ──────────────────────────────────────────
		// 第二重兜底：Router 已做第一重降级（识别失败 / 置信度不足 → IntentChat），
		// 此处再做第二重降级（未注册 → IntentChat），确保 Executor 永远有可用 SubAgent。
		agent := core.GetSubAgent(input.IntentType)
		if agent == nil {
			// 注册表中不存在此意图类型的 SubAgent（如新增意图但忘记注册适配器）
			g.Log().Warningf(ctx, "[Executor] SubAgent 未注册，降级为 chat | intent=%s", input.IntentType)
			agent = core.GetSubAgent(IntentChat)
		}

		// 构建任务，携带全局唯一 ID 便于日志追踪（格式：task_<自增序号>）
		task := &Task{
			ID:        generateTaskID(),
			Query:     input.Input.Query,
			Intent:    input.IntentType,
			SessionId: input.Input.SessionId,
		}

		// 将用户偏好提示前置注入 Query，使标准模式 SubAgent 也能感知偏好
		userID, _ := ctx.Value(memory.UserIdCtxKey{}).(string)
		if userID != "" {
			if pref := memory.GetPreference(ctx, userID); pref != nil {
				if hint := pref.FormatPromptHint(); hint != "" {
					task.Query = hint + task.Query
					g.Log().Debugf(ctx, "[Executor] 注入用户偏好 | user=%s | hint=%q", userID, hint)
				}
			}
		}

		g.Log().Infof(ctx, "[Executor] 分发 SubAgent | intent=%s | task=%s | session=%s",
			input.IntentType, task.ID, task.SessionId)

		// ── AGENT span 埋点：包裹 SubAgent 执行，使 LLM/TOOL/RETRIEVER/DB/CACHE 子节点挂载到 AGENT 下 ──
		agentName := resolveAgentName(input.IntentType)
		spanCtx, spanID := aitrace.StartSpan(ctx, aitrace.NodeTypeAgent, agentName)

		// 用 WithoutCancel 隔离 Agent 执行，避免 HTTP 客户端断连时取消正在进行的 LLM 流式请求。
		// Go HTTP server 在客户端断连时自动取消 request context，若不隔离则会导致
		// LLM streaming 中途收到 context canceled，使整个 ReAct 循环失败。
		// WithoutCancel 保留所有 context value（trace、session 等），仅移除取消传播。
		agentCtx := context.WithoutCancel(spanCtx)

		// 执行子 Agent（内部通过 Callback 流式回传结果）
		// Execute 内部的错误不向 Graph 上抛，而是封装在 IntentOutput.Error，
		// 保证 DAG 始终正常结束（不会因 SubAgent 错误导致整个 DAG 异常终止）
		result, err := agent.Execute(agentCtx, task, input.Input.Callback)

		aitrace.FinishSpan(spanCtx, spanID, err, map[string]any{
			"intent":     string(input.IntentType),
			"task_id":    task.ID,
			"session_id": task.SessionId,
		})

		var out *IntentOutput
		if err != nil {
			g.Log().Errorf(ctx, "[Executor] SubAgent 执行失败 | intent=%s | task=%s | err=%v",
				input.IntentType, task.ID, err)
			// 执行失败：保留任务 ID 和意图类型，Content 置空，Error 记录原因
			// 调用方（chatsvc.ExecuteIntent）通过检查 IntentOutput.Error 决定如何处理
			out = &IntentOutput{
				TaskID:  task.ID,
				Intent:  input.IntentType,
				Content: "",
				Error:   err,
			}
		} else {
			g.Log().Infof(ctx, "[Executor] SubAgent 执行完成 | intent=%s | task=%s | contentLen=%d",
				input.IntentType, task.ID, len(result.Content))
			// 执行成功：透传子 Agent 返回的完整结果（TaskID/Intent/Content/Error 均由 SubAgent 填充）
			out = &IntentOutput{
				TaskID:  result.TaskID,
				Intent:  result.Intent,
				Content: result.Content,
				Error:   result.Error,
			}
		}
		return out, nil
	})
	return lambda
}

// generateTaskID 生成唯一任务 ID，格式为 task_<自增序号>。
//
// 用途：
//   - 在日志中关联同一请求的 Router 和 Executor 步骤
//   - SubAgent 执行结果中返回此 ID，调用方可据此追踪请求
//
// 实现：atomic.Int64.Add(1) 是原子操作，多 goroutine 并发调用不会产生重复 ID。
func generateTaskID() string {
	return fmt.Sprintf("task_%d", taskIDCounter.Add(1))
}

// resolveAgentName 根据意图类型返回语义化 Agent 名称（用于 trace 节点显示）
func resolveAgentName(intentType IntentType) string {
	switch intentType {
	case IntentChat:
		return "ChatAgent"
	case IntentEvent:
		return "EventAnalysisAgent"
	case IntentReport:
		return "ReportAgent"
	case IntentRisk:
		return "RiskAssessmentAgent"
	case IntentSolve:
		return "SolveAgent"
	case IntentIntel:
		return "IntelligenceAgent"
	case IntentOps:
		return "OpsAgent"
	default:
		return "Agent(" + string(intentType) + ")"
	}
}
