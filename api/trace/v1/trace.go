// Package v1 定义链路追踪 API 的请求/响应类型。
package v1

import "github.com/gogf/gf/v2/frame/g"

// ListReq 链路运行记录列表请求
type ListReq struct {
	g.Meta    `path:"/trace/v1/list" method:"GET"`
	Page      int    `json:"page"      d:"1"`
	PageSize  int    `json:"pageSize"  d:"20"`
	Status    string `json:"status"`
	TraceId   string `json:"traceId"`
	SessionId string `json:"sessionId"`
}

// ListRes 链路运行记录列表响应
type ListRes struct {
	Total int64        `json:"total"`
	List  []TraceRunVO `json:"list"`
}

// TraceRunVO 链路运行记录 VO（视图对象）
type TraceRunVO struct {
	TraceId           string  `json:"traceId"`
	TraceName         string  `json:"traceName"`
	EntryPoint        string  `json:"entryPoint"`
	Status            string  `json:"status"`
	DurationMs        int64   `json:"durationMs"`
	StartTime         string  `json:"startTime"`
	SessionId         string  `json:"sessionId"`
	QueryText         string  `json:"queryText"`
	TotalInputTokens  int     `json:"totalInputTokens"`
	CachedInputTokens int     `json:"cachedInputTokens"`
	TotalOutputTokens int     `json:"totalOutputTokens"`
	ReasoningTokens   int     `json:"reasoningTokens"`
	EstimatedCostCNY  float64 `json:"estimatedCostCny"`
	ErrorCode         string  `json:"errorCode,omitempty"`
	ErrorMessage      string  `json:"errorMessage,omitempty"`
	Tags              string  `json:"tags"`
}

// DetailReq 链路详情请求
type DetailReq struct {
	g.Meta  `path:"/trace/v1/detail" method:"GET"`
	TraceId string `json:"traceId" v:"required"`
}

// DetailRes 链路详情响应（包含节点树）
type DetailRes struct {
	TraceRunVO
	Nodes []TraceNodeVO `json:"nodes"`
}

// TraceNodeVO 链路节点 VO
type TraceNodeVO struct {
	NodeId       string `json:"nodeId"`
	ParentNodeId string `json:"parentNodeId"`
	NodeType     string `json:"nodeType"`
	NodeName     string `json:"nodeName"`
	Depth        int    `json:"depth"`
	Status       string `json:"status"`
	DurationMs   int64  `json:"durationMs"`
	StartTime    string `json:"startTime"`
	ErrorMessage string `json:"errorMessage,omitempty"`
	ErrorCode    string `json:"errorCode,omitempty"`
	ErrorType    string `json:"errorType,omitempty"`
	// LLM 专属
	ModelName         string  `json:"modelName,omitempty"`
	InputTokens       int     `json:"inputTokens,omitempty"`
	CachedInputTokens int     `json:"cachedInputTokens,omitempty"`
	OutputTokens      int     `json:"outputTokens,omitempty"`
	ReasoningTokens   int     `json:"reasoningTokens,omitempty"`
	CostCNY           float64 `json:"costCny,omitempty"`
	CompletionText    string  `json:"completionText,omitempty"`
	// RETRIEVER 专属
	QueryText     string `json:"queryText,omitempty"`
	RetrievedDocs string `json:"retrievedDocs,omitempty"`
	FinalTopK     int    `json:"finalTopK,omitempty"`
	CacheHit      bool   `json:"cacheHit,omitempty"`
	// 通用
	Metadata string `json:"metadata,omitempty"`
}

// StatsReq 链路统计请求
type StatsReq struct {
	g.Meta `path:"/trace/v1/stats" method:"GET"`
	Days   int `json:"days" d:"7"` // 最近 N 天
}

// StatsRes 链路统计响应
type StatsRes struct {
	TotalRuns         int64   `json:"totalRuns"`
	SuccessRuns       int64   `json:"successRuns"`
	ErrorRuns         int64   `json:"errorRuns"`
	AvgDurationMs     float64 `json:"avgDurationMs"`
	P95DurationMs     int64   `json:"p95DurationMs"`
	TotalInputTokens  int64   `json:"totalInputTokens"`
	TotalOutputTokens int64   `json:"totalOutputTokens"`
	TotalCostCNY      float64 `json:"totalCostCny"`
	AvgCostCNY        float64 `json:"avgCostCny"`
	ErrorRate         float64 `json:"errorRate"` // 0~1
}

// BatchDeleteReq 批量删除链路请求
type BatchDeleteReq struct {
	g.Meta   `path:"/trace/v1/batch_delete" method:"DELETE"`
	TraceIds []string `json:"traceIds" v:"required|min-length:1"`
}

// BatchDeleteRes 批量删除响应
type BatchDeleteRes struct {
	Deleted int64 `json:"deleted"` // 实际删除条数
}

// ExportReq 导出单条链路为 JSON
type ExportReq struct {
	g.Meta  `path:"/trace/v1/export" method:"GET"`
	TraceId string `json:"traceId" v:"required"`
}

// ExportRes 导出响应（直接写文件流，框架不包装）
type ExportRes struct{}

// SessionTimelineReq 会话维度链路时间线请求
type SessionTimelineReq struct {
	g.Meta    `path:"/trace/v1/session_timeline" method:"GET"`
	SessionId string `json:"sessionId" v:"required"`
}

// SessionTimelineSummary 时间线单条摘要
type SessionTimelineSummary struct {
	TraceId    string `json:"traceId"`
	QueryText  string `json:"queryText"`
	Status     string `json:"status"`
	DurationMs int64  `json:"durationMs"`
	StartTime  string `json:"startTime"`
	ErrorCode  string `json:"errorCode,omitempty"`
}

// SessionTimelineRes 会话时间线响应
type SessionTimelineRes struct {
	SessionId string                   `json:"sessionId"`
	Total     int                      `json:"total"`
	Runs      []SessionTimelineSummary `json:"runs"`
}

// ── 成本监控 API ──────────────────────────────────────────────────────────────

// CostOverviewReq 成本概览请求（支持自定义时间范围）
type CostOverviewReq struct {
	g.Meta    `path:"/trace/v1/cost/overview" method:"GET"`
	Days      int    `json:"days"      d:"30"` // 时间范围（天）
	StartDate string `json:"startDate"`        // 可选：YYYY-MM-DD 精确起止
	EndDate   string `json:"endDate"`
}

// CostOverviewRes 成本概览响应
type CostOverviewRes struct {
	// 汇总数字
	TotalCostCNY      float64 `json:"totalCostCny"`
	TotalInputTokens  int64   `json:"totalInputTokens"`
	TotalOutputTokens int64   `json:"totalOutputTokens"`
	TotalRequests     int64   `json:"totalRequests"`
	AvgCostPerReq     float64 `json:"avgCostPerReq"`
	// 同比（相同时长的上一周期）
	PrevTotalCostCNY float64 `json:"prevTotalCostCny"`
	CostChangePct    float64 `json:"costChangePct"` // 正数=上涨，负数=下降
	// 每日趋势
	DailyTrend []DailyCostPoint `json:"dailyTrend"`
	// 模型分布
	ModelBreakdown []ModelCostItem `json:"modelBreakdown"`
	// 意图分布
	IntentBreakdown []IntentCostItem `json:"intentBreakdown"`
}

// DailyCostPoint 每日成本数据点
type DailyCostPoint struct {
	Date         string  `json:"date"`
	CostCNY      float64 `json:"costCny"`
	InputTokens  int64   `json:"inputTokens"`
	OutputTokens int64   `json:"outputTokens"`
	RequestCount int64   `json:"requestCount"`
}

// ModelCostItem 模型维度成本拆分
type ModelCostItem struct {
	ModelName    string  `json:"modelName"`
	TotalCostCNY float64 `json:"totalCostCny"`
	InputTokens  int64   `json:"inputTokens"`
	OutputTokens int64   `json:"outputTokens"`
	RequestCount int64   `json:"requestCount"`
	CostPct      float64 `json:"costPct"` // 占总成本百分比
}

// IntentCostItem 意图/链路类型维度成本拆分
type IntentCostItem struct {
	TraceName    string  `json:"traceName"`
	TotalCostCNY float64 `json:"totalCostCny"`
	RequestCount int64   `json:"requestCount"`
	AvgCostCNY   float64 `json:"avgCostCny"`
	CostPct      float64 `json:"costPct"`
}

// TokenTrendReq 实时 Token 趋势（最近 N 小时，细粒度）
type TokenTrendReq struct {
	g.Meta `path:"/trace/v1/cost/token_trend" method:"GET"`
	Hours  int `json:"hours" d:"24"` // 最近 N 小时
}

// TokenTrendPoint 小时粒度数据点
type TokenTrendPoint struct {
	Hour         string `json:"hour"` // "YYYY-MM-DD HH"
	InputTokens  int64  `json:"inputTokens"`
	OutputTokens int64  `json:"outputTokens"`
	RequestCount int64  `json:"requestCount"`
}

// TokenTrendRes 实时 Token 趋势响应
type TokenTrendRes struct {
	Points []TokenTrendPoint `json:"points"`
}

// ExportSessionSnapshotReq 导出会话对话快照请求（实时从 Redis 读取）
type ExportSessionSnapshotReq struct {
	g.Meta    `path:"/trace/v1/export_session_snapshot" method:"GET"`
	SessionId string `json:"sessionId" v:"required"`
}

// ExportSessionSnapshotRes 导出会话快照响应（直接写文件流）
type ExportSessionSnapshotRes struct{}
