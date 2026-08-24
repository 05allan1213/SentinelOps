// Package rageval RAG 质量评估 HTTP 控制器。
package rageval

import (
	"context"

	v1 "SentinelOps/api/rageval/v1"
	"SentinelOps/internal/ai/memory"
	rageval "SentinelOps/internal/service/rageval"
)

type controllerV1 struct{}

// NewV1 返回 rageval 控制器实例。
func NewV1() *controllerV1 {
	return &controllerV1{}
}

// Dashboard 返回 RAG KPI 汇总。
func (c *controllerV1) Dashboard(ctx context.Context, req *v1.DashboardReq) (res *v1.DashboardRes, err error) {
	m, err := rageval.GetDashboard(ctx, req.Window)
	if err != nil {
		return nil, err
	}
	return &v1.DashboardRes{
		SuccessRate:       m.SuccessRate,
		AvgLatencyMs:      m.AvgLatencyMs,
		P95LatencyMs:      m.P95LatencyMs,
		TotalRuns:         m.TotalRuns,
		AvgRetrievedDocs:  m.AvgRetrievedDocs,
		AvgTopScore:       m.AvgTopScore,
		SuccessRateStatus: m.SuccessRateStatus,
		LatencyStatus:     m.LatencyStatus,
		Trends:            m.Trends,
	}, nil
}

// Traces 返回最近 RAG 链路列表。
func (c *controllerV1) Traces(ctx context.Context, req *v1.TracesReq) (res *v1.TracesRes, err error) {
	items, total, err := rageval.ListTraces(ctx, req.Page, req.PageSize, req.Status)
	if err != nil {
		return nil, err
	}
	return &v1.TracesRes{List: items, Total: total}, nil
}

// TraceDetail 返回 Trace 链路详情。
func (c *controllerV1) TraceDetail(ctx context.Context, req *v1.TraceDetailReq) (res *v1.TraceDetailRes, err error) {
	detail, err := rageval.GetTraceDetail(ctx, req.TraceID)
	if err != nil {
		return nil, err
	}
	return &v1.TraceDetailRes{
		TraceID:           detail.TraceID,
		TraceName:         detail.TraceName,
		SessionID:         detail.SessionID,
		QueryText:         detail.QueryText,
		Status:            detail.Status,
		DurationMs:        detail.DurationMs,
		TotalInputTokens:  detail.TotalInputTokens,
		CachedInputTokens: detail.CachedInputTokens,
		TotalOutputTokens: detail.TotalOutputTokens,
		ReasoningTokens:   detail.ReasoningTokens,
		EstimatedCostCNY:  detail.EstimatedCostCNY,
		StartTime:         detail.StartTime,
		Nodes:             detail.Nodes,
		FeedbackVote:      detail.FeedbackVote,
	}, nil
}

// DeleteTrace 删除 Trace 链路记录。
func (c *controllerV1) DeleteTrace(ctx context.Context, req *v1.DeleteTraceReq) (res *v1.DeleteTraceRes, err error) {
	return &v1.DeleteTraceRes{}, rageval.DeleteTrace(ctx, req.TraceID)
}

// Feedback 提交消息反馈。
func (c *controllerV1) Feedback(ctx context.Context, req *v1.FeedbackReq) (res *v1.FeedbackRes, err error) {
	userID := req.UserID
	if id, _ := ctx.Value(memory.UserIdCtxKey{}).(string); id != "" {
		userID = id
	}
	return &v1.FeedbackRes{}, rageval.SubmitFeedback(ctx, req.SessionID, userID, req.MessageIndex, req.Vote, req.Reasons)
}

// FeedbackStats 返回反馈统计。
func (c *controllerV1) FeedbackStats(ctx context.Context, req *v1.FeedbackStatsReq) (res *v1.FeedbackStatsRes, err error) {
	stats, err := rageval.GetFeedbackStats(ctx)
	if err != nil {
		return nil, err
	}
	return &v1.FeedbackStatsRes{
		LikeRate:    stats.LikeRate,
		DislikeRate: stats.DislikeRate,
		NoVoteRate:  stats.NoVoteRate,
		Total:       stats.Total,
		Recent:      stats.Recent,
	}, nil
}
