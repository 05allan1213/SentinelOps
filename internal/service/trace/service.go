// Package trace 提供链路追踪业务逻辑层
package trace

import (
	"context"
	"sort"
	"time"

	v1 "SentinelOps/api/trace/v1"
	dao "SentinelOps/internal/dao/mysql"
	redisdao "SentinelOps/internal/dao/redis"
)

// Service trace 业务服务
type Service struct {
	dao *dao.TraceDAO
}

// NewService 创建服务实例
func NewService() *Service {
	return &Service{
		dao: dao.NewTraceDAO(),
	}
}

// ListRuns 分页查询链路列表
func (s *Service) ListRuns(ctx context.Context, status, traceID, sessionID string, page, pageSize int) ([]v1.TraceRunVO, int64, error) {
	// 参数校验
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	runs, total, err := s.dao.ListRuns(ctx, status, traceID, sessionID, page, pageSize)
	if err != nil {
		return nil, 0, err
	}

	list := make([]v1.TraceRunVO, 0, len(runs))
	for _, r := range runs {
		list = append(list, toTraceRunVO(&r))
	}
	return list, total, nil
}

// GetDetail 获取链路详情
func (s *Service) GetDetail(ctx context.Context, traceID string) (*v1.TraceRunVO, []v1.TraceNodeVO, error) {
	run, err := s.dao.GetRunByTraceID(ctx, traceID)
	if err != nil {
		return nil, nil, err
	}

	nodes, err := s.dao.ListNodesByTraceID(ctx, traceID)
	if err != nil {
		return nil, nil, err
	}

	nodeVOs := make([]v1.TraceNodeVO, 0, len(nodes))
	for _, n := range nodes {
		nodeVOs = append(nodeVOs, toTraceNodeVO(&n))
	}

	runVO := toTraceRunVO(run)
	return &runVO, nodeVOs, nil
}

// GetStats 获取统计数据
func (s *Service) GetStats(ctx context.Context, days int) (*v1.StatsRes, error) {
	if days <= 0 {
		days = 7
	}
	since := time.Now().AddDate(0, 0, -days)

	// 基础统计
	agg, err := s.dao.GetStatsAgg(ctx, since)
	if err != nil {
		return &v1.StatsRes{}, nil
	}

	// P95 计算
	durations, _ := s.dao.GetSuccessDurations(ctx, since)
	p95 := calcP95(durations)

	var errorRate, avgCost float64
	if agg.Total > 0 {
		errorRate = float64(agg.Errors) / float64(agg.Total)
		avgCost = agg.TotalCost / float64(agg.Total)
	}

	return &v1.StatsRes{
		TotalRuns:         agg.Total,
		SuccessRuns:       agg.Success,
		ErrorRuns:         agg.Errors,
		AvgDurationMs:     agg.AvgDur,
		P95DurationMs:     p95,
		TotalInputTokens:  agg.TotalIn,
		TotalOutputTokens: agg.TotalOut,
		TotalCostCNY:      agg.TotalCost,
		AvgCostCNY:        avgCost,
		ErrorRate:         errorRate,
	}, nil
}

// BatchDelete 批量删除链路
func (s *Service) BatchDelete(ctx context.Context, traceIDs []string) (int64, error) {
	if len(traceIDs) == 0 {
		return 0, nil
	}
	return s.dao.BatchDeleteByTraceIDs(ctx, traceIDs)
}

// GetSessionTimeline 获取会话时间线
func (s *Service) GetSessionTimeline(ctx context.Context, sessionID string) (*v1.SessionTimelineRes, error) {
	runs, err := s.dao.ListRunsBySessionID(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	summaries := make([]v1.SessionTimelineSummary, 0, len(runs))
	for _, r := range runs {
		summaries = append(summaries, v1.SessionTimelineSummary{
			TraceId:    r.TraceID,
			QueryText:  r.QueryText,
			Status:     r.Status,
			DurationMs: r.DurationMs,
			StartTime:  r.StartTime.Format("2006-01-02T15:04:05.000Z07:00"),
			ErrorCode:  r.ErrorCode,
		})
	}

	return &v1.SessionTimelineRes{
		SessionId: sessionID,
		Total:     len(summaries),
		Runs:      summaries,
	}, nil
}

// GetCostOverview 获取成本概览
func (s *Service) GetCostOverview(ctx context.Context, startDate, endDate string, days int) (*v1.CostOverviewRes, error) {
	// 计算时间范围
	var since, until time.Time
	if startDate != "" && endDate != "" {
		since, _ = time.ParseInLocation("2006-01-02", startDate, time.Local)
		until, _ = time.ParseInLocation("2006-01-02", endDate, time.Local)
		until = until.Add(24 * time.Hour)
	} else {
		if days <= 0 {
			days = 30
		}
		until = time.Now()
		since = until.AddDate(0, 0, -days)
	}
	prevSince := since.Add(-(until.Sub(since)))

	// 当前周期和上一周期聚合
	cur, _ := s.dao.GetCostAgg(ctx, since, until)
	prev, _ := s.dao.GetCostAgg(ctx, prevSince, since)

	var changePct, avgCost float64
	if prev.TotalCost > 0 {
		changePct = (cur.TotalCost - prev.TotalCost) / prev.TotalCost * 100
	}
	if cur.TotalReqs > 0 {
		avgCost = cur.TotalCost / float64(cur.TotalReqs)
	}

	// 每日趋势
	dailyRows, _ := s.dao.GetDailyCostTrend(ctx, since, until)
	dailyTrend := make([]v1.DailyCostPoint, 0, len(dailyRows))
	for _, r := range dailyRows {
		dailyTrend = append(dailyTrend, v1.DailyCostPoint{
			Date:         r.Day,
			CostCNY:      r.DayCost,
			InputTokens:  r.DayIn,
			OutputTokens: r.DayOut,
			RequestCount: r.DayRequests,
		})
	}

	// 模型分布
	modelRows, _ := s.dao.GetModelCostBreakdown(ctx, since, until)
	modelBreakdown := make([]v1.ModelCostItem, 0, len(modelRows))
	for _, r := range modelRows {
		pct := 0.0
		if cur.TotalCost > 0 {
			pct = r.NodeCost / cur.TotalCost * 100
		}
		modelBreakdown = append(modelBreakdown, v1.ModelCostItem{
			ModelName:    r.ModelName,
			TotalCostCNY: r.NodeCost,
			InputTokens:  r.NodeIn,
			OutputTokens: r.NodeOut,
			RequestCount: r.NodeReqs,
			CostPct:      pct,
		})
	}

	// 意图分布
	intentRows, _ := s.dao.GetIntentCostBreakdown(ctx, since, until, 10)
	intentBreakdown := make([]v1.IntentCostItem, 0, len(intentRows))
	for _, r := range intentRows {
		pct := 0.0
		if cur.TotalCost > 0 {
			pct = r.IntentCost / cur.TotalCost * 100
		}
		avg := 0.0
		if r.IntentReqs > 0 {
			avg = r.IntentCost / float64(r.IntentReqs)
		}
		intentBreakdown = append(intentBreakdown, v1.IntentCostItem{
			TraceName:    r.TraceName,
			TotalCostCNY: r.IntentCost,
			RequestCount: r.IntentReqs,
			AvgCostCNY:   avg,
			CostPct:      pct,
		})
	}

	return &v1.CostOverviewRes{
		TotalCostCNY:      cur.TotalCost,
		TotalInputTokens:  cur.TotalIn,
		TotalOutputTokens: cur.TotalOut,
		TotalRequests:     cur.TotalReqs,
		AvgCostPerReq:     avgCost,
		PrevTotalCostCNY:  prev.TotalCost,
		CostChangePct:     changePct,
		DailyTrend:        dailyTrend,
		ModelBreakdown:    modelBreakdown,
		IntentBreakdown:   intentBreakdown,
	}, nil
}

// GetTokenTrend 获取 Token 趋势
func (s *Service) GetTokenTrend(ctx context.Context, hours int) (*v1.TokenTrendRes, error) {
	if hours <= 0 {
		hours = 24
	}
	since := time.Now().Add(-time.Duration(hours) * time.Hour)

	rows, err := s.dao.GetHourlyTokenTrend(ctx, since)
	if err != nil {
		return &v1.TokenTrendRes{}, nil
	}

	points := make([]v1.TokenTrendPoint, 0, len(rows))
	for _, r := range rows {
		points = append(points, v1.TokenTrendPoint{
			Hour:         r.HourStr,
			InputTokens:  r.HourIn,
			OutputTokens: r.HourOut,
			RequestCount: r.HourReqs,
		})
	}
	return &v1.TokenTrendRes{Points: points}, nil
}

// calcP95 计算第 95 百分位
func calcP95(durations []int64) int64 {
	if len(durations) == 0 {
		return 0
	}
	sorted := make([]int64, len(durations))
	copy(sorted, durations)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	idx := int(float64(len(sorted)) * 0.95)
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

// toTraceRunVO 模型转 VO
func toTraceRunVO(r *dao.TraceRun) v1.TraceRunVO {
	return v1.TraceRunVO{
		TraceId:           r.TraceID,
		TraceName:         r.TraceName,
		EntryPoint:        r.EntryPoint,
		Status:            r.Status,
		DurationMs:        r.DurationMs,
		StartTime:         r.StartTime.Format("2006-01-02T15:04:05.000Z07:00"),
		SessionId:         r.SessionID,
		QueryText:         r.QueryText,
		TotalInputTokens:  r.TotalInputTokens,
		CachedInputTokens: r.CachedInputTokens,
		TotalOutputTokens: r.TotalOutputTokens,
		ReasoningTokens:   r.ReasoningTokens,
		EstimatedCostCNY:  r.EstimatedCostCNY,
		ErrorCode:         r.ErrorCode,
		ErrorMessage:      r.ErrorMessage,
		Tags:              r.Tags,
	}
}

// toTraceNodeVO 模型转 VO
func toTraceNodeVO(n *dao.TraceNode) v1.TraceNodeVO {
	return v1.TraceNodeVO{
		NodeId:            n.NodeID,
		ParentNodeId:      n.ParentNodeID,
		NodeType:          n.NodeType,
		NodeName:          n.NodeName,
		Depth:             n.Depth,
		Status:            n.Status,
		DurationMs:        n.DurationMs,
		StartTime:         n.StartTime.Format("2006-01-02T15:04:05.000Z07:00"),
		ErrorMessage:      n.ErrorMessage,
		ErrorCode:         n.ErrorCode,
		ErrorType:         n.ErrorType,
		ModelName:         n.ModelName,
		InputTokens:       n.InputTokens,
		CachedInputTokens: n.CachedInputTokens,
		OutputTokens:      n.OutputTokens,
		ReasoningTokens:   n.ReasoningTokens,
		CostCNY:           n.CostCNY,
		CompletionText:    n.CompletionText,
		QueryText:         n.QueryText,
		RetrievedDocs:     n.RetrievedDocs,
		FinalTopK:         n.FinalTopK,
		CacheHit:          n.CacheHit,
		Metadata:          n.Metadata,
	}
}

// GetSessionSnapshot 实时从 Redis 读取会话对话快照
func (s *Service) GetSessionSnapshot(ctx context.Context, sessionID string) (string, error) {
	return redisdao.GetSessionSnapshot(ctx, sessionID)
}
