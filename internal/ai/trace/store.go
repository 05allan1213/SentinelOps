package trace

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"SentinelOps/internal/ai/policy"
	appconfig "SentinelOps/internal/config"
	dao "SentinelOps/internal/dao/mysql"
	"SentinelOps/utility/stringutil"

	"github.com/gogf/gf/v2/frame/g"
	"gorm.io/gorm/clause"
)

// ── 配置设计 ──────────────────────────────────────────────────────────────────
//
// trace 系统的配置仅在首次请求时从 g.Cfg() 读取一次（sync.Once 单例），
// 避免每次请求都走配置热读路径，同时保持与 GoFrame 配置体系的兼容性。
//
// 配置项（manifest/config/config.yaml 中的 trace: 块）：
//
//	trace:
//	  enabled: true              # 总开关，false 时所有 trace API 为空操作
//	  max_error_length: 1000     # 错误消息截断长度（防止超长错误撑大 DB 行）
//	  record_prompt: false       # 是否记录 LLM completion 文本（含 PII 风险，默认关）
//	  model_pricing:             # 各模型单价（¥/1M tokens，直接填人民币）
//	    vendor-model:
//	      input: 2.0
//	      output: 8.0

// Config trace 系统运行时配置，由 GetConfig() 初始化并缓存
type Config struct {
	Enabled           bool
	MaxErrorLength    int
	RecordPrompt      bool
	RecordSQL         bool
	DBSlowThresholdMs int64
}

var (
	cfgOnce sync.Once
	cfg     *Config
)

// GetConfig 返回 trace 配置单例，首次调用时从 GoFrame 配置中心初始化。
// 后续调用直接返回缓存值，无锁读（sync.Once 保证初始化完成后只读）。
func GetConfig() *Config {
	cfgOnce.Do(func() {
		ctx := context.Background()
		enabled, _ := g.Cfg().Get(ctx, "trace.enabled")
		maxErrLen, _ := g.Cfg().Get(ctx, "trace.max_error_length")
		recordPrompt, _ := g.Cfg().Get(ctx, "trace.record_prompt")
		recordSQL, _ := g.Cfg().Get(ctx, "trace.record_sql")
		dbSlowThreshold, _ := g.Cfg().Get(ctx, "trace.db_slow_threshold_ms")

		maxLen := maxErrLen.Int()
		if maxLen <= 0 {
			maxLen = 1000
		}

		slowThreshold := dbSlowThreshold.Int64()
		if slowThreshold <= 0 {
			slowThreshold = 100
		}

		cfg = &Config{
			Enabled:           enabled.Bool(),
			MaxErrorLength:    maxLen,
			RecordPrompt:      recordPrompt.Bool(),
			RecordSQL:         recordSQL.Bool(),
			DBSlowThresholdMs: slowThreshold,
		}
	})
	return cfg
}

// IsEnabled 快速判断 trace 是否启用，供各埋点入口做快速路径判断（inline 友好）
func IsEnabled() bool {
	return GetConfig().Enabled
}

// ── 错误分类 ──────────────────────────────────────────────────────────────────
//
// classifyError 通过字符串匹配将原始错误归类为标准错误码。
// 目的：前端 Traces 页面可按 error_code 过滤，快速定位限流 / 超时 / 取消等模式。
func classifyError(err error) (code, errType string) {
	if err == nil {
		return ErrCodeUnknown, "NilError"
	}
	errStr := strings.ToLower(err.Error())
	switch {
	case strings.Contains(errStr, "rate limit") || strings.Contains(errStr, "429"):
		return ErrCodeRateLimit, "RateLimitError"
	case strings.Contains(errStr, "context canceled") || strings.Contains(errStr, "context cancelled"):
		return ErrCodeCanceled, "CanceledError"
	case strings.Contains(errStr, "timeout") || strings.Contains(errStr, "deadline exceeded"):
		return ErrCodeTimeout, "TimeoutError"
	case strings.Contains(errStr, "invalid") || strings.Contains(errStr, "bad request") || strings.Contains(errStr, "400"):
		return ErrCodeInvalidParam, "ValidationError"
	default:
		return ErrCodeInternal, "InternalError"
	}
}

// ── 异步写库 ──────────────────────────────────────────────────────────────────
//
// 设计原则：trace 写库不阻塞主请求链路。
//
// 所有 async* 写都提交到 ActiveTrace 的串行尾链，调用方立即返回。
// 每个写 goroutine 包含 recover() 防护，错误由 Attempt barrier 汇总。

func (at *ActiveTrace) submit(write func(context.Context) error) {
	if at == nil || write == nil {
		return
	}
	at.writeWg.Add(1)
	at.submitMu.Lock()
	previous := at.writeTail
	done := make(chan struct{})
	at.writeTail = done
	at.submitMu.Unlock()
	go func() {
		defer at.writeWg.Done()
		defer close(done)
		if previous != nil {
			<-previous
		}
		defer func() {
			if recovered := recover(); recovered != nil {
				at.addWriteError(fmt.Errorf("trace write panic: %v", recovered))
			}
		}()
		ctx, cancel := context.WithTimeout(at.writeCtx, 10*time.Second)
		defer cancel()
		at.addWriteError(write(ctx))
	}()
}

// asyncInsertRun 异步写入 TraceRun 初始记录（status=running）。
// 使用 INSERT IGNORE（OnConflict DoNothing）：trace_id uniqueIndex 冲突时静默跳过，
// 语义比 recover() 吞错更清晰。
func submitInsertRun(at *ActiveTrace, run *dao.TraceRun) {
	at.submit(func(ctx context.Context) error {
		db, err := dao.DB(ctx)
		if err != nil {
			return err
		}
		if result := db.Clauses(clause.OnConflict{DoNothing: true}).Create(run); result.Error != nil {
			g.Log().Warningf(ctx, "[trace] asyncInsertRun failed: %v", result.Error)
			return result.Error
		}
		return nil
	})
}

// asyncInsertNode 异步写入 TraceNode 初始记录（status=running）。
// 使用 INSERT IGNORE（OnConflict DoNothing）：node_id uniqueIndex 冲突时静默跳过。
func asyncInsertNode(at *ActiveTrace, node *dao.TraceNode) {
	at.submit(func(ctx context.Context) error {
		db, err := dao.DB(ctx)
		if err != nil {
			return err
		}
		if result := db.Clauses(clause.OnConflict{DoNothing: true}).Create(node); result.Error != nil {
			g.Log().Warningf(ctx, "[trace] asyncInsertNode failed: %v", result.Error)
			return result.Error
		}
		return nil
	})
}

// NodeUpdate 封装节点结束时需要 UPDATE 的字段，按组件类型选填。
// 设计为值语义：字段零值表示"不更新此字段"，asyncFinishNode 只 UPDATE 非零字段。
//
// 字段分组：
//   - LLM 专属：模型名、Token 数、Prompt / Completion 文本（record_prompt=true 时记录）
//   - RETRIEVER 专属：查询文本、检索结果（top-3 截断序列化）、最终返回文档数、缓存命中标志
//   - 通用：Metadata JSON（TOOL 节点的 tool_name / tool_input / tool_output 等）
type NodeUpdate struct {
	// LLM 专属
	ModelName         string
	InputTokens       int
	CachedInputTokens int
	OutputTokens      int
	ReasoningTokens   int
	CostCNY           float64 // 节点级别估算成本（CNY）
	PromptText        string
	CompletionText    string
	// RETRIEVER 专属
	QueryText      string
	RetrievedDocs  string
	FinalTopK      int
	CacheHit       bool
	DocCount       int     // 召回文档数
	MaxVectorScore float64 // 最高相似度分数
	// 通用
	Metadata string
}

// asyncFinishNode 异步将 TraceNode 由 running 更新为终态（success/error），
// 同时写入耗时、错误信息和节点专属字段（通过 NodeUpdate）。
// 只 UPDATE 非零值字段，避免覆盖 INSERT 时已写入的静态字段（node_type、node_name 等）。
//
// onFinish（通常为 at.UntrackNode）在 UPDATE 执行后调用，而非调用前。
// 这修复了 INSERT/UPDATE 竞态：若 asyncInsertNode goroutine 尚未完成，本次 UPDATE
// 会 0 rows affected，节点最终以 status='running' 入库。推迟 Untrack 保证该节点
// 仍在 pendingNodeIDs 中，AttemptBarrier.Flush 的兜底清理能找到并修正它。
func asyncFinishNode(at *ActiveTrace, nodeID, status, errMsg, errCode, errType string, startTime, endTime time.Time, update *NodeUpdate, onFinish func()) {
	at.submit(func(ctx context.Context) error {
		db, err := dao.DB(ctx)
		if err != nil {
			return err
		}
		durationMs := endTime.Sub(startTime).Milliseconds()
		updates := map[string]any{
			"status":      status,
			"end_time":    endTime,
			"duration_ms": durationMs,
		}
		if errMsg != "" {
			updates["error_message"] = errMsg
		}
		if errCode != "" {
			updates["error_code"] = errCode
		}
		if errType != "" {
			updates["error_type"] = errType
		}
		if update != nil {
			redactor := policy.NewRedactor()
			update.PromptText = redactor.RedactText(update.PromptText)
			update.CompletionText = redactor.RedactText(update.CompletionText)
			update.QueryText = redactor.RedactText(update.QueryText)
			update.RetrievedDocs = redactor.RedactText(update.RetrievedDocs)
			update.Metadata = redactor.RedactText(update.Metadata)
			if update.ModelName != "" {
				updates["model_name"] = update.ModelName
			}
			if update.InputTokens > 0 {
				updates["input_tokens"] = update.InputTokens
			}
			if update.CachedInputTokens > 0 {
				updates["cached_input_tokens"] = update.CachedInputTokens
			}
			if update.OutputTokens > 0 {
				updates["output_tokens"] = update.OutputTokens
			}
			if update.ReasoningTokens > 0 {
				updates["reasoning_tokens"] = update.ReasoningTokens
			}
			// 只要有 token 消耗就写入成本，即使成本很小（如 0.00073 CNY）
			if update.InputTokens > 0 || update.OutputTokens > 0 {
				updates["cost_cny"] = update.CostCNY
			}
			if update.PromptText != "" {
				updates["prompt_text"] = update.PromptText
			}
			if update.CompletionText != "" {
				updates["completion_text"] = update.CompletionText
			}
			if update.QueryText != "" {
				updates["query_text"] = update.QueryText
			}
			if update.RetrievedDocs != "" {
				updates["retrieved_docs"] = update.RetrievedDocs
			}
			if update.FinalTopK >= 0 {
				updates["final_top_k"] = update.FinalTopK
			}
			// CacheHit 只在 update 中明确设置时才更新（避免覆盖为 false）
			if update.CacheHit {
				updates["cache_hit"] = true
			}
			if update.Metadata != "" {
				updates["metadata"] = update.Metadata
			}
		}
		result := db.Model(&dao.TraceNode{}).
			Where("node_id = ? AND status = ?", nodeID, StatusRunning).
			Updates(updates)
		if result.Error != nil {
			g.Log().Warningf(ctx, "[trace] asyncFinishNode update failed node=%s: %v", nodeID, result.Error)
		}
		// UPDATE 后再 Untrack：onFinish（即 at.UntrackNode）在 UPDATE 执行后调用。
		// 目的：修复 INSERT/UPDATE 竞态。若 asyncInsertNode goroutine 尚未执行，
		// UPDATE 会 0 rows affected，节点行以 status='running' 插入。
		// 延迟 Untrack 确保该节点仍在 pendingNodeIDs 中，AttemptBarrier.Flush 的兜底清理
		// 可通过 DrainPendingNodeIDs() 找到并强制更新为终态。
		if onFinish != nil {
			onFinish()
		}
		return result.Error
	})
}

// costConfig 模型定价配置（CNY/1M tokens，直接从配置读取，无需汇率转换）
type costConfig struct {
	Input       float64
	CachedInput float64
	Output      float64
}

var (
	costCfgMu       sync.RWMutex
	costCfgMap      map[string]costConfig // 模型前缀(小写) → 单价
	costCfgLoadedAt time.Time
	costCfgTTL      = 5 * time.Minute
)

// loadCostConfig 从 GoFrame 配置中心加载 model_pricing 配置，带 TTL 缓存（5 分钟）。
// 配置中的价格直接以 CNY/1M tokens 填写，无需汇率转换。
// 允许调价后无需重启服务，最多 5 分钟内生效。
func loadCostConfig(ctx context.Context) map[string]costConfig {
	// 快路径：读锁检查缓存是否有效
	costCfgMu.RLock()
	if costCfgMap != nil && time.Since(costCfgLoadedAt) < costCfgTTL {
		m := costCfgMap
		costCfgMu.RUnlock()
		return m
	}
	costCfgMu.RUnlock()

	// 慢路径：写锁重新加载
	costCfgMu.Lock()
	defer costCfgMu.Unlock()
	// double-check：避免并发时重复加载
	if costCfgMap != nil && time.Since(costCfgLoadedAt) < costCfgTTL {
		return costCfgMap
	}

	m := make(map[string]costConfig)

	cfg, err := appconfig.Current()
	if err != nil {
		costCfgMap = m
		costCfgLoadedAt = time.Now()
		return costCfgMap
	}
	for _, model := range cfg.ModelCatalog {
		m[strings.ToLower(model.ModelID)] = costConfig{
			Input:       model.Pricing.Input,
			CachedInput: model.Pricing.CachedInput,
			Output:      model.Pricing.Output,
		}
	}
	costCfgMap = m
	costCfgLoadedAt = time.Now()
	return costCfgMap
}

func estimateCostWithBreakdown(ctx context.Context, modelName string, inputTokens, cachedInputTokens, outputTokens, reasoningTokens int64) float64 {
	if inputTokens == 0 && outputTokens == 0 {
		return 0
	}

	pricing := loadCostConfig(ctx)
	key := strings.ToLower(modelName)

	var cost costConfig
	var matched bool
	if c, ok := pricing[key]; ok {
		cost = c
		matched = true
	} else {
		// 前缀匹配：vendor-model-latest 匹配 vendor-model。
		for prefix, c := range pricing {
			if strings.HasPrefix(key, prefix) {
				cost = c
				matched = true
				break
			}
		}
	}

	// 未命中配置时记录警告日志，便于排查定价配置问题
	if !matched && (inputTokens > 0 || outputTokens > 0) {
		g.Log().Warningf(ctx, "[trace] 模型 %s 未配置定价，成本计为 0 | inputTokens=%d outputTokens=%d",
			modelName, inputTokens, outputTokens)
	}

	return estimateTokenCost(cost, inputTokens, cachedInputTokens, outputTokens, reasoningTokens)
}

func estimateTokenCost(cost costConfig, inputTokens, cachedInputTokens, outputTokens, reasoningTokens int64) float64 {
	if inputTokens < 0 {
		inputTokens = 0
	}
	if outputTokens < 0 {
		outputTokens = 0
	}
	if cachedInputTokens < 0 {
		cachedInputTokens = 0
	}
	if reasoningTokens < 0 {
		reasoningTokens = 0
	}
	if cachedInputTokens > inputTokens {
		cachedInputTokens = inputTokens
	}
	// reasoningTokens 已包含在 outputTokens 中，禁止再次累加。
	_ = reasoningTokens
	uncachedInput := inputTokens - cachedInputTokens
	return (float64(uncachedInput)*cost.Input +
		float64(cachedInputTokens)*cost.CachedInput +
		float64(outputTokens)*cost.Output) / 1_000_000.0
}

// AttemptBarrier 等待同一 ActiveTrace 的流式回调和全部异步 MySQL 写。
type AttemptBarrier struct {
	active       *ActiveTrace
	skipFinalize bool
	langfuse     *LangfuseRuntime
	langfuseCtx  context.Context
	langfuseEnd  sync.Once
}

// Finish 记录 Attempt 业务结果；真正的 TraceRun 终态写由 Flush 在节点落盘后执行。
func (b *AttemptBarrier) Finish(err error) {
	if b == nil || b.active == nil {
		return
	}
	status, errMsg, errCode := StatusSuccess, "", ""
	if err != nil {
		status = StatusError
		errMsg = policy.NewRedactor().RedactText(stringutil.TruncateError(err, GetConfig().MaxErrorLength))
		errCode, _ = classifyError(err)
	}
	b.active.mu.Lock()
	b.active.finish = traceFinish{set: true, status: status, errMsg: errMsg, errCode: errCode, endTime: time.Now()}
	b.active.mu.Unlock()
	if b.langfuse != nil {
		output := "success"
		if err != nil {
			output = policy.NewRedactor().RedactText(stringutil.TruncateError(err, GetConfig().MaxErrorLength))
		}
		b.langfuseEnd.Do(func() { b.langfuse.EndAttempt(b.langfuseCtx, output) })
	}
}

// Flush 返回可直接传给唯一 Run 完成 primitive 的 trace_quality。
func (b *AttemptBarrier) Flush(ctx context.Context) string {
	if b == nil || b.active == nil {
		return TraceQualityIncomplete
	}
	at := b.active
	if ctx == nil {
		ctx = at.writeCtx
	}
	if ctx == nil {
		return TraceQualityIncomplete
	}
	quality := TraceQualityComplete
	streamsComplete := waitTraceGroup(ctx, &at.StreamWg)
	if !streamsComplete {
		quality = TraceQualityIncomplete
	}
	writesComplete := waitTraceGroup(ctx, &at.writeWg)
	if !writesComplete {
		quality = TraceQualityIncomplete
	}
	if at.writeError() != nil {
		quality = TraceQualityIncomplete
	}
	if b.skipFinalize {
		return quality
	}
	finalizeCtx := ctx
	if !streamsComplete || !writesComplete {
		var cancel context.CancelFunc
		finalizeCtx, cancel = context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer cancel()
	}
	if streamsComplete && writesComplete {
		if err := cleanupPendingNodes(finalizeCtx, at); err != nil {
			quality = TraceQualityIncomplete
		}
	}
	if err := finalizeTraceRun(finalizeCtx, at, quality); err != nil {
		return TraceQualityIncomplete
	}
	if b.langfuse != nil {
		_ = b.langfuse.Flush(ctx)
	}
	return quality
}

func waitTraceGroup(ctx context.Context, group *sync.WaitGroup) bool {
	done := make(chan struct{})
	go func() {
		group.Wait()
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-ctx.Done():
		return false
	}
}

func cleanupPendingNodes(ctx context.Context, at *ActiveTrace) error {
	pendingIDs := at.DrainPendingNodeIDs()
	if len(pendingIDs) == 0 {
		return nil
	}
	db, err := dao.DB(ctx)
	if err != nil {
		return err
	}
	at.mu.Lock()
	finish := at.finish
	at.mu.Unlock()
	result := db.Model(&dao.TraceNode{}).
		Where("node_id IN ? AND status = ?", pendingIDs, StatusRunning).
		Updates(map[string]any{"status": finish.status, "end_time": finish.endTime})
	return result.Error
}

func finalizeTraceRun(ctx context.Context, at *ActiveTrace, quality string) error {
	at.mu.Lock()
	finish := at.finish
	totalIn, totalCachedIn := at.TotalInputTokens, at.TotalCachedInputTokens
	totalOut, totalReasoning := at.TotalOutputTokens, at.TotalReasoningTokens
	costCNY := at.TotalCostCNY
	at.mu.Unlock()
	if !finish.set {
		finish = traceFinish{set: true, status: StatusSuccess, endTime: time.Now()}
	}
	tags := authoritativeTraceTags(ctx, map[string]any{
		"run_id": at.Attempt.RunID, "attempt": at.Attempt.Attempt,
		"lease_generation": at.Attempt.LeaseGeneration, "runtime_version": at.Attempt.RuntimeVersion,
		"trace_quality": quality,
	})
	redactedTags, err := redactTraceValue(tags)
	if err != nil {
		return err
	}
	tagsJSON, err := json.Marshal(redactedTags)
	if err != nil {
		return err
	}
	db, err := dao.DB(ctx)
	if err != nil {
		return err
	}
	updates := map[string]any{
		"status": finish.status, "end_time": finish.endTime,
		"duration_ms":        max(finish.endTime.Sub(at.StartTime).Milliseconds(), 0),
		"total_input_tokens": totalIn, "cached_input_tokens": totalCachedIn,
		"total_output_tokens": totalOut, "reasoning_tokens": totalReasoning,
		"estimated_cost_cny": costCNY, "tags": string(tagsJSON),
	}
	if finish.errMsg != "" {
		updates["error_message"] = finish.errMsg
	}
	if finish.errCode != "" {
		updates["error_code"] = finish.errCode
	}
	result := db.Model(&dao.TraceRun{}).Where("trace_id = ?", at.TraceID).Updates(updates)
	return result.Error
}

func newTestBarrier(write func(context.Context) error) *AttemptBarrier {
	at := &ActiveTrace{TraceID: "test-trace", StartTime: time.Now(), Stack: &SpanStack{}, writeCtx: context.TODO()}
	at.submit(write)
	return &AttemptBarrier{active: at, skipFinalize: true}
}
