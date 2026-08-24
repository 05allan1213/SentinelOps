// Package ops 提供 ChatOps 工具：在对话中直接触发 AI 运维。
package ops

import (
	"context"
	"encoding/json"
	"fmt"

	"SentinelOps/internal/ai/policy"
	dao "SentinelOps/internal/dao/mysql"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/gogf/gf/v2/frame/g"
)

// TriggerFunc 触发函数类型，由外部（main.go）注入，避免循环依赖
type TriggerFunc func(ctx context.Context, event *dao.Event)

var triggerFn TriggerFunc

// SetTriggerFunc 在 main.go 启动时注入实际触发函数（engine.TriggerForEvent）
func SetTriggerFunc(fn TriggerFunc) { triggerFn = fn }

type TriggerOpsInput struct {
	EventID string `json:"event_id" jsonschema:"description=要触发 AI 运维的安全事件 ID,required=true"`
}

// NewTriggerOpsTool 创建 ChatOps trigger_ops 工具。
func NewTriggerOpsTool() tool.InvokableTool {
	t, err := utils.InferOptionableTool(
		"trigger_ops",
		"触发指定安全事件的 AI 智能运维，自动分析事件并执行响应动作。",
		func(ctx context.Context, input *TriggerOpsInput, opts ...tool.Option) (string, error) {
			if err := policy.Authorize(ctx, policy.PermissionBusinessWrite, policy.Resource{}); err != nil {
				return "", err
			}
			if input.EventID == "" {
				return "", fmt.Errorf("event_id 不能为空")
			}
			if triggerFn == nil {
				return "", fmt.Errorf("运维触发函数未初始化")
			}
			event, err := dao.GetEventByID(ctx, input.EventID)
			if err != nil {
				return "", fmt.Errorf("获取事件失败: %w", err)
			}
			g.Log().Infof(ctx, "[trigger_ops] 触发事件 %s 的 AI 运维", input.EventID)
			go triggerFn(context.Background(), event)
			out, _ := json.Marshal(map[string]interface{}{
				"success":        true,
				"event_id":       input.EventID,
				"event_title":    event.Title,
				"event_severity": event.Severity,
				"message":        "AI 运维已触发，正在异步执行",
			})
			return string(out), nil
		},
	)
	if err != nil {
		panic(err)
	}
	return t
}
