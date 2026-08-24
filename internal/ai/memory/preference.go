// Package memory 用户偏好记忆管理。
//
// # 设计背景
//
// 不同用户对 AI 回答有截然不同的期望：安全研究员希望深度溯源分析，
// 管理层只需快速结论，运维人员关注可操作的处置步骤。
// 如果所有用户都使用相同的 Prompt，AI 无法感知这些差异，回答质量参差不齐。
//
// # 解决方案：隐式偏好推断
//
// 系统通过两类信号自动感知用户偏好，无需用户手动配置：
//   - 对话行为：消息积累到一定数量时，从近期对话中提取偏好特征
//   - 点踩反馈：用户对回答不满意时选择原因标签，明确标签直接写字段，
//     无标签时走 LLM 推断
//
// # 数据流
//
//	触发信号（对话行为 / 点踩标签）
//	    ↓
//	TriggerInferPreference（防抖过滤）
//	    ↓
//	inferNoteFromMessages（取最近 6 条消息 → LLM 提取摘要）
//	    ↓ 合并更新（不直接覆盖，防止偶发对话污染长期偏好）
//	updateInferredNote（MySQL upsert → 失效进程内缓存）
//	    ↓
//	GetPreference（进程内缓存 5min → MySQL）← executor.go 每次 SubAgent 执行前读取
//	    ↓
//	FormatPromptHint → 前置注入 task.Query
//
// # 存储层次
//
//   - 进程内 map：5min TTL，上限 1000 条，读多写少用 RWMutex
//   - MySQL user_preferences：跨会话、跨重启持久化
//
// # 防抖设计
//
// 偏好是缓慢变化的特征，高频推断既浪费 LLM 调用成本，结果也不稳定。
// canInfer 保证同一用户在 inferDebounceHours 配置的间隔内只触发一次 LLM 推断。
// 明确标签（too_verbose 等）绕过此防抖，由调用方（rageval service）直接写字段。
package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"SentinelOps/internal/ai/models"
	aiprompt "SentinelOps/internal/ai/prompt/memory"
	"SentinelOps/internal/dao/mysql"

	"github.com/cloudwego/eino/schema"
	"github.com/gogf/gf/v2/frame/g"
)

// UserIdCtxKey 是在 context 中传递 userId 的键类型。
// 定义在 memory 包，供 intent、plan_pipeline、controller 等多处共享，避免循环依赖。
type UserIdCtxKey struct{}

// Preference 用户偏好运行时快照。
// 字段来源全为隐式推断：OutputStyle/AnalysisDepth/FocusAreas 由点踩标签直接写入，
// InferredNote 由 LLM 从对话中提取。
type Preference struct {
	OutputStyle   string   // 输出风格：detailed / concise
	AnalysisDepth string   // 分析深度：quick / standard / deep
	FocusAreas    []string // 关注领域，如 ["web","supply_chain"]
	InferredNote  string   // LLM 从对话中提取的用户背景摘要（≤50字）
}

// FormatPromptHint 将偏好快照格式化为可前置注入 task.Query 的提示片段。
// 无有效偏好时返回空字符串，调用方无需特殊处理。
// 输出示例：
//
//	【用户偏好】
//	回答要简洁，聚焦关键结论；用户背景：安全研究员，关注 APT 攻击链
func (p *Preference) FormatPromptHint() string {
	if p == nil {
		return ""
	}
	var parts []string
	switch p.OutputStyle {
	case "concise":
		parts = append(parts, "回答要简洁，聚焦关键结论")
	case "detailed":
		parts = append(parts, "回答要详尽，包含完整分析过程")
	}
	switch p.AnalysisDepth {
	case "deep":
		parts = append(parts, "进行深度溯源分析，不省略技术细节")
	case "quick":
		parts = append(parts, "给出快速摘要，不展开细节")
	}
	if len(p.FocusAreas) > 0 {
		parts = append(parts, "重点关注领域："+strings.Join(p.FocusAreas, "、"))
	}
	if p.InferredNote != "" {
		parts = append(parts, "用户背景："+p.InferredNote)
	}
	if len(parts) == 0 {
		return ""
	}
	return "【用户偏好】\n" + strings.Join(parts, "；") + "\n"
}

// ── 进程内缓存 ────────────────────────────────────────────────────────────────
// 读多写少：每次 SubAgent 执行前都会读一次偏好，写入只在推断完成后发生。
// 因此使用 RWMutex，允许并发读，写时独占。

var (
	prefCache    = make(map[string]*Preference)
	prefCacheTTL = make(map[string]time.Time)
	prefMu       sync.RWMutex
)

const prefTTL = 5 * time.Minute
const prefCacheMaxSize = 1000

// GetPreference 获取用户偏好，优先从进程内缓存读取，缓存未命中或过期时查 MySQL。
// userID 为空或数据库中无记录时返回 nil，调用方需做 nil 判断。
//
// 缓存策略：TTL 5min，上限 1000 条。超上限时跳过写缓存而非淘汰旧条目，
// 避免在高并发场景下因淘汰逻辑引入额外锁竞争。
func GetPreference(ctx context.Context, userID string) *Preference {
	if userID == "" {
		return nil
	}
	prefMu.RLock()
	if p, ok := prefCache[userID]; ok && time.Now().Before(prefCacheTTL[userID]) {
		prefMu.RUnlock()
		return p
	}
	prefMu.RUnlock()

	db, err := mysql.DB(ctx)
	if err != nil {
		return nil
	}
	var row mysql.UserPreference
	if err := db.WithContext(ctx).Where("user_id = ?", userID).First(&row).Error; err != nil {
		return nil
	}
	p := &Preference{
		OutputStyle:   row.OutputStyle,
		AnalysisDepth: row.AnalysisDepth,
		InferredNote:  row.InferredNote,
	}
	if row.FocusAreas != "" {
		_ = json.Unmarshal([]byte(row.FocusAreas), &p.FocusAreas)
	}

	prefMu.Lock()
	// 缓存上限：超过 1000 条时跳过写缓存，防止无界增长
	if len(prefCache) < prefCacheMaxSize {
		prefCache[userID] = p
		prefCacheTTL[userID] = time.Now().Add(prefTTL)
	}
	prefMu.Unlock()
	return p
}

// InvalidateCache 使指定用户的偏好缓存失效。
// 偏好写入 MySQL 后必须调用此函数，否则进程内缓存会在 TTL 到期前继续返回旧值。
func InvalidateCache(userID string) {
	prefMu.Lock()
	delete(prefCache, userID)
	prefMu.Unlock()
}

// ── 隐式推断 ──────────────────────────────────────────────────────────────────

// inferLocks 进程内推断防抖锁：同一用户在防抖间隔内只触发一次 LLM 推断
var (
	inferLocks      = make(map[string]time.Time)
	inferLocksMu    sync.Mutex
	inferDebounce   time.Duration
	inferConfigOnce sync.Once
)

// loadInferConfig 从配置文件懒加载推断防抖间隔，通过 sync.Once 保证只加载一次。
// 配置项：memory.inferDebounceHours（默认 1 小时）。
func loadInferConfig() {
	inferConfigOnce.Do(func() {
		ctx := context.Background()
		minutes := g.Cfg().MustGet(ctx, "memory.inferDebounceMinutes").Int()
		if minutes <= 0 {
			minutes = 5
		}
		inferDebounce = time.Duration(minutes) * time.Minute
	})
}

// canInfer 检查是否允许对该用户触发推断（防抖），同时清理过期条目防止 map 无界增长
func canInfer(userID string) bool {
	loadInferConfig()
	inferLocksMu.Lock()
	defer inferLocksMu.Unlock()
	if last, ok := inferLocks[userID]; ok && time.Since(last) < inferDebounce {
		return false
	}
	// 清理所有已过期条目（每次写入时顺带 GC，均摊 O(n)，避免 map 无界增长）
	for uid, t := range inferLocks {
		if time.Since(t) >= inferDebounce {
			delete(inferLocks, uid)
		}
	}
	inferLocks[userID] = time.Now()
	return true
}

// TriggerInferPreference 异步从最近消息中提取偏好摘要并更新，不阻塞调用方。
// 防抖间隔由 memory.inferDebounceHours 配置（默认 1 小时），同一用户短时间内多次触发只执行一次。
func TriggerInferPreference(ctx context.Context, userID string, msgs []*schema.Message) {
	if userID == "" || len(msgs) == 0 {
		return
	}
	if !canInfer(userID) {
		g.Log().Debugf(ctx, "[Preference] 偏好推断被防抖跳过 | user=%s", userID)
		return
	}
	g.Log().Infof(ctx, "[Preference] 触发偏好推断 | user=%s | msgs=%d", userID, len(msgs))
	// 快照消息，避免 goroutine 中读到被修改的切片
	snapshot := make([]*schema.Message, len(msgs))
	copy(snapshot, msgs)
	go func() {
		bgCtx := context.Background()
		note := inferNoteFromMessages(bgCtx, userID, snapshot)
		if note != "" {
			updateInferredNote(bgCtx, userID, note)
		}
	}()
}

// inferNoteFromMessages 从最近 6 条消息中调用 LLM 提取用户偏好摘要。
//
// 取最近 6 条而非全量消息的原因：偏好特征在近期对话中体现最明显，
// 全量消息会引入噪声且增加 token 成本。每条消息截断至 150 字，避免单条长消息主导结果。
//
// 合并更新策略：先查 MySQL 中已有的 InferredNote，有旧值时用 InferPreferenceMerge
// 提示词合并（保留稳定特征，修正变化部分），无旧值时用 InferPreferenceNew 首次提取。
// 这样即使某次对话内容偏离用户平时风格，也不会直接覆盖长期积累的偏好画像。
func inferNoteFromMessages(ctx context.Context, userID string, msgs []*schema.Message) string {
	start := len(msgs) - 6
	if start < 0 {
		start = 0
	}
	var sb strings.Builder
	for _, m := range msgs[start:] {
		role := "用户"
		if m.Role == schema.Assistant {
			role = "助手"
		}
		content := m.Content
		if runes := []rune(content); len(runes) > 150 {
			content = string(runes[:150]) + "..."
		}
		if content != "" {
			sb.WriteString(role + ": " + content + "\n")
		}
	}
	if sb.Len() == 0 {
		return ""
	}

	// 查旧偏好，合并更新
	existing := ""
	if db, err := mysql.DB(ctx); err == nil {
		var row mysql.UserPreference
		if err := db.WithContext(ctx).Where("user_id = ?", userID).First(&row).Error; err == nil {
			existing = row.InferredNote
		}
	}

	model, err := models.ChatDefault(ctx)
	if err != nil {
		return ""
	}

	var prompt string
	if existing != "" {
		prompt = fmt.Sprintf(aiprompt.InferPreferenceMerge, existing, sb.String())
	} else {
		prompt = aiprompt.InferPreferenceNew + sb.String()
	}

	out, err := model.Generate(ctx, []*schema.Message{schema.UserMessage(prompt)})
	if err != nil || out == nil {
		return ""
	}
	note := strings.TrimSpace(out.Content)
	if len([]rune(note)) > 50 {
		note = string([]rune(note)[:50])
	}
	g.Log().Infof(ctx, "[Preference] LLM推断用户偏好 | user=%s | note=%q", userID, note)
	return note
}

// updateInferredNote 将 LLM 推断出的摘要写入 MySQL（upsert 语义）并失效进程内缓存。
// 先尝试 UPDATE，影响行数为 0 时说明该用户尚无偏好记录，执行 INSERT。
func updateInferredNote(ctx context.Context, userID, note string) {
	db, err := mysql.DB(ctx)
	if err != nil {
		return
	}
	res := db.Model(&mysql.UserPreference{}).Where("user_id = ?", userID).Update("inferred_note", note)
	if res.Error != nil {
		g.Log().Warningf(ctx, "[Preference] 更新隐式推断失败 | user=%s | err=%v", userID, res.Error)
		return
	}
	if res.RowsAffected == 0 {
		db.Create(&mysql.UserPreference{UserID: userID, InferredNote: note, FocusAreas: "[]"})
	}
	InvalidateCache(userID)
}
