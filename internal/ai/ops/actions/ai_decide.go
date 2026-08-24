// ai_decide.go AI 智能决策动作：根据事件内容自动选择并执行最合适的响应动作。
//
// 设计思想（AIOps 方向A）：
//
//	传统规则驱动是固定流程，ai_decide 让 AI 作为决策者，
//	根据事件的严重程度、类型、来源等上下文，从候选动作列表中选择最合适的动作并执行。
//	这是从"规则驱动自动化"到"AI 驱动智能运维"的核心跨越。
//
// 参数：
//
//	candidates: 逗号分隔的候选 action_type 列表，如 "block_ip,notify_dingtalk,update_event_status"
//	其余参数透传给被选中的动作（如 ip、status、webhook_url 等）
package actions

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"SentinelOps/internal/ai/models"
	dao "SentinelOps/internal/dao/mysql"

	"github.com/cloudwego/eino/schema"
)

type AIDecideAction struct{}

func (a *AIDecideAction) Name() string { return "ai_decide" }

func (a *AIDecideAction) Execute(ctx context.Context, params map[string]string) (ActionResult, error) {
	eventID := params["event_id"]
	candidatesStr := params["candidates"]
	if eventID == "" || candidatesStr == "" {
		return ActionResult{}, fmt.Errorf("ai_decide: event_id 和 candidates 不能为空")
	}
	candidates := strings.Split(candidatesStr, ",")
	for i := range candidates {
		candidates[i] = strings.TrimSpace(candidates[i])
	}

	event, err := dao.GetEventByID(ctx, eventID)
	if err != nil {
		return ActionResult{}, fmt.Errorf("ai_decide: 获取事件失败: %w", err)
	}

	chosen, reason, err := decideAction(ctx, event, candidates)
	if err != nil {
		return ActionResult{}, fmt.Errorf("ai_decide: LLM 决策失败: %w", err)
	}

	executor, ok := Get(chosen)
	if !ok {
		return ActionResult{}, fmt.Errorf("ai_decide: AI 选择了未知动作类型 %q", chosen)
	}

	result, execErr := executor.Execute(ctx, params)
	if execErr != nil {
		return ActionResult{}, fmt.Errorf("ai_decide: 执行 %s 失败: %w", chosen, execErr)
	}

	result.Message = fmt.Sprintf("[AI决策→%s] %s（理由：%s）", chosen, result.Message, reason)
	result.Output["ai_chosen_action"] = chosen
	result.Output["ai_reason"] = reason
	return result, nil
}

// decideAction 调用快速 LLM，从候选列表中选出最合适的动作
func decideAction(ctx context.Context, event *dao.Event, candidates []string) (chosen, reason string, err error) {
	m, err := models.ChatDefault(ctx)
	if err != nil {
		return "", "", err
	}

	prompt := fmt.Sprintf(`你是安全运营专家。根据以下安全事件，从候选响应动作中选择最合适的一个。

事件信息：
- 标题：%s
- 严重程度：%s（critical/high/medium/low）
- 事件类型：%s
- 来源：%s
- CVE：%s

候选动作：%s

动作说明：
- block_ip：封禁攻击源 IP，适用于有明确攻击 IP 的入侵/扫描事件
- notify_dingtalk：钉钉告警通知，适用于需要人工介入的高危事件
- notify_wecom：企微告警通知，同上
- notify_email：邮件通知，适用于需要正式记录的事件
- update_event_status：更新事件状态为处理中，适用于已有自动化处置的事件
- ai_analyze：AI 深度分析，适用于复杂/未知威胁需要深度研判的事件
- webhook_out：调用外部系统，适用于需要联动第三方平台的场景

请只返回 JSON，格式：{"action":"动作名","reason":"选择理由（一句话）"}`,
		event.Title, event.Severity, event.EventType, event.Source, event.CVEID,
		strings.Join(candidates, ", "),
	)

	out, err := m.Generate(ctx, []*schema.Message{schema.UserMessage(prompt)})
	if err != nil {
		return "", "", err
	}

	content := strings.TrimSpace(out.Content)
	// 去掉可能的 markdown 代码块
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)

	var resp struct {
		Action string `json:"action"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(content), &resp); err != nil {
		return "", "", fmt.Errorf("解析 LLM 响应失败: %w (raw: %s)", err, content)
	}

	// 校验 AI 选择的动作在候选列表中
	for _, c := range candidates {
		if c == resp.Action {
			return resp.Action, resp.Reason, nil
		}
	}
	return "", "", fmt.Errorf("AI 选择了不在候选列表中的动作: %q", resp.Action)
}

func init() {
	Register(&AIDecideAction{})
}
