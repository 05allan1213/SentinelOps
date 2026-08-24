package plan_pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/prebuilt/planexecute"
	"github.com/cloudwego/eino/schema"
	"github.com/gogf/gf/v2/frame/g"
)

// WorkflowRecorder 定义 Plan Pipeline 可选的工作流记录器。
// 通过 context 注入，避免 plan_pipeline 依赖 controller 或具体持久化实现。
type WorkflowRecorder interface {
	Checkpoint(ctx context.Context, stepName string, values map[string]any) error
	Event(ctx context.Context, eventType, payload string) error
}

type workflowRecorderCtxKey struct{}

// WithWorkflowRecorder 将工作流记录器注入 context。
func WithWorkflowRecorder(ctx context.Context, recorder WorkflowRecorder) context.Context {
	if recorder == nil {
		return ctx
	}
	return context.WithValue(ctx, workflowRecorderCtxKey{}, recorder)
}

func workflowRecorderFromContext(ctx context.Context) WorkflowRecorder {
	recorder, _ := ctx.Value(workflowRecorderCtxKey{}).(WorkflowRecorder)
	return recorder
}

// PlanStepMsg 是推送给前端的结构化步骤事件，JSON 序列化后通过 SSE plan_step 事件发送。
// 前端按 type 字段分类渲染：规划步骤清单、执行推理摘要、Worker 结果摘要。
type PlanStepMsg struct {
	Type    string   `json:"type"`              // "plan_steps" | "exec" | "tool_result"
	Steps   []string `json:"steps,omitempty"`   // plan_steps：Planner 生成的步骤列表
	Content string   `json:"content,omitempty"` // exec/tool_result：内容或摘要
	Name    string   `json:"name,omitempty"`    // tool_result：工具友好名称
}

// uuidPattern 匹配标准 UUID 格式（xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx）。
var uuidPattern = regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)

// stripUUIDs 从文本中移除所有 UUID，并清理多余空白，避免技术性 ID 暴露给用户。
func stripUUIDs(s string) string {
	// 移除 UUID 及其紧邻的空白
	cleaned := uuidPattern.ReplaceAllString(s, "")
	// 清理连续空白（包括换行后残留的空行）
	lines := strings.Split(cleaned, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.Join(strings.Fields(line), " ")
		out = append(out, line)
	}
	// 合并连续空行为最多一个空行
	result := strings.Join(out, "\n")
	for strings.Contains(result, "\n\n\n") {
		result = strings.ReplaceAll(result, "\n\n\n", "\n\n")
	}
	return strings.TrimSpace(result)
}

// workerFriendlyName 将内部工具名映射为用户可读的中文名称。
func workerFriendlyName(toolName string) string {
	switch toolName {
	case "event_analysis_agent":
		return "事件分析"
	case "report_agent":
		return "报告生成"
	case "risk_assessment_agent":
		return "风险评估"
	case "solve_agent":
		return "应急响应"
	case "intelligence_agent":
		return "威胁情报"
	case "query_internal_docs":
		return "知识库检索"
	case "get_current_time":
		return "获取时间"
	default:
		if toolName != "" {
			return toolName
		}
		return "工具执行"
	}
}

// marshalStepMsg 序列化步骤事件为 JSON 字符串，序列化失败时返回内容原文。
func marshalStepMsg(msg PlanStepMsg) string {
	b, err := json.Marshal(msg)
	if err != nil {
		return msg.Content
	}
	return string(b)
}

func checkpointWorkflow(ctx context.Context, stepName string, values map[string]any) {
	recorder := workflowRecorderFromContext(ctx)
	if recorder == nil {
		return
	}
	if err := recorder.Checkpoint(ctx, stepName, values); err != nil {
		g.Log().Warningf(ctx, "[PlanPipeline] 写入工作流检查点失败 | step=%s err=%v", stepName, err)
	}
}

func summarizeCheckpointContent(content string) string {
	content = strings.TrimSpace(content)
	runes := []rune(content)
	if len(runes) > 500 {
		return string(runes[:500]) + "…"
	}
	return content
}

// BuildPlanAgent 驱动完整的 Plan-Execute-Replan 循环，通过两个回调实时流式推送输出。
//
// ── 底层 Agent 组合结构（planexecute.New 内部实现）──────────────────────────
//
//	SequentialAgent "plan_pipeline"
//	  ├─ Planner          （顺序执行，运行一次，生成初始步骤清单）
//	  └─ LoopAgent "execute_replan"  （循环执行，最多 MaxIterations 轮）
//	       ├─ Executor     （每轮执行 Plan.FirstStep，结果写入 Session）
//	       └─ Replanner    （读取执行结果，调用 PlanTool 继续 或 RespondTool 终止）
//
// ── 回调分工 ──────────────────────────────────────────────────────────────────
//
//	onStep  ：中间步骤输出（Planner 规划、Executor 执行过程），推送到规划过程块展示
//	onFinal ：最终答案（Replanner 终止时生成的最终回复），推送到主内容气泡展示
//
// ── 实现原理：事件分类 ────────────────────────────────────────────────────────
//
//	按消息角色和内容分类处理：
//	  - Tool 消息（Worker 工具返回结果）→ onStep（中间步骤）
//	  - Assistant 纯文本（Executor 总结）→ onStep（中间步骤）
//	  - Assistant JSON（Planner/Replanner）→ 解析 Respond 字段作为 finalAnswer
//	  - finalAnswer 存在时 → onFinal；否则 fullContent 兜底 → onFinal
func BuildPlanAgent(ctx context.Context, query string, onStep func(string), onFinal func(string)) (string, error) {
	// 每次请求重新创建子 Agent：
	// ChatModelAgent（Executor）实现了 OnSetAsSubAgent，会修改内部 parentAgent 字段，
	// 导致第二次调用 planexecute.New 时报 "agent has already been set as a sub-agent" 错误。
	// 因此三个子 Agent 和 planexecute.New 均需每次请求重建（只是 HTTP 客户端初始化，无 API 调用开销）。
	planAgent, err := NewPlanner(ctx)
	if err != nil {
		return "", fmt.Errorf("init Planner: %w", err)
	}
	executeAgent, err := NewExecutor(ctx)
	if err != nil {
		return "", fmt.Errorf("init Executor: %w", err)
	}
	replanAgent, err := NewRePlanAgent(ctx)
	if err != nil {
		return "", fmt.Errorf("init Replanner: %w", err)
	}
	agent, err := planexecute.New(ctx, &planexecute.Config{
		Planner:       planAgent,
		Executor:      executeAgent,
		Replanner:     replanAgent,
		MaxIterations: 20,
	})
	if err != nil {
		return "", fmt.Errorf("create PlanExecuteAgent: %w", err)
	}

	// adk.NewRunner 将 Agent 包装为事件驱动的运行器。
	// 注意：Runner 每次新建是正确的（持有 query 查询状态），只有 Agent 本身需要缓存。
	// r.Query 启动异步执行（内部开 goroutine），返回事件迭代器 iter。
	// 每当 Agent 有输出（LLM 回复、工具结果、步骤完成等）时，iter.Next() 返回对应事件。
	r := adk.NewRunner(ctx, adk.RunnerConfig{Agent: agent})
	iter := r.Query(ctx, query)

	var fullContent string
	var finalAnswer string // Replanner Respond 工具的最终答案

	// 持续消费事件，直到迭代器耗尽。
	// 事件分四类：
	//   1. Planner/Replanner 的 JSON 输出（Assistant，Content 以 "{" 开头）：
	//      - Respond JSON → 解析 response 字段作为最终答案
	//      - Plan JSON    → 跳过
	//   2. Executor LLM 纯文本输出（Assistant，无 ToolCalls）→ 推送为 plan_step
	//   3. Executor Tool 消息（Worker 工具返回结果）→ 推送为 plan_step
	//   4. 其他（带 ToolCalls 的 Assistant 消息、空内容等）→ 跳过
	for {
		event, ok := iter.Next()
		if !ok {
			break
		}
		g.Log().Debugf(ctx, "[PlanPipeline] 事件到达 | agentName=%s", event.AgentName)
		if event.Err != nil {
			return fullContent, fmt.Errorf("plan agent 执行错误: %w", event.Err)
		}
		if event.Output == nil {
			continue
		}
		msg, _, err := adk.GetMessage(event)
		if err != nil || msg == nil {
			continue
		}

		// Tool 消息：Worker 工具执行结果，只推送前 150 字摘要，完整内容仍写入 fullContent
		if msg.Role == schema.Tool {
			content := strings.TrimSpace(msg.Content)
			if content == "" {
				continue
			}
			summary := content
			runes := []rune(content)
			if len(runes) > 150 {
				summary = string(runes[:150]) + "…"
			}
			checkpointWorkflow(ctx, "worker:"+msg.ToolName, map[string]any{
				"tool_name": msg.ToolName,
				"summary":   summarizeCheckpointContent(content),
			})
			if onStep != nil {
				onStep(marshalStepMsg(PlanStepMsg{
					Type:    "tool_result",
					Name:    workerFriendlyName(msg.ToolName),
					Content: summary,
				}))
			}
			fullContent += content + "\n"
			continue
		}

		// 只处理 Assistant 消息
		if msg.Role != schema.Assistant {
			continue
		}

		// 带 ToolCalls 的 Assistant 消息（Executor 决定调用 Worker）→ 推送 tool_call 事件
		if len(msg.ToolCalls) != 0 {
			if onStep != nil {
				for _, tc := range msg.ToolCalls {
					onStep(marshalStepMsg(PlanStepMsg{
						Type: "tool_call",
						Name: workerFriendlyName(tc.Function.Name),
					}))
				}
			}
			continue
		}

		trimmed := strings.TrimSpace(msg.Content)
		if trimmed == "" {
			continue
		}
		// JSON 输出：Planner（Plan JSON）或 Replanner（Plan/Respond JSON）
		if strings.HasPrefix(trimmed, "{") {
			// 尝试解析为 Plan（步骤列表），推送规划方案给前端
			var planParsed struct {
				Steps []string `json:"steps"`
			}
			if json.Unmarshal([]byte(trimmed), &planParsed) == nil && len(planParsed.Steps) > 0 {
				checkpointWorkflow(ctx, "planner", map[string]any{
					"steps": planParsed.Steps,
				})
				if onStep != nil {
					onStep(marshalStepMsg(PlanStepMsg{Type: "plan_steps", Steps: planParsed.Steps}))
				}
			}
			// 尝试解析为 Respond 工具的输出（Replanner 终止信号）
			var resp planexecute.Response
			if json.Unmarshal([]byte(trimmed), &resp) == nil && resp.Response != "" {
				finalAnswer = resp.Response
			}
			continue
		}
		// Executor 自然语言推理输出 → 推送为 exec 类型步骤
		if onStep != nil {
			onStep(marshalStepMsg(PlanStepMsg{Type: "exec", Content: msg.Content}))
		}
		fullContent += msg.Content + "\n"
	}

	// 优先使用 Replanner Respond 解析出的最终答案
	if finalAnswer != "" {
		finalAnswer = stripUUIDs(finalAnswer)
		checkpointWorkflow(ctx, "final", map[string]any{
			"final_answer": finalAnswer,
		})
		if onFinal != nil {
			onFinal(finalAnswer)
		}
		return fullContent + finalAnswer, nil
	}

	// 兜底：没有 Respond JSON，用 fullContent 作为最终答案
	if fullContent == "" {
		return "", fmt.Errorf("plan agent 未产生任何文本输出")
	}
	fullContent = stripUUIDs(fullContent)
	checkpointWorkflow(ctx, "final", map[string]any{
		"final_answer": fullContent,
	})
	if onFinal != nil {
		onFinal(fullContent)
	}
	return fullContent, nil
}
