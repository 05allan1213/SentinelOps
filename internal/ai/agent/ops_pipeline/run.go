package ops_pipeline

import (
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"SentinelOps/internal/ai/agent/base"
	"SentinelOps/internal/ai/agent/event_analysis_pipeline"
	"SentinelOps/internal/ai/prompt/agents"
	dao "SentinelOps/internal/dao/mysql"

	"github.com/cloudwego/eino/schema"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/google/uuid"
)

// ExecuteRun 两阶段运维执行：事件分析 Agent → 运维 Agent
func ExecuteRun(ctx context.Context, runID string, event *dao.Event) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()

	g.Log().Infof(ctx, "[ops] 开始执行 | runID=%s | event=%s | severity=%s", runID, event.Title, event.Severity)
	startedAt := time.Now()
	if err := dao.CreateRun(ctx, &dao.OpsRun{
		ID: runID, PlaybookID: "direct", EventID: event.ID,
		Status: "running", StartedAt: startedAt,
	}); err != nil {
		return fmt.Errorf("创建执行记录失败: %w", err)
	}

	g.Log().Infof(ctx, "[ops] 阶段1：事件分析 | runID=%s", runID)
	analysis, err := RunEventAnalysis(ctx, event)
	if err != nil {
		dao.UpdateRunStatus(ctx, runID, "failed", err.Error())
		g.Log().Warningf(ctx, "[ops] 事件分析失败 | runID=%s | err=%v", runID, err)
		return fmt.Errorf("事件分析失败: %w", err)
	}
	g.Log().Infof(ctx, "[ops] 事件分析完成 | runID=%s | len=%d", runID, len([]rune(analysis)))
	_ = dao.UpdateEventDescription(ctx, event.ID, "[AI自动分析]\n"+analysis)

	g.Log().Infof(ctx, "[ops] 阶段2：运维响应 | runID=%s", runID)
	if err := runOpsAgent(ctx, runID, event, analysis); err != nil {
		dao.UpdateRunStatus(ctx, runID, "failed", err.Error())
		g.Log().Warningf(ctx, "[ops] 运维响应失败 | runID=%s | err=%v", runID, err)
		return fmt.Errorf("运维执行失败: %w", err)
	}

	if nextStatus := nextAutoOpsStatus(event.Status); nextStatus != event.Status {
		if err := dao.UpdateEventStatus(ctx, event.ID, nextStatus); err != nil {
			g.Log().Warningf(ctx, "[ops] 更新事件状态失败 | runID=%s | eventID=%s | status=%s | err=%v", runID, event.ID, nextStatus, err)
		} else {
			event.Status = nextStatus
		}
	}
	dao.UpdateRunStatus(ctx, runID, "success", "")
	g.Log().Infof(ctx, "[ops] 执行完成 | runID=%s | elapsed=%s", runID, time.Since(startedAt).Round(time.Millisecond))
	return nil
}

func nextAutoOpsStatus(current string) string {
	switch current {
	case "new", "processing":
		return "resolved"
	default:
		return current
	}
}

// RunEventAnalysis 调用事件分析 Agent，返回分析文本（供 ai_analyze action 注入使用）
func RunEventAnalysis(ctx context.Context, event *dao.Event) (string, error) {
	agentCtx := context.Background()
	runner, err := event_analysis_pipeline.GetEventAnalysisAgent(agentCtx)
	if err != nil {
		return "", fmt.Errorf("初始化事件分析 Agent 失败: %w", err)
	}
	stream, err := runner.Stream(agentCtx, &base.UserMessage{
		Query: fmt.Sprintf("请分析以下安全事件：\n事件ID：%s\n标题：%s\n严重程度：%s\n类型：%s\n来源：%s\nCVE：%s",
			event.ID, event.Title, event.Severity, event.EventType, event.Source, event.CVEID),
	})
	if err != nil {
		return "", fmt.Errorf("事件分析 Agent 调用失败: %w", err)
	}
	defer stream.Close()
	return collectStream(stream, 0)
}

func runOpsAgent(ctx context.Context, runID string, event *dao.Event, analysis string) error {
	// 将 runID 注入 context，供工具层自动写步骤记录
	agentCtx := context.WithValue(context.Background(), runIDCtxKey{}, runID)
	runner, err := GetOpsAgent(agentCtx)
	if err != nil {
		return fmt.Errorf("初始化运维 Agent 失败: %w", err)
	}
	startedAt := time.Now()
	stream, err := runner.Stream(agentCtx, &base.UserMessage{
		Query: fmt.Sprintf(agents.OpsRunQuery,
			event.ID, event.Title, event.Severity, event.Source, event.CVEID, analysis),
	})
	if err != nil {
		return fmt.Errorf("运维 Agent 调用失败: %w", err)
	}
	defer stream.Close()
	if _, err := collectStream(stream, 0); err != nil {
		return err
	}
	now := time.Now()
	// ops_agent 父步骤只记录状态和耗时，不保存输出文本
	rs := &dao.OpsRunStep{
		RunID: runID, StepID: uuid.New().String(), StepOrder: 0,
		ActionType: "ops_agent", ResolvedParams: "{}", Status: "success",
		Output: "", StartedAt: startedAt, FinishedAt: &now,
		DurationMs: now.Sub(startedAt).Milliseconds(),
	}
	dao.CreateRunStep(ctx, rs)
	return nil
}

// eventIDPattern 用于从 query 文本中提取 UUID 格式的事件 ID
var eventIDPattern = regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)

// RunWithQuery 供 Plan Agent Worker 调用：从 query 中提取事件 ID，异步触发运维并返回确认文本。
func RunWithQuery(ctx context.Context, query string) (string, error) {
	eventID := eventIDPattern.FindString(query)
	if eventID == "" {
		return "未在任务描述中找到有效的事件 ID（UUID 格式），无法触发运维。请在任务中明确指定事件 ID。", nil
	}
	event, err := dao.GetEventByID(ctx, eventID)
	if err != nil {
		return "", fmt.Errorf("获取事件失败: %w", err)
	}
	claimed, err := dao.ClaimEventForOps(ctx, event.ID)
	if err != nil {
		return "", fmt.Errorf("抢占事件运维处理权失败: %w", err)
	}
	if !claimed {
		return fmt.Sprintf("事件「%s」已被其他 AI 运维任务接管或已完成，跳过重复触发。", event.Title), nil
	}
	event.Status = "processing"

	runID := uuid.New().String()
	g.Log().Infof(ctx, "[ops] Plan Agent 触发运维 | runID=%s | event=%s", runID, eventID)
	go func() {
		if err := ExecuteRun(context.Background(), runID, event); err != nil {
			g.Log().Warningf(context.Background(), "[ops] Plan Agent 触发运维失败 | runID=%s | err=%v", runID, err)
		}
	}()
	return fmt.Sprintf("已触发事件「%s」的 AI 智能运维（run_id: %s），正在异步执行封禁/通知/状态更新等响应动作。\n\n请前往「AI 智能运维」界面查看实时执行进度和步骤详情。", event.Title, runID), nil
}

func collectStream(stream interface {
	Recv() (*schema.Message, error)
}, maxRunes int) (string, error) {
	var sb strings.Builder
	for {
		msg, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", err
		}
		if msg != nil && msg.Content != "" {
			sb.WriteString(msg.Content)
		}
	}
	result := sb.String()
	if maxRunes > 0 {
		if runes := []rune(result); len(runes) > maxRunes {
			result = string(runes[:maxRunes]) + "\n[内容已截断]"
		}
	}
	return result, nil
}
