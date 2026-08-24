// Package v1 RAG 质量评估 API 路由定义。
package v1

import "github.com/gogf/gf/v2/frame/g"

// DashboardReq KPI 汇总请求
type DashboardReq struct {
	g.Meta `path:"/rageval/v1/dashboard" method:"GET" tags:"RagEval" summary:"RAG质量KPI汇总"`
	Window string `json:"window" p:"window" d:"24h"` // 可选时间窗口：24h / 7d / 30d
}

// DashboardRes KPI 汇总响应
type DashboardRes struct {
	SuccessRate       float64     `json:"success_rate"`
	AvgLatencyMs      int64       `json:"avg_latency_ms"`
	P95LatencyMs      int64       `json:"p95_latency_ms"`
	CacheHitRate      float64     `json:"cache_hit_rate"`
	NoDocRate         float64     `json:"no_doc_rate"`
	TotalRuns         int64       `json:"total_runs"`
	AvgRetrievedDocs  float64     `json:"avg_retrieved_docs"` // P1: 平均召回文档数
	AvgTopScore       float64     `json:"avg_top_score"`      // P1: 平均最高相似度分
	SuccessRateStatus string      `json:"success_rate_status"`
	LatencyStatus     string      `json:"latency_status"`
	NoDocRateStatus   string      `json:"no_doc_rate_status"`
	Trends            interface{} `json:"trends"`
}

// TracesReq 最近 RAG 链路列表请求
type TracesReq struct {
	g.Meta   `path:"/rageval/v1/traces" method:"GET" tags:"RagEval" summary:"最近RAG链路列表"`
	Page     int    `json:"page" p:"page" d:"1"`
	PageSize int    `json:"page_size" p:"page_size" d:"5"`
	Status   string `json:"status" p:"status"` // 过滤 success/error
}

// TracesRes 最近 RAG 链路列表响应
type TracesRes struct {
	List  interface{} `json:"list"`
	Total int64       `json:"total"`
}

// TraceDetailReq Trace 详情请求（P0）
type TraceDetailReq struct {
	g.Meta  `path:"/rageval/v1/traces/detail" method:"GET" tags:"RagEval" summary:"Trace链路详情"`
	TraceID string `json:"trace_id" p:"trace_id" v:"required#trace_id不能为空"`
}

// TraceDetailRes Trace 详情响应（P0）
type TraceDetailRes struct {
	TraceID           string      `json:"trace_id"`
	TraceName         string      `json:"trace_name"`
	SessionID         string      `json:"session_id"`
	QueryText         string      `json:"query_text"`
	Status            string      `json:"status"`
	DurationMs        int64       `json:"duration_ms"`
	TotalInputTokens  int         `json:"total_input_tokens"`
	CachedInputTokens int         `json:"cached_input_tokens"`
	TotalOutputTokens int         `json:"total_output_tokens"`
	ReasoningTokens   int         `json:"reasoning_tokens"`
	EstimatedCostCNY  float64     `json:"estimated_cost_cny"`
	StartTime         string      `json:"start_time"`
	Nodes             interface{} `json:"nodes"`         // []TraceNodeItem 树形
	FeedbackVote      int         `json:"feedback_vote"` // P2: 关联反馈
}

// DeleteTraceReq 删除 Trace 请求
type DeleteTraceReq struct {
	g.Meta  `path:"/rageval/v1/traces" method:"DELETE" tags:"RagEval" summary:"删除Trace链路"`
	TraceID string `json:"trace_id" v:"required#trace_id不能为空"`
}

// DeleteTraceRes 删除 Trace 响应
type DeleteTraceRes struct{}

type FeedbackReq struct {
	g.Meta       `path:"/rageval/v1/feedback" method:"POST" tags:"RagEval" summary:"提交消息反馈"`
	UserID       string   `json:"user_id"` // 兼容旧客户端；服务端忽略此字段并只使用 JWT Identity
	SessionID    string   `json:"session_id" v:"required#session_id不能为空"`
	MessageIndex int      `json:"message_index"`
	Vote         int      `json:"vote" v:"required|in:1,-1,0#vote不能为空|vote必须为1、-1或0"`
	Reasons      []string `json:"reasons"` // 多选标签，如 ["too_verbose","inaccurate"]
}

// FeedbackRes 提交用户反馈响应
type FeedbackRes struct{}

// FeedbackStatsReq 反馈统计请求
type FeedbackStatsReq struct {
	g.Meta `path:"/rageval/v1/feedback_stats" method:"GET" tags:"RagEval" summary:"反馈统计"`
}

// FeedbackStatsRes 反馈统计响应
type FeedbackStatsRes struct {
	LikeRate    float64     `json:"like_rate"`
	DislikeRate float64     `json:"dislike_rate"`
	NoVoteRate  float64     `json:"no_vote_rate"`
	Total       int64       `json:"total"`
	Recent      interface{} `json:"recent"`
}
