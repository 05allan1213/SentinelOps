// Package ops AI 运维通知类 Eino tools，供 AI 运维 Agent 调用。
package ops

import (
	"context"
	"encoding/json"
	"time"

	"SentinelOps/internal/ai/ops/actions"
	"SentinelOps/internal/ai/ops/ctxkey"
	dao "SentinelOps/internal/dao/mysql"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/google/uuid"
)

// runIDCtxKey 与 ops_pipeline 包共享同一类型，确保 context 取值正确
type runIDCtxKey = ctxkey.RunID

// writeStep 从 context 取 runID，写一条工具执行步骤记录。
func writeStep(ctx context.Context, order int, actionType, output, errMsg string, start time.Time) {
	runID, _ := ctx.Value(runIDCtxKey{}).(string)
	if runID == "" {
		return
	}
	now := time.Now()
	status := "success"
	if errMsg != "" {
		status = "failed"
	}
	dao.CreateRunStep(ctx, &dao.OpsRunStep{
		RunID: runID, StepID: uuid.New().String(), StepOrder: order,
		ActionType: actionType, ResolvedParams: "{}", Status: status,
		Output: output, ErrorMsg: errMsg,
		StartedAt: start, FinishedAt: &now,
		DurationMs: now.Sub(start).Milliseconds(),
	})
}

func execAction(ctx context.Context, name string, params map[string]string) (string, error) {
	exec, ok := actions.Get(name)
	if !ok {
		return `{"error":"action not found"}`, nil
	}
	result, err := exec.Execute(ctx, params)
	if err != nil {
		return "", err
	}
	out, _ := json.Marshal(result.Output)
	return string(out), nil
}

// stepOrder 按工具名返回固定顺序，保证前端展示顺序稳定。
var stepOrder = map[string]int{
	"update_event_status": 1,
	"block_ip":            2,
	"notify_dingtalk":     3,
	"notify_wecom":        4,
	"notify_email":        5,
}

func execAndRecord(ctx context.Context, name string, params map[string]string) (string, error) {
	start := time.Now()
	out, err := execAction(ctx, name, params)
	errMsg := ""
	if err != nil {
		errMsg = err.Error()
	}
	writeStep(ctx, stepOrder[name], name, out, errMsg, start)
	return out, err
}

type NotifyDingTalkInput struct {
	Title   string `json:"title" jsonschema:"description=通知标题,required=true"`
	Content string `json:"content" jsonschema:"description=通知正文（Markdown）,required=true"`
}

func NewNotifyDingTalkTool() tool.InvokableTool {
	t, err := utils.InferOptionableTool(
		"notify_dingtalk",
		"通过钉钉 Webhook 发送安全告警通知（Markdown）。系统已预配置，直接调用即可；未配置时自动跳过。",
		func(ctx context.Context, in *NotifyDingTalkInput, _ ...tool.Option) (string, error) {
			return execAndRecord(ctx, "notify_dingtalk", map[string]string{"title": in.Title, "content": in.Content})
		},
	)
	if err != nil {
		panic(err)
	}
	return t
}

type NotifyWeComInput struct {
	Content string `json:"content" jsonschema:"description=通知正文（Markdown）,required=true"`
}

func NewNotifyWeComTool() tool.InvokableTool {
	t, err := utils.InferOptionableTool(
		"notify_wecom",
		"通过企业微信 Webhook 发送安全告警通知（Markdown）。系统已预配置，直接调用即可；未配置时自动跳过。",
		func(ctx context.Context, in *NotifyWeComInput, _ ...tool.Option) (string, error) {
			return execAndRecord(ctx, "notify_wecom", map[string]string{"content": in.Content})
		},
	)
	if err != nil {
		panic(err)
	}
	return t
}

type NotifyEmailInput struct {
	To            string `json:"to" jsonschema:"description=收件人邮箱（多个用逗号分隔），留空时自动使用系统配置的默认收件人"`
	Subject       string `json:"subject" jsonschema:"description=邮件主题，留空自动生成"`
	EventTitle    string `json:"event_title" jsonschema:"description=事件标题"`
	EventSeverity string `json:"event_severity" jsonschema:"description=事件严重程度"`
	EventSource   string `json:"event_source" jsonschema:"description=事件来源"`
	EventCVE      string `json:"event_cve" jsonschema:"description=CVE 编号"`
	EventID       string `json:"event_id" jsonschema:"description=事件 ID"`
	Analysis      string `json:"analysis" jsonschema:"description=AI 分析结论（附在邮件中）"`
}

func NewNotifyEmailTool() tool.InvokableTool {
	t, err := utils.InferOptionableTool(
		"notify_email",
		"发送 HTML 格式安全告警邮件。系统已预配置 SMTP 和默认收件人，直接调用即可；未配置时自动跳过。",
		func(ctx context.Context, in *NotifyEmailInput, _ ...tool.Option) (string, error) {
			return execAndRecord(ctx, "notify_email", map[string]string{
				"to": in.To, "subject": in.Subject,
				"event_title": in.EventTitle, "event_severity": in.EventSeverity,
				"event_source": in.EventSource, "event_cve": in.EventCVE,
				"event_id": in.EventID, "analysis": in.Analysis,
			})
		},
	)
	if err != nil {
		panic(err)
	}
	return t
}

type BlockIPInput struct {
	IP     string `json:"ip" jsonschema:"description=要封禁的 IP 地址,required=true"`
	Reason string `json:"reason" jsonschema:"description=封禁原因"`
}

func NewBlockIPTool() tool.InvokableTool {
	t, err := utils.InferOptionableTool(
		"block_ip",
		"将 IP 加入封禁名单。若 IP 在保护白名单中则自动跳过。",
		func(ctx context.Context, in *BlockIPInput, _ ...tool.Option) (string, error) {
			return execAndRecord(ctx, "block_ip", map[string]string{"ip": in.IP, "reason": in.Reason})
		},
	)
	if err != nil {
		panic(err)
	}
	return t
}

type UpdateEventStatusInput struct {
	EventID string `json:"event_id" jsonschema:"description=安全事件 ID,required=true"`
	Status  string `json:"status" jsonschema:"description=新状态：processing/resolved/ignored,required=true"`
}

func NewUpdateEventStatusTool() tool.InvokableTool {
	t, err := utils.InferOptionableTool(
		"update_event_status",
		"更新安全事件的处理状态（processing/resolved/ignored）。",
		func(ctx context.Context, in *UpdateEventStatusInput, _ ...tool.Option) (string, error) {
			return execAndRecord(ctx, "update_event_status", map[string]string{"event_id": in.EventID, "status": in.Status})
		},
	)
	if err != nil {
		panic(err)
	}
	return t
}
