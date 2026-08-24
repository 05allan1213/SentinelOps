// Package cache 实现会话历史的分层记忆架构：短期记忆（最近对话原文）+ 长期摘要（早期对话压缩）。
//
// 核心设计：
//   - 短期记忆（RecentMessages）：保留最近若干条原始消息，直接注入 Prompt，让 LLM 感知当前上下文
//   - 长期摘要（LongTermSummary）：早期对话超过阈值后，由 Summary Agent 压缩为一段摘要文本，
//     以 User 消息形式拼接在历史最前端，用远少于原文的 token 携带背景信息
//
// 双触发器机制（主/辅短路求值，满足任一即异步触发总结）：
//   - 主触发器（Token 预算）：token.EstimateMessages > tokenTrigger；主触发器命中时辅触发器不参与判断；
//     批次大小动态计算，裁剪至 tokenTrigger 以内
//   - 辅触发器（消息条数）：仅当 token 未超限时求值，len > summaryTrigger；固定批次 summaryBatchSize
//
// 数据流：
//
//	SetMessages → 追加消息 → token 超限？→ 是：触发总结
//	                                    → 否：条数超限？→ 是：触发总结
//	                                                   → 否：不触发
//	触发后 → go summarizeOldMessages → calcSummarizeCount → Summary Agent
//	       → 更新 LongTermSummary → 裁剪 RecentMessages → 保存到 Redis
package cache

import (
	"context"
	"sync"
	"sync/atomic"

	"SentinelOps/internal/ai/agent/summary_pipeline"
	"SentinelOps/internal/ai/token"
	aitrace "SentinelOps/internal/ai/trace"

	"github.com/cloudwego/eino/schema"
	"github.com/gogf/gf/v2/frame/g"
)

var (
	// 从配置文件加载的参数（懒初始化，仅执行一次）
	summaryTrigger    int // 消息条数触发阈值（辅助触发器）
	summaryBatchSize  int // 消息条数触发时每次总结的条数（固定批次）
	tokenTrigger      int // 短期记忆 token 预算（主触发器）
	minRecentMessages int // 总结后尾部至少保留的消息条数
	configOnce        sync.Once
)

// historySummaryPrefix 是长期摘要注入 Prompt 时的固定前缀，
// 用于让 LLM 明确区分"历史摘要"与"原始对话"。
const historySummaryPrefix = "【历史对话摘要】\n"

// SessionMemory 单个会话的分层记忆容器，持有短期记忆和长期摘要。
//
// 并发设计：
//   - mu 保护 RecentMessages / LongTermSummary 的读写
//   - summarizing 通过 CAS 保证同一会话只有一个总结 goroutine 在运行（0=空闲，1=总结中）
type SessionMemory struct {
	ID              string            `json:"id"`                // 会话 ID
	RecentMessages  []*schema.Message `json:"recent_messages"`   // 短期记忆（原始消息列表）
	LongTermSummary string            `json:"long_term_summary"` // 长期摘要文本
	Revision        int64             `json:"revision"`          // 内部状态版本号，每次状态变更递增
	summarizing     int32             // CAS 标志：0=空闲，1=总结中
	mu              sync.Mutex        // 保护 RecentMessages / LongTermSummary / Revision 的读写
}

// SessionSnapshot 表示会话记忆的一致性快照，用于跨流程传递时校验状态版本。
type SessionSnapshot struct {
	Recent   []*schema.Message
	Summary  string
	Revision int64
}

// SessionMemoryMap 全局会话记忆注册表，key 为会话 ID。
// 进程内存存储，服务重启后清空；由 Redis（SaveSessionMemory）负责存储。
// 并发访问须通过外层 mu 保护。
var SessionMemoryMap = make(map[string]*SessionMemory)

// mu 保护 SessionMemoryMap 的全局互斥锁。
// 双层锁设计：外层（此锁）保护 map 的增删操作，内层（SessionMemory.mu）保护单会话的字段读写。
var mu sync.Mutex

// GetSessionMemory 返回指定会话的记忆实例，不存在则新建并注册。
// 每次调用会触发 loadConfig（内部通过 sync.Once 保证只加载一次）。
func GetSessionMemory(id string) *SessionMemory {
	loadConfig(context.Background())

	mu.Lock()
	defer mu.Unlock()

	if mem, ok := SessionMemoryMap[id]; ok {
		return mem
	}

	newMem := &SessionMemory{
		ID:             id,
		RecentMessages: []*schema.Message{},
	}
	SessionMemoryMap[id] = newMem
	return newMem
}

// GetRecentMessages 线程安全地返回短期记忆消息列表。
func (c *SessionMemory) GetRecentMessages() []*schema.Message {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.RecentMessages
}

// GetLongTermSummary 线程安全地返回长期摘要文本。
func (c *SessionMemory) GetLongTermSummary() string {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.LongTermSummary
}

// GetRevision 线程安全地返回当前会话记忆版本号。
func (c *SessionMemory) GetRevision() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.Revision
}

// Snapshot 线程安全地返回会话记忆快照，并深拷贝消息对象，避免调用方修改内部状态。
func (c *SessionMemory) Snapshot() SessionSnapshot {
	c.mu.Lock()
	defer c.mu.Unlock()

	return SessionSnapshot{
		Recent:   cloneMessages(c.RecentMessages),
		Summary:  c.LongTermSummary,
		Revision: c.Revision,
	}
}

// SetState 从外部状态（通常是 Redis 恢复）整体覆写会话记忆。
// 用于服务重启后从持久化存储恢复上一次的 RecentMessages 和 LongTermSummary。
func (c *SessionMemory) SetState(recent []*schema.Message, summary string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.RecentMessages = recent
	c.LongTermSummary = summary
	c.Revision++
}

// BuildHistoryWithSummary 将长期摘要和短期记忆拼接为注入 Prompt 的 History 列表。
//
// 有摘要时结构为：[User("【历史对话摘要】\n"+summary), recent...]
// 无摘要时结构为：[recent...]
//
// 摘要以 User 消息承载（而非 System），是为了兼容部分不支持多 System 消息的模型。
func BuildHistoryWithSummary(recent []*schema.Message, summary string) []*schema.Message {
	history := make([]*schema.Message, 0, len(recent)+1)
	if summary != "" {
		history = append(history, &schema.Message{
			Role:    schema.User,
			Content: historySummaryPrefix + summary,
		})
	}
	history = append(history, recent...)
	return history
}

// cloneMessages 深拷贝消息对象，确保快照与内部记忆不共享消息指针。
func cloneMessages(messages []*schema.Message) []*schema.Message {
	cloned := make([]*schema.Message, len(messages))
	for i, msg := range messages {
		if msg == nil {
			continue
		}
		msgCopy := *msg
		cloned[i] = &msgCopy
	}
	return cloned
}

// SetMessages 追加一条消息到短期记忆，并在满足条件时异步触发历史总结。
//
// 双触发器（主/辅短路求值，同一会话同时只有一个总结 goroutine 在运行）：
//   - 主触发器（token 预算）：estimateTokens > tokenTrigger 时立即触发；命中后辅触发器不参与判断
//   - 辅触发器（消息条数）：仅当 token 未超限时求值，len > summaryTrigger 时作为兜底触发
//
// CAS（CompareAndSwap）保证并发追加消息时只有一个 goroutine 进入总结流程。
func (c *SessionMemory) SetMessages(msg *schema.Message) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.RecentMessages = append(c.RecentMessages, msg)
	c.Revision++

	// 主触发器优先：token 超限直接触发，辅触发器（条数）仅在 token 未超限时求值。
	tokenExceeded := tokenTrigger > 0 && token.EstimateMessages(c.RecentMessages) > tokenTrigger
	countExceeded := !tokenExceeded && len(c.RecentMessages) > summaryTrigger

	if (countExceeded || tokenExceeded) && atomic.CompareAndSwapInt32(&c.summarizing, 0, 1) {
		go c.summarizeOldMessages()
	}
}

// summarizeOldMessages 将短期记忆头部的早期消息压缩为长期摘要（异步执行，不阻塞请求链路）。
//
// 渐进式压缩策略：
//   - 只压缩"头部"若干条旧消息，尾部最近的 minRecentMessages 条始终保留，维持当前话题连贯性
//   - 每次压缩的条数由 calcSummarizeCount 决定：token 超限时动态计算，条数超限时取固定值
//   - 新摘要覆盖旧摘要（Summary Agent 的 Prompt 会将上一轮摘要纳入输入，实现滚动压缩）
//
// Trace 设计：
//   - 此方法在独立 goroutine 中运行，无 HTTP 请求上下文，无法挂载到已有 TraceRun。
//   - 通过 StartRun 创建独立链路（trace_name="memory.summarize"），记录 LLM 调用和 Redis 写入，
//     便于在 Traces 页面单独分析摘要任务的耗时和 Token 消耗。
//
// 锁操作说明：
//   - 提取消息副本后立即释放锁，避免持锁期间进行网络 I/O（调用 LLM）
//   - 写回结果时重新加锁，通过 defer 确保解锁
func (c *SessionMemory) summarizeOldMessages() {
	defer atomic.StoreInt32(&c.summarizing, 0)

	c.mu.Lock()
	if len(c.RecentMessages) <= minRecentMessages {
		c.mu.Unlock()
		return
	}

	summarizeCount := c.calcSummarizeCount()

	// 提取待总结消息的副本后立即释放锁，避免持锁调用 LLM（网络 I/O）
	messagesToSummarize := make([]*schema.Message, summarizeCount)
	copy(messagesToSummarize, c.RecentMessages[:summarizeCount])
	sessionID := c.ID
	c.mu.Unlock()

	// 为独立的后台摘要任务创建独立链路，便于在 Traces 页面单独分析摘要 Agent 耗时
	ctx := context.Background()
	var runErr error
	if aitrace.IsEnabled() {
		ctx = aitrace.StartRun(ctx, "memory.summarize", "background",
			sessionID, 0, "", map[string]any{"session_id": sessionID, "summarize_count": summarizeCount})
		defer func() { aitrace.FinishRun(ctx, runErr) }()
	}

	// 步骤 1：获取摘要 Agent（全局单例，懒初始化）
	summaryAgent, err := summary_pipeline.GetSummaryAgent(ctx)
	if err != nil {
		runErr = err
		g.Log().Errorf(ctx, "[SummaryAgent] 获取摘要 Agent 失败 | session=%s | err=%v", sessionID, err)
		return
	}

	// 步骤 2：执行摘要 Agent（DAG 流水线：格式化消息 → 渲染 Prompt → LLM → 提取摘要文本）
	input := &summary_pipeline.SummaryInput{
		SessionID: sessionID,
		Messages:  messagesToSummarize,
	}
	output, err := summaryAgent.Invoke(ctx, input)
	if err != nil {
		runErr = err
		g.Log().Errorf(ctx, "[SummaryAgent] 摘要生成失败 | session=%s | err=%v", sessionID, err)
		return
	}

	// 步骤 3：写回新摘要，裁剪已总结的头部消息
	c.mu.Lock()
	defer c.mu.Unlock()

	c.LongTermSummary = output.Summary
	// 裁剪头部已总结的消息，保留尾部最新的消息（当前话题上下文）
	if len(c.RecentMessages) > summarizeCount {
		c.RecentMessages = c.RecentMessages[summarizeCount:]
	}

	// 构造快照副本，防止异步持久化期间切片被修改
	currentRecent := make([]*schema.Message, len(c.RecentMessages))
	copy(currentRecent, c.RecentMessages)
	currentSummary := c.LongTermSummary

	g.Log().Infof(ctx, "[SummaryAgent] 摘要生成完成 | session=%s | summary_len=%d | remaining=%d",
		sessionID, len(output.Summary), len(c.RecentMessages))
	g.Log().Debugf(ctx, "[SummaryAgent] 摘要内容预览 | session=%s\n%s", sessionID, output.Summary)

	// 步骤 4：异步保存最新状态到 Redis（RecentMessages + LongTermSummary）
	go func(recent []*schema.Message, summary string, sid string) {
		bgCtx := context.Background()
		if err := SaveSession(bgCtx, sid, recent, summary); err != nil {
			g.Log().Errorf(bgCtx, "[SummaryAgent] 摘要状态持久化失败 | session=%s | err=%v", sid, err)
			return
		}
		g.Log().Infof(bgCtx, "[SummaryAgent] 摘要状态持久化成功 | session=%s | recent_len=%d | has_summary=%t",
			sid, len(recent), summary != "")
	}(currentRecent, currentSummary, sessionID)
}

// calcSummarizeCount 动态计算本次需要压缩（裁剪）的消息条数。
//
// 两条计算路径：
//  1. Token 超限路径：从头逐条扫描，找到最小裁剪数 i，使剩余 token 降至 tokenTrigger 以内。
//  2. 消息数路径：直接取 summaryBatchSize（固定值），上限为 maxSummarize。
//
// maxSummarize = total - minRecentMessages，确保尾部始终保留至少 minRecentMessages 条。
func (c *SessionMemory) calcSummarizeCount() int {
	total := len(c.RecentMessages)

	// 可总结的上限：尾部 minRecentMessages 条必须保留，不参与压缩
	maxSummarize := total - minRecentMessages
	if maxSummarize <= 0 {
		// 消息总数不足以在保留尾部的前提下再裁剪任何内容，跳过本次总结
		return 0
	}

	if tokenTrigger > 0 && token.EstimateMessages(c.RecentMessages) > tokenTrigger {
		// 从裁剪 1 条开始逐步尝试，找到满足目标的最小裁剪数（贪心：尽量少裁剪）
		for i := 1; i <= maxSummarize; i++ {
			if token.EstimateMessages(c.RecentMessages[i:]) <= tokenTrigger {
				return i
			}
		}
		// 即使裁剪到上限（保留 minRecentMessages 条），token 仍超过目标——全量裁剪
		return maxSummarize
	}

	// 消息数超限路径：使用固定批次 summaryBatchSize，但不超过 maxSummarize
	if summaryBatchSize < maxSummarize {
		return summaryBatchSize
	}
	return maxSummarize
}

// loadConfig 从配置文件加载记忆管理参数，通过 sync.Once 保证整个进程生命周期只执行一次。
// 所有参数附带合理性校验，配置缺失或非法时回退到内置默认值并打印警告日志。
func loadConfig(ctx context.Context) {
	configOnce.Do(func() {
		summaryTrigger = g.Cfg().MustGet(ctx, "memory.summaryTrigger").Int()
		summaryBatchSize = g.Cfg().MustGet(ctx, "memory.summaryBatchSize").Int()
		tokenTrigger = g.Cfg().MustGet(ctx, "memory.tokenTrigger").Int()
		minRecentMessages = g.Cfg().MustGet(ctx, "memory.minRecentMessages").Int()

		if summaryTrigger <= 0 {
			g.Log().Warningf(ctx, "[MemoryCache] summaryTrigger 配置无效，使用默认值 30 | got=%d", summaryTrigger)
			summaryTrigger = 30
		}
		if summaryBatchSize <= 0 {
			g.Log().Warningf(ctx, "[MemoryCache] summaryBatchSize 配置无效，使用默认值 10 | got=%d", summaryBatchSize)
			summaryBatchSize = 10
		}
		if tokenTrigger <= 0 {
			tokenTrigger = 3000
		}
		if minRecentMessages <= 0 {
			minRecentMessages = 4
		}

		// summaryBatchSize 须小于 summaryTrigger，否则每次总结后立即再次触发
		if summaryBatchSize >= summaryTrigger {
			g.Log().Warningf(ctx, "[MemoryCache] summaryBatchSize 超过 summaryTrigger，自动调整 | batchSize=%d | trigger=%d | adjusted=%d",
				summaryBatchSize, summaryTrigger, summaryTrigger/3)
			summaryBatchSize = summaryTrigger / 3
		}
		// minRecentMessages 须小于 summaryTrigger，否则永远无法触发总结
		if minRecentMessages >= summaryTrigger {
			g.Log().Warningf(ctx, "[MemoryCache] minRecentMessages 超过 summaryTrigger，重置为 4 | minRecent=%d | trigger=%d",
				minRecentMessages, summaryTrigger)
			minRecentMessages = 4
		}

		g.Log().Infof(ctx, "[MemoryCache] 初始化完成 | summaryTrigger=%d | summaryBatchSize=%d | tokenTrigger=%d | minRecentMessages=%d",
			summaryTrigger, summaryBatchSize, tokenTrigger, minRecentMessages)
	})
}
