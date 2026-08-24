// Package rageval RAG 检索质量评估服务：KPI 聚合 + 用户反馈 CRUD。
//
// # 背景与动机
//
// RAG（检索增强生成）系统的质量直接影响 AI 回答的准确性，但传统日志难以量化评估：
//   - 检索召回率低：Milvus 返回的文档与问题不相关，导致 LLM 回答偏离
//   - 缓存命中率不透明：语义缓存是否真正减少了 Embedding API 调用？
//   - 用户满意度未追踪：AI 回答质量好坏缺乏反馈闭环
//
// # 设计目标
//
//  1. 数据驱动优化：通过 KPI 仪表盘量化 RAG 系统表现（成功率、延迟、检索质量）
//  2. 问题快速定位：链路列表支持按状态过滤，点击查看详细 Trace 树，精准排查低质量回答
//  3. 用户反馈闭环：点赞/踩机制收集真实满意度，指导 Prompt 和检索策略迭代
//
// # 数据来源
//
// 本模块不直接执行 RAG 检索，而是从 trace 系统聚合已记录的链路数据：
//   - agent_trace_runs：请求级别指标（成功率、总耗时、Token 消耗）
//   - agent_trace_nodes：节点级别指标（RETRIEVER 节点的召回文档数、相似度分数）
//   - message_feedbacks：用户主动提交的点赞/踩反馈
//
// # 与 trace 系统的协作
//
// trace 系统（internal/ai/trace/）负责"记录"，rageval 负责"分析"：
//   - trace.StartRun/FinishRun 在每次请求时写入 TraceRun
//   - trace.Callback 自动捕获 RETRIEVER 节点的检索质量指标（avg_vector_score 等）
//   - rageval.GetDashboard 聚合这些原始数据，计算 KPI 并生成趋势图
package rageval

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"SentinelOps/internal/ai/cache"
	"SentinelOps/internal/ai/memory"
	dao "SentinelOps/internal/dao/mysql"
	redisdao "SentinelOps/internal/dao/redis"

	"github.com/cloudwego/eino/schema"
	"github.com/gogf/gf/v2/frame/g"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// ── Dashboard 指标 ──────────────────────────────────────────────────────────
//
// GetDashboard 从 trace 数据聚合 RAG 系统的关键性能指标（KPI）。
//
// 时间窗口（window 参数）：
//   - "24h"：最近 24 小时，按小时分组（适合实时监控）
//   - "7d"：最近 7 天，按天分组（适合周趋势分析）
//   - "30d"：最近 30 天，按天分组（适合月度报告）
//
// KPI 指标说明：
//   - SuccessRate：status='success' 的请求占比（0-1），反映系统稳定性
//   - AvgLatencyMs：平均响应耗时（毫秒），反映用户体验
//   - P95LatencyMs：95 分位延迟（毫秒），排除异常值后的典型慢请求耗时
//   - AvgRetrievedDocs：RETRIEVER 节点平均召回文档数，反映检索覆盖度
//   - AvgTopScore：RETRIEVER 节点平均最高相似度分（0-1），反映检索精准度
//
// 阈值判断（*Status 字段）：
//   - SuccessRate：>= 0.99 为 good，>= 0.95 为 warning，否则 bad
//   - P95LatencyMs：<= 5s 为 good，<= 15s 为 warning，否则 bad
//   这些阈值基于生产环境经验值，可通过配置文件调整（TODO）

// TrendPoint 单个时间点的趋势数据。
type TrendPoint struct {
	Timestamp    string  `json:"timestamp"`      // 格式：2006-01-02 15（小时）或 2006-01-02（天）
	SuccessRate  float64 `json:"success_rate"`   // 0-1
	AvgLatencyMs int64   `json:"avg_latency_ms"` // 平均延迟（毫秒）
}

// DashboardMetrics KPI 汇总指标。
type DashboardMetrics struct {
	// KPI 卡片
	SuccessRate      float64 `json:"success_rate"`
	AvgLatencyMs     int64   `json:"avg_latency_ms"`
	P95LatencyMs     int64   `json:"p95_latency_ms"`
	TotalRuns        int64   `json:"total_runs"`
	AvgRetrievedDocs float64 `json:"avg_retrieved_docs"` // P1: 平均召回文档数
	AvgTopScore      float64 `json:"avg_top_score"`      // P1: 平均最高相似度分

	// 阈值状态（good/warning/bad）
	SuccessRateStatus string `json:"success_rate_status"`
	LatencyStatus     string `json:"latency_status"`

	// 趋势数据
	Trends []TrendPoint `json:"trends"`
}

// GetDashboard 聚合指定时间窗口内的 RAG KPI。
// window：24h / 7d / 30d，缺省 24h。
func GetDashboard(ctx context.Context, window string) (*DashboardMetrics, error) {
	db, err := dao.DB(ctx)
	if err != nil {
		return nil, err
	}

	since, groupByHour := parseWindow(window)

	// ── 查 agent_trace_runs ──────────────────────────────────────────────────
	var runs []dao.TraceRun
	db.Where("start_time >= ?", since).Order("start_time asc").Find(&runs)

	total := int64(len(runs))
	var successCount int64
	var totalDurationMs int64
	durations := make([]int64, 0, len(runs))
	for _, r := range runs {
		if r.Status == "success" {
			successCount++
		}
		if r.DurationMs > 0 {
			totalDurationMs += r.DurationMs
			durations = append(durations, r.DurationMs)
		}
	}

	successRate := float64(0)
	avgLatencyMs := int64(0)
	p95LatencyMs := int64(0)
	if total > 0 {
		successRate = float64(successCount) / float64(total)
	}
	if len(durations) > 0 {
		avgLatencyMs = totalDurationMs / int64(len(durations))
		sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
		p95Idx := int(float64(len(durations)) * 0.95)
		if p95Idx >= len(durations) {
			p95Idx = len(durations) - 1
		}
		p95LatencyMs = durations[p95Idx]
	}

	// ── 查 agent_trace_nodes（RETRIEVER）──────────────────────────────────
	var retrieverNodes []dao.TraceNode
	db.Where("start_time >= ? AND node_type = ?", since, "RETRIEVER").Find(&retrieverNodes)

	var retrieverTotal int64
	var totalDocCount int64
	var totalTopScore float64
	for _, n := range retrieverNodes {
		retrieverTotal++
		totalDocCount += int64(n.DocCount)
		totalTopScore += n.MaxVectorScore
	}
	avgRetrievedDocs := float64(0)
	avgTopScore := float64(0)
	if retrieverTotal > 0 {
		avgRetrievedDocs = float64(totalDocCount) / float64(retrieverTotal)
		avgTopScore = totalTopScore / float64(retrieverTotal)
	}

	// ── 阈值判断 ─────────────────────────────────────────────────────────────
	metrics := &DashboardMetrics{
		SuccessRate:       successRate,
		AvgLatencyMs:      avgLatencyMs,
		P95LatencyMs:      p95LatencyMs,
		TotalRuns:         total,
		AvgRetrievedDocs:  avgRetrievedDocs,
		AvgTopScore:       avgTopScore,
		SuccessRateStatus: rateStatus(successRate, 0.99, 0.95),
		LatencyStatus:     latencyStatus(p95LatencyMs),
		Trends:            buildTrends(runs, since, groupByHour),
	}
	return metrics, nil
}

// ── Trace 链路列表 ─────────────────────────────────────────────────────────
//
// ListTraces 返回最近的 RAG 链路记录，支持分页和状态过滤。
//
// 设计要点：
//   1. 过滤事件分析链路（trace_name='event.pipeline'）：这类链路通常是后台批处理，
//      不代表用户交互质量，排除后 KPI 更准确反映真实用户体验。
//   2. 关联用户反馈：通过 (session_id, message_index) 精确匹配 message_feedbacks 表，
//      在列表中直接显示点赞/踩状态，方便快速识别低质量回答。
//   3. 分页限制：pageSize 最大 100，防止单次查询返回过多数据影响前端渲染性能。

// TraceItem 最近 RAG 链路简要信息。
type TraceItem struct {
	TraceID      string `json:"trace_id"`
	TraceName    string `json:"trace_name"`
	SessionID    string `json:"session_id"`
	Status       string `json:"status"`
	DurationMs   int64  `json:"duration_ms"`
	StartTime    string `json:"start_time"`
	FeedbackVote int    `json:"feedback_vote"` // 0=无反馈 1=点赞 -1=点踩
}

// ListTraces 返回分页的 trace 记录，支持 status 过滤。
func ListTraces(ctx context.Context, page, pageSize int, status string) ([]TraceItem, int64, error) {
	db, err := dao.DB(ctx)
	if err != nil {
		return nil, 0, err
	}
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 5
	}
	offset := (page - 1) * pageSize

	q := db.Model(&dao.TraceRun{})
	if status != "" {
		q = q.Where("status = ?", status)
	}
	// 过滤掉事件分析链路（event.pipeline）
	q = q.Where("trace_name != ?", "event.pipeline")

	var total int64
	q.Count(&total)

	var runs []dao.TraceRun
	q.Order("start_time desc").Offset(offset).Limit(pageSize).Find(&runs)

	// 关联 session 反馈（按 session_id + message_index 精确匹配）
	type feedbackKey struct {
		sessionID    string
		messageIndex int
	}
	feedbackMap := map[feedbackKey]int{}

	sessionIDs := make([]string, 0, len(runs))
	for _, r := range runs {
		if r.SessionID != "" {
			sessionIDs = append(sessionIDs, r.SessionID)
		}
	}

	if len(sessionIDs) > 0 {
		var feedbacks []dao.MessageFeedback
		db.Where("session_id IN ?", sessionIDs).Find(&feedbacks)
		for _, f := range feedbacks {
			key := feedbackKey{sessionID: f.SessionID, messageIndex: f.MessageIndex}
			feedbackMap[key] = f.Vote
		}
	}

	items := make([]TraceItem, len(runs))
	for i, r := range runs {
		key := feedbackKey{sessionID: r.SessionID, messageIndex: r.MessageIndex}
		items[i] = TraceItem{
			TraceID:      r.TraceID,
			TraceName:    r.TraceName,
			SessionID:    r.SessionID,
			Status:       r.Status,
			DurationMs:   r.DurationMs,
			StartTime:    r.StartTime.Format("2006-01-02 15:04:05"),
			FeedbackVote: feedbackMap[key],
		}
	}
	return items, total, nil
}

// DeleteTrace 删除指定 traceID 的链路记录（TraceRun + TraceNode 硬删除）。
func DeleteTrace(ctx context.Context, traceID string) error {
	db, err := dao.DB(ctx)
	if err != nil {
		return err
	}
	if err := db.Where("trace_id = ?", traceID).Delete(&dao.TraceRun{}).Error; err != nil {
		return err
	}
	return db.Unscoped().Where("trace_id = ?", traceID).Delete(&dao.TraceNode{}).Error
}

// ── 用户反馈 CRUD ──────────────────────────────────────────────────────────
//
// 用户反馈机制设计：
//
// 1. 反馈粒度：按 (session_id, message_index) 唯一标识一条 AI 回复
//    - session_id：对话会话 ID，跨多轮对话保持不变
//    - message_index：消息在会话中的序号（从 0 开始），精确定位某条回复
//
// 2. 反馈类型：
//    - vote=1：点赞（有帮助）
//    - vote=-1：点踩（没帮助）
//    - vote=0：取消反馈（删除记录）
//
// 3. Upsert 语义：同一 (session_id, message_index) 重复提交时更新 vote/reason，
//    而非插入新记录，保证一条回复只有一条反馈记录。
//
// 4. 反馈用途：
//    - 短期：在 Traces 列表中显示满意度，快速识别问题回答
//    - 长期：统计满意度趋势，指导 Prompt 优化和检索策略调整

// SubmitFeedback 提交消息点赞/踩。
// 同一 (session_id, message_index) 只保留最新一条，重复提交时更新 vote，
// 传入 vote=0 表示取消反馈（直接删除该条记录）。
// 踩（-1）时异步触发用户偏好重新推断，形成反馈闭环。
func SubmitFeedback(ctx context.Context, sessionID, userID string, messageIndex, vote int, reasons []string) error {
	db, err := dao.DB(ctx)
	if err != nil {
		return err
	}
	if vote == 0 {
		// 取消反馈：硬删除该条记录
		// 注意：偏好字段（output_style/analysis_depth/inferred_note）不回滚，
		// 偏好是长期画像，单次取消不足以推翻已积累的特征。
		if err := db.Unscoped().
			Where("session_id = ? AND message_index = ?", sessionID, messageIndex).
			Delete(&dao.MessageFeedback{}).Error; err != nil {
			g.Log().Warningf(ctx, "[Feedback] 取消反馈失败 | session=%s | idx=%d | err=%v", sessionID, messageIndex, err)
			return err
		}
		g.Log().Infof(ctx, "[Feedback] 取消反馈 | session=%s | idx=%d | user=%s", sessionID, messageIndex, userID)
		return nil
	}
	// Upsert：已存在则更新 vote/reason，不存在则插入
	var existing dao.MessageFeedback
	err = db.Session(&gorm.Session{Logger: db.Logger.LogMode(logger.Silent)}).
		Where("session_id = ? AND message_index = ?", sessionID, messageIndex).
		First(&existing).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	reasonsJSON := marshalReasons(reasons)
	if err == nil {
		if updateErr := db.Model(&existing).Updates(map[string]any{
			"vote":    vote,
			"reasons": reasonsJSON,
			"user_id": userID,
		}).Error; updateErr != nil {
			g.Log().Warningf(ctx, "[Feedback] 更新反馈失败 | session=%s | idx=%d | vote=%d | err=%v", sessionID, messageIndex, vote, updateErr)
			return updateErr
		}
	} else if err := db.Create(&dao.MessageFeedback{
		UserID:       userID,
		SessionID:    sessionID,
		MessageIndex: messageIndex,
		Vote:         vote,
		Reasons:      reasonsJSON,
	}).Error; err != nil {
		g.Log().Warningf(ctx, "[Feedback] 写入反馈失败 | session=%s | idx=%d | vote=%d | err=%v", sessionID, messageIndex, vote, err)
		return err
	}

	voteLabel := "点赞"
	if vote == -1 {
		voteLabel = "点踩"
	}
	g.Log().Infof(ctx, "[Feedback] %s | session=%s | idx=%d | user=%s | reasons=%v", voteLabel, sessionID, messageIndex, userID, reasons)

	// 踩信号 → 按 reason 标签分流处理偏好（反馈闭环，update/create 均触发）
	if vote == -1 && userID != "" {
		go handleDislikePreference(context.Background(), sessionID, userID, reasons)
	}
	return nil
}

// handleDislikePreference 根据踩的原因标签更新用户偏好。
// 明确标签（风格/深度类）直接写字段，无需 LLM；内容质量类不影响偏好；兜底走 LLM 推断。
func handleDislikePreference(ctx context.Context, sessionID, userID string, reasons []string) {
	db, err := dao.DB(ctx)
	if err != nil {
		return
	}

	updates := map[string]any{}
	for _, reason := range reasons {
		switch reason {
		case "too_verbose":
			updates["output_style"] = "concise"
		case "too_brief":
			updates["output_style"] = "detailed"
		case "too_technical":
			updates["analysis_depth"] = "quick"
		case "not_technical_enough":
			updates["analysis_depth"] = "deep"
		}
	}
	// 仅内容质量标签（inaccurate/off_topic）时不影响偏好，走 LLM 推断

	if len(updates) > 0 {
		// 明确标签：直接 upsert 偏好字段，零 LLM 开销
		res := db.Model(&dao.UserPreference{}).Where("user_id = ?", userID).Updates(updates)
		if res.Error != nil {
			return
		}
		if res.RowsAffected == 0 {
			row := dao.UserPreference{UserID: userID, FocusAreas: "[]"}
			if v, ok := updates["output_style"].(string); ok {
				row.OutputStyle = v
			}
			if v, ok := updates["analysis_depth"].(string); ok {
				row.AnalysisDepth = v
			}
			db.Create(&row)
		}
		memory.InvalidateCache(userID)
		return
	}

	// 兜底：无明确标签，走 LLM 推断
	recent, summary, loadErr := loadRecentForInfer(ctx, sessionID)
	if loadErr != nil || len(recent) == 0 {
		return
	}
	msgs := cache.BuildHistoryWithSummary(recent, summary)
	memory.TriggerInferPreference(ctx, userID, msgs)
}

// loadRecentForInfer 从 Redis 加载会话历史，供踩信号触发偏好推断使用
func loadRecentForInfer(ctx context.Context, sessionID string) ([]*schema.Message, string, error) {
	return redisdao.LoadSession(ctx, sessionID)
}

// marshalReasons 将 reasons 序列化为 JSON 字符串存入数据库
func marshalReasons(reasons []string) string {
	if len(reasons) == 0 {
		return "[]"
	}
	b, _ := json.Marshal(reasons)
	return string(b)
}

// FeedbackStats 反馈统计结果。
type FeedbackStats struct {
	LikeRate    float64          `json:"like_rate"`    // 0-1
	DislikeRate float64          `json:"dislike_rate"` // 0-1
	NoVoteRate  float64          `json:"no_vote_rate"` // 0-1（仅基于有反馈数据计算）
	Total       int64            `json:"total"`
	Recent      []RecentFeedback `json:"recent"`
}

// RecentFeedback 单条反馈摘要。
type RecentFeedback struct {
	SessionID    string `json:"session_id"`
	MessageIndex int    `json:"message_index"`
	Vote         int    `json:"vote"`
	Reason       string `json:"reason,omitempty"`
	CreatedAt    string `json:"created_at"`
}

// GetFeedbackStats 统计反馈数据。
func GetFeedbackStats(ctx context.Context) (*FeedbackStats, error) {
	db, err := dao.DB(ctx)
	if err != nil {
		return nil, err
	}

	var feedbacks []dao.MessageFeedback
	db.Order("created_at desc").Find(&feedbacks)

	var likes, dislikes int64
	for _, f := range feedbacks {
		if f.Vote == 1 {
			likes++
		} else if f.Vote == -1 {
			dislikes++
		}
	}
	total := int64(len(feedbacks))

	likeRate, dislikeRate := float64(0), float64(0)
	if total > 0 {
		likeRate = float64(likes) / float64(total)
		dislikeRate = float64(dislikes) / float64(total)
	}

	// 按 vote 分组合并：统计各类反馈数量，取最近 3 条有 reason 的记录展示
	type voteGroup struct {
		vote    int
		count   int
		samples []dao.MessageFeedback
	}
	groups := map[int]*voteGroup{
		1:  {vote: 1},
		-1: {vote: -1},
	}
	for _, f := range feedbacks {
		if g, ok := groups[f.Vote]; ok {
			g.count++
			if len(g.samples) < 3 {
				g.samples = append(g.samples, f)
			}
		}
	}
	var recentItems []RecentFeedback
	for _, vote := range []int{1, -1} {
		g := groups[vote]
		if g.count == 0 {
			continue
		}
		label := "有帮助"
		if vote == -1 {
			label = "没帮助"
		}
		// 找第一条有 reasons 的样本作为代表，否则用默认文案
		reason := ""
		for _, s := range g.samples {
			if s.Reasons != "" {
				reason = s.Reasons
				break
			}
		}
		if reason == "" {
			reason = label
		}
		createdAt := ""
		if len(g.samples) > 0 {
			createdAt = g.samples[0].CreatedAt.Format("2006-01-02 15:04:05")
		}
		recentItems = append(recentItems, RecentFeedback{
			Vote:      vote,
			Reason:    fmt.Sprintf("%s（共 %d 条）", reason, g.count),
			CreatedAt: createdAt,
		})
	}

	return &FeedbackStats{
		LikeRate:    likeRate,
		DislikeRate: dislikeRate,
		NoVoteRate:  1 - likeRate - dislikeRate,
		Total:       total,
		Recent:      recentItems,
	}, nil
}

// ── 辅助函数 ───────────────────────────────────────────────────────────────

func parseWindow(window string) (since time.Time, byHour bool) {
	now := time.Now()
	switch window {
	case "7d":
		return now.Add(-7 * 24 * time.Hour), false
	case "30d":
		return now.Add(-30 * 24 * time.Hour), false
	default: // 24h
		return now.Add(-24 * time.Hour), true
	}
}

func buildTrends(runs []dao.TraceRun, since time.Time, byHour bool) []TrendPoint {
	type bucket struct {
		total   int
		success int
		latSum  int64
	}
	buckets := map[string]*bucket{}

	for _, r := range runs {
		var key string
		if byHour {
			key = r.StartTime.Format("01-02 15:00")
		} else {
			key = r.StartTime.Format("2006-01-02")
		}
		b := buckets[key]
		if b == nil {
			b = &bucket{}
			buckets[key] = b
		}
		b.total++
		if r.Status == "success" {
			b.success++
		}
		b.latSum += r.DurationMs
	}

	// 填充时间轴（无数据点也输出）
	now := time.Now()
	var keys []string
	if byHour {
		for t := since; t.Before(now); t = t.Add(time.Hour) {
			keys = append(keys, t.Format("01-02 15:00"))
		}
	} else {
		for t := since; t.Before(now); t = t.Add(24 * time.Hour) {
			keys = append(keys, t.Format("2006-01-02"))
		}
	}

	points := make([]TrendPoint, 0, len(keys))
	for _, k := range keys {
		b := buckets[k]
		if b == nil {
			points = append(points, TrendPoint{Timestamp: k, SuccessRate: 0, AvgLatencyMs: 0})
			continue
		}
		sr := float64(0)
		if b.total > 0 {
			sr = float64(b.success) / float64(b.total)
		}
		avg := int64(0)
		if b.success > 0 {
			avg = b.latSum / int64(b.total)
		}
		points = append(points, TrendPoint{
			Timestamp:    k,
			SuccessRate:  sr,
			AvgLatencyMs: avg,
		})
	}
	return points
}

func rateStatus(v, good, warn float64) string {
	if v >= good {
		return "good"
	}
	if v >= warn {
		return "warning"
	}
	return "bad"
}

func latencyStatus(p95Ms int64) string {
	if p95Ms <= 5000 {
		return "good"
	}
	if p95Ms <= 15000 {
		return "warning"
	}
	return "bad"
}

// ── Trace 详情（P0）─────────────────────────────────────────────────────────

// TraceNodeItem 节点树中的单个节点。
type TraceNodeItem struct {
	NodeID            string  `json:"node_id"`
	ParentNodeID      string  `json:"parent_node_id,omitempty"`
	Depth             int     `json:"depth"`
	NodeType          string  `json:"node_type"`
	NodeName          string  `json:"node_name"`
	Status            string  `json:"status"`
	DurationMs        int64   `json:"duration_ms"`
	ErrorMessage      string  `json:"error_message,omitempty"`
	ModelName         string  `json:"model_name,omitempty"`
	InputTokens       int     `json:"input_tokens,omitempty"`
	CachedInputTokens int     `json:"cached_input_tokens,omitempty"`
	OutputTokens      int     `json:"output_tokens,omitempty"`
	ReasoningTokens   int     `json:"reasoning_tokens,omitempty"`
	CostCNY           float64 `json:"cost_cny,omitempty"`
	CacheHit          bool    `json:"cache_hit,omitempty"`
	// RETRIEVER 专属
	FinalTopK      int             `json:"final_top_k,omitempty"`
	DocCount       int             `json:"doc_count,omitempty"`
	AvgVectorScore float64         `json:"avg_vector_score,omitempty"`
	MaxVectorScore float64         `json:"max_vector_score,omitempty"`
	RerankUsed     bool            `json:"rerank_used,omitempty"`
	AvgRerankScore float64         `json:"avg_rerank_score,omitempty"`
	RetrievedDocs  string          `json:"retrieved_docs,omitempty"`
	Children       []TraceNodeItem `json:"children,omitempty"`
}

// TraceDetail Trace 详情。
type TraceDetail struct {
	TraceID           string          `json:"trace_id"`
	TraceName         string          `json:"trace_name"`
	SessionID         string          `json:"session_id"`
	QueryText         string          `json:"query_text"`
	Status            string          `json:"status"`
	DurationMs        int64           `json:"duration_ms"`
	TotalInputTokens  int             `json:"total_input_tokens"`
	CachedInputTokens int             `json:"cached_input_tokens"`
	TotalOutputTokens int             `json:"total_output_tokens"`
	ReasoningTokens   int             `json:"reasoning_tokens"`
	EstimatedCostCNY  float64         `json:"estimated_cost_cny"`
	StartTime         string          `json:"start_time"`
	Nodes             []TraceNodeItem `json:"nodes"`
	FeedbackVote      int             `json:"feedback_vote"`
}

// GetTraceDetail 返回指定 traceID 的完整链路详情（含节点树）。
func GetTraceDetail(ctx context.Context, traceID string) (*TraceDetail, error) {
	db, err := dao.DB(ctx)
	if err != nil {
		return nil, err
	}

	var run dao.TraceRun
	if err := db.Where("trace_id = ?", traceID).First(&run).Error; err != nil {
		return nil, fmt.Errorf("trace not found: %w", err)
	}

	var nodes []dao.TraceNode
	db.Where("trace_id = ?", traceID).Order("depth asc, start_time asc").Find(&nodes)

	// 构建节点树
	nodeMap := map[string]*TraceNodeItem{}
	for i := range nodes {
		n := &nodes[i]
		item := &TraceNodeItem{
			NodeID:            n.NodeID,
			ParentNodeID:      n.ParentNodeID,
			Depth:             n.Depth,
			NodeType:          n.NodeType,
			NodeName:          n.NodeName,
			Status:            n.Status,
			DurationMs:        n.DurationMs,
			ErrorMessage:      n.ErrorMessage,
			ModelName:         n.ModelName,
			InputTokens:       n.InputTokens,
			CachedInputTokens: n.CachedInputTokens,
			OutputTokens:      n.OutputTokens,
			ReasoningTokens:   n.ReasoningTokens,
			CostCNY:           n.CostCNY,
			CacheHit:          n.CacheHit,
			FinalTopK:         n.FinalTopK,
			DocCount:          n.DocCount,
			AvgVectorScore:    n.AvgVectorScore,
			MaxVectorScore:    n.MaxVectorScore,
			RerankUsed:        n.RerankUsed,
			AvgRerankScore:    n.AvgRerankScore,
			RetrievedDocs:     n.RetrievedDocs,
		}
		nodeMap[n.NodeID] = item
	}

	var roots []TraceNodeItem
	for i := range nodes {
		n := &nodes[i]
		item := nodeMap[n.NodeID]
		if n.ParentNodeID == "" {
			roots = append(roots, *item)
		} else if parent, ok := nodeMap[n.ParentNodeID]; ok {
			parent.Children = append(parent.Children, *item)
		} else {
			roots = append(roots, *item)
		}
	}

	// P2: 关联反馈
	feedbackVote := 0
	if run.SessionID != "" {
		var fb dao.MessageFeedback
		err := db.Where("session_id = ? AND message_index = ?", run.SessionID, run.MessageIndex).First(&fb).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("query feedback: %w", err)
		}
		if err == nil {
			feedbackVote = fb.Vote
		}
	}

	return &TraceDetail{
		TraceID:           run.TraceID,
		TraceName:         run.TraceName,
		SessionID:         run.SessionID,
		QueryText:         run.QueryText,
		Status:            run.Status,
		DurationMs:        run.DurationMs,
		TotalInputTokens:  run.TotalInputTokens,
		CachedInputTokens: run.CachedInputTokens,
		TotalOutputTokens: run.TotalOutputTokens,
		ReasoningTokens:   run.ReasoningTokens,
		EstimatedCostCNY:  run.EstimatedCostCNY,
		StartTime:         run.StartTime.Format("2006-01-02 15:04:05"),
		Nodes:             roots,
		FeedbackVote:      feedbackVote,
	}, nil
}
