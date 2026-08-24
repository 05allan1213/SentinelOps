// Package pipeline 安全事件处理流水线：抓取 → 提取 → 去重入库 → 向量索引
package pipeline

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"SentinelOps/internal/ai/indexer"
	dao "SentinelOps/internal/dao/mysql"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/google/uuid"
	"github.com/mmcdole/gofeed"
)

// ==================== 抓取层（Fetcher） ====================

// RawItem 抓取后的原始条目，统一表示从不同数据源获取的安全事件
type RawItem struct {
	Title     string     // 事件标题
	Link      string     // 事件链接（原始来源 URL）
	Content   string     // 事件详细内容（完整内容，不截断）
	Source    string     // 数据源名称（如 "NVD RSS"、"github:owner/repo"）
	EventType string     // 事件来源大类：github、rss
	PubDate   *time.Time // 发布时间（可能为空）
	Severity  string     // 明确的严重程度（可选，Advisory 等可显式传入；空时由 Extract 阶段关键词推断）
	CVEID     string     // CVE 编号（可选，Advisory 等可显式传入；空时由 Extract 阶段正则提取）
}

// Fetch 从订阅源抓取并解析为 RawItem，支持 rss、github 类型。
// 根据 sub.Type 分发到对应的抓取实现，类型不区分大小写。
func Fetch(ctx context.Context, sub *dao.Subscription) ([]RawItem, error) {
	subType := strings.ToLower(strings.TrimSpace(sub.Type))
	switch subType {
	case "github":
		return fetchGitHub(ctx, sub)
	default:
		return fetchRSS(ctx, sub)
	}
}

// fetchRSS 从 RSS/Atom feed 抓取安全事件。
// 使用 gofeed 统一解析 RSS 2.0 和 Atom 1.0 格式。
func fetchRSS(ctx context.Context, sub *dao.Subscription) ([]RawItem, error) {
	fp := gofeed.NewParser()
	// 带 context 的 HTTP 请求，支持超时取消
	feed, err := fp.ParseURLWithContext(sub.URL, ctx)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", sub.URL, err)
	}
	// 来源标识优先用订阅名，兜底用 URL（保证 source 字段非空）
	source := sub.Name
	if source == "" {
		source = sub.URL
	}
	var items []RawItem
	for _, item := range feed.Items {
		content := item.Content
		if content == "" {
			content = item.Description
		}
		content = strings.TrimSpace(content)
		ri := RawItem{
			Title:     strings.TrimSpace(item.Title),
			Link:      item.Link,
			Content:   content,
			Source:    source,
			EventType: "rss",
			PubDate:   item.PublishedParsed, // gofeed 已自动解析多种时间格式
		}
		// 标题为空时用链接代替，确保后续 Extract 过滤逻辑不会误丢
		if ri.Title == "" {
			ri.Title = item.Link
		}
		items = append(items, ri)
	}
	return items, nil
}

// githubRepoRe 从 GitHub URL 提取 owner/repo，兼容带路径和 .git 后缀的形式。
// 支持以下格式：
//   - https://github.com/owner/repo
//   - https://github.com/owner/repo.git
//   - https://github.com/owner/repo/security/advisories（仅订阅安全公告时）
var githubRepoRe = regexp.MustCompile(`github\.com/([^/]+)/([^/]+?)(?:/.*)?$`)

// isAdvisoryURL 判断 URL 是否为安全公告页面（以 /security/advisories 结尾）。
// 此类订阅只需调用 Security Advisories API，跳过 Releases 节约限速配额。
func isAdvisoryURL(url string) bool {
	return strings.Contains(url, "/security/advisories")
}

// githubSource 构建统一的来源标识：优先使用订阅名，兜底用 "github:owner/repo" 格式
func githubSource(sub *dao.Subscription, owner, repo string) string {
	if sub.Name != "" {
		return sub.Name
	}
	return fmt.Sprintf("github:%s/%s", owner, repo)
}

// parseGitHubTime 解析 GitHub API 返回的 RFC3339 格式时间，解析失败返回 nil
func parseGitHubTime(s string) *time.Time {
	if s == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return nil
	}
	return &t
}

// githubGet 向 GitHub REST API v3 发起 GET 请求并将响应体 JSON 解码到 dst。
// 调用方负责传入带 context 的 deadline，确保不会无限阻塞。
func githubGet(ctx context.Context, apiURL string, dst any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return err
	}
	// Accept header 指定 GitHub API v3 响应格式
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	if token := g.Cfg().MustGet(ctx, "tools.github.token", "").String(); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("github api: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("github api status %d for %s", resp.StatusCode, apiURL)
	}
	return json.NewDecoder(resp.Body).Decode(dst)
}

// fetchGitHub 从 GitHub 仓库聚合抓取安全相关事件。
//
// 根据订阅 URL 路径自动识别抓取意图：
//   - URL 包含 /security/advisories（如 https://github.com/moby/moby/security/advisories）
//     → 仅调用 Security Advisories API，跳过 Releases，节约未认证限速配额（60次/小时）
//   - 普通仓库 URL（如 https://github.com/owner/repo）
//     → 同时调用 Releases + Security Advisories，覆盖更完整的安全情报面
//
// 两类数据源独立抓取，单类失败仅记录警告；全部失败才向上返回错误。
func fetchGitHub(ctx context.Context, sub *dao.Subscription) ([]RawItem, error) {
	// 从 URL 提取 owner/repo（支持 .git 后缀和子路径）
	matches := githubRepoRe.FindStringSubmatch(sub.URL)
	if len(matches) < 3 {
		return nil, fmt.Errorf("invalid github url: %s", sub.URL)
	}
	owner, repo := matches[1], strings.TrimSuffix(matches[2], ".git")
	source := githubSource(sub, owner, repo)

	// 仅订阅安全公告页面时，跳过 Releases 直接返回 Advisories
	if isAdvisoryURL(sub.URL) {
		return fetchGitHubAdvisories(ctx, owner, repo, source)
	}

	// 普通仓库 URL：同时拉取 Releases 和 Advisories
	releases, releaseErr := fetchGitHubReleases(ctx, owner, repo, source)
	if releaseErr != nil {
		g.Log().Warningf(ctx, "[pipeline] 抓取 GitHub Releases 失败（%s/%s）: %v", owner, repo, releaseErr)
	}

	advisories, advisoryErr := fetchGitHubAdvisories(ctx, owner, repo, source)
	if advisoryErr != nil {
		g.Log().Warningf(ctx, "[pipeline] 抓取 GitHub Security Advisories 失败（%s/%s）: %v", owner, repo, advisoryErr)
	}

	// 两类均失败才返回错误，避免单类失败导致整个订阅中断
	if releaseErr != nil && advisoryErr != nil {
		return nil, fmt.Errorf("所有 GitHub 数据源抓取失败（%s/%s）", owner, repo)
	}
	return append(releases, advisories...), nil
}

// fetchGitHubReleases 抓取仓库最近 10 个 Release，映射为 RawItem。
// Release.Name 为空时降级使用 TagName，确保 Title 非空（防止 Extract 阶段过滤掉有效事件）。
func fetchGitHubReleases(ctx context.Context, owner, repo, source string) ([]RawItem, error) {
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases?per_page=10", owner, repo)

	// 只解析需要的字段，减少内存占用
	var releases []struct {
		Name        string `json:"name"`
		TagName     string `json:"tag_name"`
		Body        string `json:"body"`
		HTMLURL     string `json:"html_url"`
		PublishedAt string `json:"published_at"`
	}
	if err := githubGet(ctx, apiURL, &releases); err != nil {
		return nil, err
	}

	items := make([]RawItem, 0, len(releases))
	for _, r := range releases {
		// Name 为空时降级使用 TagName，避免 Title="" 导致 Extract 阶段丢弃事件
		title := strings.TrimSpace(r.Name)
		if title == "" {
			title = strings.TrimSpace(r.TagName)
		}
		items = append(items, RawItem{
			Title:     title,
			Link:      r.HTMLURL,
			Content:   strings.TrimSpace(r.Body),
			Source:    source,
			EventType: "github",
			PubDate:   parseGitHubTime(r.PublishedAt),
		})
	}
	return items, nil
}

// fetchGitHubAdvisories 抓取仓库最近 10 条 Security Advisory（GHSA），映射为 RawItem。
// Advisory 的 severity 字段直接传入 RawItem.Severity，在 Extract 阶段优先于关键词推断使用。
// Advisory 可携带多个 CVE ID，取第一个写入 RawItem.CVEID（与单条事件一一对应）。
func fetchGitHubAdvisories(ctx context.Context, owner, repo, source string) ([]RawItem, error) {
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/security-advisories?per_page=10", owner, repo)

	// 只解析需要的字段，减少内存占用
	var advisories []struct {
		GHSAID      string   `json:"ghsa_id"`
		Summary     string   `json:"summary"`
		Description string   `json:"description"`
		Severity    string   `json:"severity"`
		HTMLURL     string   `json:"html_url"`
		PublishedAt string   `json:"published_at"`
		CVEIDs      []string `json:"cve_ids"`
	}
	if err := githubGet(ctx, apiURL, &advisories); err != nil {
		return nil, err
	}

	items := make([]RawItem, 0, len(advisories))
	for _, a := range advisories {
		// Summary 为空时降级使用 GHSA ID，确保 Title 始终非空
		title := strings.TrimSpace(a.Summary)
		if title == "" {
			title = a.GHSAID
		}
		// 多个 CVE ID 时取首个（一条 Advisory 通常仅关联一个主 CVE）
		var cveID string
		if len(a.CVEIDs) > 0 {
			cveID = a.CVEIDs[0]
		}
		items = append(items, RawItem{
			Title:     title,
			Link:      a.HTMLURL,
			Content:   strings.TrimSpace(a.Description),
			Source:    source,
			EventType: "github",
			PubDate:   parseGitHubTime(a.PublishedAt),
			Severity:  a.Severity, // 官方明确的严重程度，Extract 阶段直接使用，无需关键词推断
			CVEID:     cveID,
		})
	}
	return items, nil
}

// ==================== 提取层（Extractor） ====================

// cveRe 匹配 CVE 编号，格式：CVE-年份-序号（序号至少 4 位）
var cveRe = regexp.MustCompile(`CVE-\d{4}-\d{4,}`)

// severity 推断正则：使用词边界（\b）避免子串误匹配（如 high→highlight、low→below）。
// 中文关键词不需要 \b（CJK 字符在 Go RE2 中天然不属于 \w）。
var (
	reCritical = regexp.MustCompile(`\bcritical\b|严重`)
	reHigh     = regexp.MustCompile(`\bhigh\b|高危`)
	reLow      = regexp.MustCompile(`\blow\b|低危`)
)

// Extract 将 RawItem 批量转换为 Event，执行严重程度推断、风险评分映射、CVE 提取。
// 过滤掉 Title 为空的无效条目（无法作为有效事件入库）。
// severity 优先级：RawItem.Severity（数据源明确指定，如 Advisory）> 关键词推断（RSS、Release 等无结构化严重度的来源）。
// risk_score 始终由最终确定的 severity 派生（SeverityToRiskScore），保证两者严格一致。
func Extract(items []RawItem) []dao.Event {
	events := make([]dao.Event, 0, len(items))
	for _, item := range items {
		// 标题为空视为无效条目，直接跳过
		if item.Title == "" {
			continue
		}
		// severity 优先使用数据源明确传入的值（如 Advisory），否则按关键词推断
		severity := item.Severity
		if severity == "" {
			severity = inferSeverity(item.Title, item.Content)
		}
		e := dao.Event{
			ID:        uuid.New().String(), // 每条事件生成唯一 UUID
			Title:     item.Title,
			Content:   item.Content,
			EventType: item.EventType,
			Severity:  severity,
			Source:    item.Source,
			Status:    "new", // 新事件默认状态为待处理
			CVEID:     item.CVEID,
			RiskScore: SeverityToRiskScore(severity), // 由 severity 统一派生，保证与 severity 始终一致
		}
		// RawItem 未携带 CVE ID 时，尝试从标题/内容中正则提取
		if e.CVEID == "" {
			e.CVEID = extractCVEID(item.Title, item.Content)
		}
		// 原始链接和发布时间写入 metadata，便于前端溯源
		if item.Link != "" || item.PubDate != nil {
			meta := map[string]string{}
			if item.Link != "" {
				meta["link"] = item.Link
			}
			if item.PubDate != nil {
				meta["pub_date"] = item.PubDate.Format(time.RFC3339)
			}
			if b, merr := json.Marshal(meta); merr == nil {
				e.Metadata = string(b)
			}
		}
		// 集中计算去重键（在 Extract 阶段统一生成，避免分散计算）
		e.DedupKey = dedupKey(e)
		events = append(events, e)
	}
	return events
}

// SeverityToRiskScore 将严重程度字符串映射为 0-10 风险评分（参考 CVSS 标准）。
// critical=9.0 / high=7.0 / medium=5.0 / low=3.0 / 未知=5.0（默认中等）
func SeverityToRiskScore(severity string) float64 {
	switch strings.ToLower(severity) {
	case "critical":
		return 9.0
	case "high":
		return 7.0
	case "medium":
		return 5.0
	case "low":
		return 3.0
	default:
		return 5.0 // 未知等级按中等处理
	}
}

// inferSeverity 基于关键词推断严重程度，按优先级从高到低匹配，支持中英文。
// 匹配范围：标题 + 内容拼接后统一转小写。
// 使用正则词边界（\b）防止子串误匹配（如 high→highlight、low→below/allow）。
func inferSeverity(title, content string) string {
	s := strings.ToLower(title + " " + content)
	switch {
	case reCritical.MatchString(s):
		return "critical"
	case reHigh.MatchString(s):
		return "high"
	case reLow.MatchString(s):
		return "low"
	default:
		return "medium"
	}
}

// extractCVEID 从文本中提取首个 CVE 编号。
// 优先从标题提取（标题信息密度高），未找到再从正文提取。
func extractCVEID(title, content string) string {
	// 优先从标题中匹配，通常关键漏洞编号会出现在标题
	if m := cveRe.FindString(title); m != "" {
		return m
	}
	// 标题未命中则从正文中查找首个 CVE 编号
	if m := cveRe.FindString(content); m != "" {
		return m
	}
	return ""
}

// ==================== 去重入库层（Dedup） ====================

// DedupAndInsert 对事件列表去重后批量插入 MySQL，返回实际插入的事件列表（含完整内存数据）。
// 委托 dao 层执行，保持 pipeline 包的公共接口稳定。
func DedupAndInsert(ctx context.Context, events []dao.Event) ([]dao.Event, error) {
	return dao.DedupAndInsertEvents(ctx, events)
}

// dedupKey 生成事件去重键：对 title|source|content(前500字符) 取 SHA256，返回十六进制前 32 位。
// content 按 rune 边界截取前 500 字符，避免在 UTF-8 多字节字符中间截断产生无效编码。
// 取前 32 位（128 bit）：存储友好，碰撞概率极低（2^-128）。
func dedupKey(e dao.Event) string {
	h := sha256.New()
	content := e.Content
	if runes := []rune(content); len(runes) > 500 {
		content = string(runes[:500])
	}
	// 用 | 分隔字段，防止 "ab"+"c" 与 "a"+"bc" 产生相同哈希
	h.Write([]byte(e.Title + "|" + e.Source + "|" + strings.TrimSpace(content)))
	return hex.EncodeToString(h.Sum(nil))[:32]
}

// ==================== 向量索引层（Indexer） ====================

// IndexDocuments 将内存中的事件列表向量化并写入 Milvus，按配置的批次大小分批执行。
// 接受 DedupAndInsert 返回的已插入事件（含完整 Content），无需再查询 MySQL。
// 单批失败不中断其余批次；仅对成功写入 Milvus 的记录更新 indexed_at，保证标记准确。
func IndexDocuments(ctx context.Context, events []dao.Event) error {
	if len(events) == 0 {
		return nil
	}
	// 从配置读取批次大小，兜底为当前 Embedding 模型兼容的 10 条。
	batchSize := 10
	if v, e := g.Cfg().Get(ctx, "scheduler.index_batch_size"); e == nil && v.Int() > 0 {
		batchSize = v.Int()
	}

	// 委托 indexer 层执行文档构建 + 分批写入 Milvus
	ids, err := indexer.StoreEvents(ctx, events, batchSize)
	if err != nil {
		// GetIndexer 初始化失败，无法继续
		return err
	}

	if len(ids) > 0 {
		// 仅标记实际写入成功的记录，截断到秒与 DATETIME 列精度一致
		now := time.Now().Truncate(time.Second)
		if markErr := dao.MarkEventsIndexed(ctx, ids, now); markErr != nil {
			g.Log().Warningf(ctx, "[pipeline] 更新 indexed_at 失败: %v", markErr)
		}
		g.Log().Infof(ctx, "[pipeline] 向量索引完成，成功 %d/%d 条（分 %d 批）",
			len(ids), len(events), (len(events)+batchSize-1)/batchSize)
	}
	return nil
}

// IndexDocumentsAsync 异步执行向量索引，立即返回不阻塞调用方。
// 适用于 HTTP 请求路径（FetchNow/Receive/Create），避免 Embedding API 延迟影响响应时间。
// 使用独立 context 确保 HTTP 请求结束后索引任务仍能正常完成。
func IndexDocumentsAsync(ctx context.Context, events []dao.Event) {
	if len(events) == 0 {
		return
	}
	go func() {
		// 使用独立 context，避免父 ctx（HTTP 请求）取消后任务中断或日志丢失追踪信息
		bgCtx := context.Background()
		if err := IndexDocuments(bgCtx, events); err != nil {
			g.Log().Warningf(bgCtx, "[pipeline] 异步向量索引失败: %v", err)
		}
	}()
}
