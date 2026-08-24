// Package actions AI 运维事件状态更新、IP封禁、AI分析动作。
package actions

import (
	"context"
	"fmt"

	dao "SentinelOps/internal/dao/mysql"

	"github.com/gogf/gf/v2/frame/g"
)

// AnalyzeFunc AI 分析函数类型，由外部注入避免循环依赖
type AnalyzeFunc func(ctx context.Context, event *dao.Event) (string, error)

var analyzeFn AnalyzeFunc

// SetAnalyzeFunc 在启动时注入实际分析函数
func SetAnalyzeFunc(fn AnalyzeFunc) { analyzeFn = fn }

// ---- 事件状态更新 ----

type UpdateEventStatusAction struct{}

func (a *UpdateEventStatusAction) Name() string { return "update_event_status" }

func (a *UpdateEventStatusAction) Execute(ctx context.Context, params map[string]string) (ActionResult, error) {
	eventID := params["event_id"]
	status := params["status"]
	if eventID == "" || status == "" {
		return ActionResult{}, fmt.Errorf("update_event_status: event_id 和 status 不能为空")
	}
	if err := dao.UpdateEventStatus(ctx, eventID, status); err != nil {
		return ActionResult{}, fmt.Errorf("update_event_status: %w", err)
	}
	return ActionResult{
		Success: true,
		Message: fmt.Sprintf("事件 %s 状态已更新为 %s", eventID, status),
		Output:  map[string]string{"event_id": eventID, "status": status},
	}, nil
}

// ---- IP 封禁 ----

type BlockIPAction struct{}

func (a *BlockIPAction) Name() string { return "block_ip" }

func (a *BlockIPAction) Execute(ctx context.Context, params map[string]string) (ActionResult, error) {
	ip := params["ip"]
	reason := params["reason"]
	if ip == "" {
		return ActionResult{}, fmt.Errorf("block_ip: ip 不能为空")
	}
	protected, err := dao.IsProtectedAsset(ctx, "whitelist_ip", ip)
	if err != nil {
		return ActionResult{}, fmt.Errorf("block_ip: 保护名单查询失败: %w", err)
	}
	if protected {
		return ActionResult{
			Success: false,
			Message: fmt.Sprintf("IP %s 在保护名单中，跳过封禁", ip),
			Output:  map[string]string{"ip": ip, "blocked": "false", "reason": "protected"},
		}, nil
	}
	alreadyBlocked, err := dao.IsProtectedAsset(ctx, "blocked_ip", ip)
	if err != nil {
		return ActionResult{}, fmt.Errorf("block_ip: 查询封禁状态失败: %w", err)
	}
	if !alreadyBlocked {
		if err := dao.CreateProtectedAsset(ctx, &dao.OpsProtectedAsset{
			AssetType: "blocked_ip", Value: ip, Reason: reason,
		}); err != nil {
			return ActionResult{}, fmt.Errorf("block_ip: 写入封禁记录失败: %w", err)
		}
	}

	// nginx 后端：写黑名单文件并 reload
	nginxMsg := ""
	mgr := NewNginxBlocklistManager(ctx)
	if mgr.IsEnabled() {
		if err := mgr.AddIP(ctx, ip); err != nil {
			g.Log().Warningf(ctx, "[block_ip] nginx 封禁失败，已记录数据库: %v", err)
			nginxMsg = "（nginx reload 失败，仅记录数据库）"
		} else {
			nginxMsg = "（已写入 nginx 黑名单并 reload）"
		}
	}

	return ActionResult{
		Success: true,
		Message: fmt.Sprintf("IP %s 已加入封禁名单%s", ip, nginxMsg),
		Output:  map[string]string{"ip": ip, "blocked": "true", "backend": mgr.backend},
	}, nil
}

// ---- AI 分析（通过注入函数调用，避免循环依赖） ----

type AIAnalyzeAction struct{}

func (a *AIAnalyzeAction) Name() string { return "ai_analyze" }

func (a *AIAnalyzeAction) Execute(ctx context.Context, params map[string]string) (ActionResult, error) {
	eventID := params["event_id"]
	if eventID == "" {
		return ActionResult{}, fmt.Errorf("ai_analyze: event_id 不能为空")
	}
	if analyzeFn == nil {
		return ActionResult{}, fmt.Errorf("ai_analyze: 分析函数未初始化")
	}
	event, err := dao.GetEventByID(ctx, eventID)
	if err != nil {
		return ActionResult{}, fmt.Errorf("ai_analyze: 获取事件失败: %w", err)
	}
	analysis, err := analyzeFn(ctx, event)
	if err != nil {
		return ActionResult{}, fmt.Errorf("ai_analyze: %w", err)
	}
	_ = dao.UpdateEventDescription(ctx, eventID, "[AI自动分析]\n"+analysis)
	return ActionResult{
		Success: true,
		Message: fmt.Sprintf("事件「%s」AI 分析完成", event.Title),
		Output: map[string]string{
			"event_id": eventID, "title": event.Title,
			"severity": event.Severity, "source": event.Source,
			"analysis": analysis,
		},
	}, nil
}

func init() {
	Register(&UpdateEventStatusAction{})
	Register(&BlockIPAction{})
	Register(&AIAnalyzeAction{})
}
