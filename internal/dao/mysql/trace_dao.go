// Package mysql 提供 trace 数据访问层
package mysql

import (
	"context"
	"time"

	"gorm.io/gorm"
)

// TraceDAO trace 数据访问对象
type TraceDAO struct{}

// NewTraceDAO 创建 DAO 实例
func NewTraceDAO() *TraceDAO {
	return &TraceDAO{}
}

// ListRuns 分页查询链路运行记录
func (d *TraceDAO) ListRuns(ctx context.Context, status, traceID, sessionID string, page, pageSize int) ([]TraceRun, int64, error) {
	db, err := DB(ctx)
	if err != nil {
		return nil, 0, err
	}

	query := db.Model(&TraceRun{})
	if status != "" {
		query = query.Where("status = ?", status)
	}
	if traceID != "" {
		query = query.Where("trace_id = ?", traceID)
	}
	if sessionID != "" {
		query = query.Where("session_id = ?", sessionID)
	}

	var total int64
	query.Count(&total)

	var runs []TraceRun
	offset := (page - 1) * pageSize
	query.Order("created_at DESC").Limit(pageSize).Offset(offset).
		Select("id, trace_id, trace_name, entry_point, session_id, query_text, " +
			"status, error_message, error_code, start_time, end_time, duration_ms, " +
			"total_input_tokens, total_output_tokens, " +
			"estimated_cost_cny, tags, created_at, updated_at").
		Find(&runs)

	return runs, total, nil
}

// GetRunByTraceID 根据 trace_id 查询单条记录
func (d *TraceDAO) GetRunByTraceID(ctx context.Context, traceID string) (*TraceRun, error) {
	db, err := DB(ctx)
	if err != nil {
		return nil, err
	}

	var run TraceRun
	if result := db.Where("trace_id = ?", traceID).First(&run); result.Error != nil {
		return nil, result.Error
	}
	return &run, nil
}

// ListNodesByTraceID 查询指定链路的所有节点
func (d *TraceDAO) ListNodesByTraceID(ctx context.Context, traceID string) ([]TraceNode, error) {
	db, err := DB(ctx)
	if err != nil {
		return nil, err
	}

	var nodes []TraceNode
	db.Where("trace_id = ?", traceID).Order("start_time ASC").Find(&nodes)
	return nodes, nil
}

// GetStatsAgg 获取基础统计聚合数据
func (d *TraceDAO) GetStatsAgg(ctx context.Context, since time.Time) (*StatsAggResult, error) {
	db, err := DB(ctx)
	if err != nil {
		return nil, err
	}

	var result StatsAggResult
	db.Model(&TraceRun{}).
		Where("created_at >= ?", since).
		Select("COUNT(*) as total, " +
			"SUM(CASE WHEN status='success' THEN 1 ELSE 0 END) as success, " +
			"SUM(CASE WHEN status='error' THEN 1 ELSE 0 END) as errors, " +
			"AVG(duration_ms) as avg_dur, " +
			"SUM(total_input_tokens) as total_in, " +
			"SUM(total_output_tokens) as total_out, " +
			"SUM(estimated_cost_cny) as total_cost").
		Scan(&result)

	return &result, nil
}

// GetSuccessDurations 获取成功请求的耗时列表（用于 P95 计算）
func (d *TraceDAO) GetSuccessDurations(ctx context.Context, since time.Time) ([]int64, error) {
	db, err := DB(ctx)
	if err != nil {
		return nil, err
	}

	var durations []int64
	db.Model(&TraceRun{}).
		Where("created_at >= ? AND status = 'success'", since).
		Pluck("duration_ms", &durations)

	return durations, nil
}

// BatchDeleteByTraceIDs 批量删除链路记录及关联节点
func (d *TraceDAO) BatchDeleteByTraceIDs(ctx context.Context, traceIDs []string) (int64, error) {
	db, err := DB(ctx)
	if err != nil {
		return 0, err
	}

	var deleted int64
	err = db.Transaction(func(tx *gorm.DB) error {
		// 先删除节点
		tx.Where("trace_id IN ?", traceIDs).Delete(&TraceNode{})
		// 再删除 run
		result := tx.Where("trace_id IN ?", traceIDs).Delete(&TraceRun{})
		deleted = result.RowsAffected
		return result.Error
	})
	return deleted, err
}

// ListRunsBySessionID 查询指定会话的所有链路
func (d *TraceDAO) ListRunsBySessionID(ctx context.Context, sessionID string) ([]TraceRun, error) {
	db, err := DB(ctx)
	if err != nil {
		return nil, err
	}

	var runs []TraceRun
	db.Model(&TraceRun{}).
		Where("session_id = ?", sessionID).
		Select("id, trace_id, query_text, status, duration_ms, start_time, error_code").
		Order("start_time ASC").
		Find(&runs)

	return runs, nil
}

// GetCostAgg 获取成本聚合数据
func (d *TraceDAO) GetCostAgg(ctx context.Context, since, until time.Time) (*CostAggResult, error) {
	db, err := DB(ctx)
	if err != nil {
		return nil, err
	}

	var result CostAggResult
	db.Model(&TraceRun{}).
		Where("created_at >= ? AND created_at < ? AND status != 'running'", since, until).
		Select("SUM(estimated_cost_cny) as total_cost, SUM(total_input_tokens) as total_in, " +
			"SUM(total_output_tokens) as total_out, COUNT(*) as total_reqs").
		Scan(&result)

	return &result, nil
}

// GetDailyCostTrend 获取每日成本趋势
func (d *TraceDAO) GetDailyCostTrend(ctx context.Context, since, until time.Time) ([]DailyCostRow, error) {
	db, err := DB(ctx)
	if err != nil {
		return nil, err
	}

	var rows []DailyCostRow
	db.Model(&TraceRun{}).
		Where("created_at >= ? AND created_at < ? AND status != 'running'", since, until).
		Select("DATE(created_at) as day, SUM(estimated_cost_cny) as day_cost, " +
			"SUM(total_input_tokens) as day_in, SUM(total_output_tokens) as day_out, " +
			"COUNT(*) as day_requests").
		Group("DATE(created_at)").Order("day ASC").Scan(&rows)

	return rows, nil
}

// GetModelCostBreakdown 获取模型成本分布
func (d *TraceDAO) GetModelCostBreakdown(ctx context.Context, since, until time.Time) ([]ModelCostRow, error) {
	db, err := DB(ctx)
	if err != nil {
		return nil, err
	}

	var rows []ModelCostRow
	db.Model(&TraceNode{}).
		Joins("JOIN agent_trace_runs r ON r.trace_id = agent_trace_nodes.trace_id").
		Where("r.created_at >= ? AND r.created_at < ? AND agent_trace_nodes.node_type IN ('LLM', 'RERANK', 'EMBEDDING') AND agent_trace_nodes.model_name != ''", since, until).
		Select("agent_trace_nodes.model_name, SUM(agent_trace_nodes.cost_cny) as node_cost, " +
			"SUM(agent_trace_nodes.input_tokens) as node_in, SUM(agent_trace_nodes.output_tokens) as node_out, " +
			"COUNT(DISTINCT r.trace_id) as node_reqs").
		Group("agent_trace_nodes.model_name").Order("node_cost DESC").Scan(&rows)

	return rows, nil
}

// GetIntentCostBreakdown 获取意图成本分布
func (d *TraceDAO) GetIntentCostBreakdown(ctx context.Context, since, until time.Time, limit int) ([]IntentCostRow, error) {
	db, err := DB(ctx)
	if err != nil {
		return nil, err
	}

	var rows []IntentCostRow
	db.Model(&TraceRun{}).
		Where("created_at >= ? AND created_at < ? AND status != 'running'", since, until).
		Select("trace_name, SUM(estimated_cost_cny) as intent_cost, COUNT(*) as intent_reqs").
		Group("trace_name").Order("intent_cost DESC").Limit(limit).Scan(&rows)

	return rows, nil
}

// GetHourlyTokenTrend 获取按小时的 Token 趋势
func (d *TraceDAO) GetHourlyTokenTrend(ctx context.Context, since time.Time) ([]HourlyTokenRow, error) {
	db, err := DB(ctx)
	if err != nil {
		return nil, err
	}

	var rows []HourlyTokenRow
	db.Model(&TraceRun{}).
		Where("created_at >= ? AND status != 'running'", since).
		Select("DATE_FORMAT(created_at, '%Y-%m-%d %H') as hour_str, " +
			"SUM(total_input_tokens) as hour_in, SUM(total_output_tokens) as hour_out, " +
			"COUNT(*) as hour_reqs").
		Group("hour_str").Order("hour_str ASC").Scan(&rows)

	return rows, nil
}

// DAO 查询结果结构体
type StatsAggResult struct {
	Total     int64
	Success   int64
	Errors    int64
	AvgDur    float64
	TotalIn   int64
	TotalOut  int64
	TotalCost float64
}

type CostAggResult struct {
	TotalCost float64
	TotalIn   int64
	TotalOut  int64
	TotalReqs int64
}

type DailyCostRow struct {
	Day         string
	DayCost     float64
	DayIn       int64
	DayOut      int64
	DayRequests int64
}

type ModelCostRow struct {
	ModelName string
	NodeCost  float64
	NodeIn    int64
	NodeOut   int64
	NodeReqs  int64
}

type IntentCostRow struct {
	TraceName  string
	IntentCost float64
	IntentReqs int64
}

type HourlyTokenRow struct {
	HourStr  string
	HourIn   int64
	HourOut  int64
	HourReqs int64
}
