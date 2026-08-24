package event

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	milvus "SentinelOps/internal/dao/milvus"
	dao "SentinelOps/internal/dao/mysql"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/milvus-io/milvus-sdk-go/v2/entity"
)

// QueryEventsInput 查询事件参数
type QueryEventsInput struct {
	EventID  string `json:"event_id,omitempty"  jsonschema:"description=按事件 ID 精确查询单条事件详情，填写后其他过滤条件忽略"`
	Severity string `json:"severity,omitempty" jsonschema:"description=按严重程度过滤: critical, high, medium, low，空则不过滤"`
	Status   string `json:"status,omitempty"   jsonschema:"description=按处理状态过滤: new, processing, resolved, ignored，空则不过滤"`
	CveID    string `json:"cve_id,omitempty"   jsonschema:"description=按 CVE 编号精确过滤，如 CVE-2024-1234，空则不过滤"`
	Keyword  string `json:"keyword,omitempty"  jsonschema:"description=按标题关键词模糊搜索，空则不过滤"`
	TimeFrom string `json:"time_from,omitempty" jsonschema:"description=开始时间，格式 2006-01-02，空则不限"`
	TimeTo   string `json:"time_to,omitempty"   jsonschema:"description=结束时间，格式 2006-01-02，空则不限"`
	Limit    int    `json:"limit,omitempty"    jsonschema:"description=返回条数，默认 10，最大 50"`
}

// EventResult 工具返回的事件信息（含 Milvus 中的完整内容）
type EventResult struct {
	ID        string  `json:"id"`
	Title     string  `json:"title"`
	Content   string  `json:"content"`    // 从 Milvus 获取的完整事件内容
	EventType string  `json:"event_type"` // 事件来源大类：github、rss
	Severity  string  `json:"severity"`
	Status    string  `json:"status"`
	CveID     string  `json:"cve_id"`
	RiskScore float64 `json:"risk_score"`
	Source    string  `json:"source"`
	Metadata  string  `json:"metadata"`
	CreatedAt string  `json:"created_at"`
	IndexedAt string  `json:"indexed_at"` // 是否已向量化
}

// NewQueryEventsTool 创建 query_events 工具，从 MySQL 查询事件元数据并从 Milvus 补充完整内容
func NewQueryEventsTool() tool.InvokableTool {
	t, err := utils.InferOptionableTool(
		"query_events",
		"Query security events from the database. Returns event metadata (severity, risk_score, cve_id, status) from MySQL and full content from Milvus vector store. Use this when analyzing recent events, checking severity distribution, or investigating specific CVEs.",
		func(ctx context.Context, input *QueryEventsInput, opts ...tool.Option) (output string, err error) {
			db, err := dao.DB(ctx)
			if err != nil {
				return "", fmt.Errorf("database not available: %w", err)
			}

			// 参数校验
			limit := input.Limit
			if limit <= 0 {
				limit = 10
			}
			if limit > 50 {
				limit = 50
			}

			// event_id 精确查询：直接按 ID 查单条，忽略其他过滤条件
			if input.EventID != "" {
				var event dao.Event
				if err = db.Where("id = ?", input.EventID).First(&event).Error; err != nil {
					return fmt.Sprintf(`{"error":"event not found: %s"}`, input.EventID), nil
				}
				contentMap := fetchMilvusContentByIDs(ctx, []string{event.ID})
				r := EventResult{
					ID:        event.ID,
					Title:     event.Title,
					Content:   contentMap[event.ID],
					EventType: event.EventType,
					Severity:  event.Severity,
					Status:    event.Status,
					CveID:     event.CVEID,
					RiskScore: event.RiskScore,
					Source:    event.Source,
					Metadata:  event.Metadata,
					CreatedAt: event.CreatedAt.Format("2006-01-02 15:04:05"),
				}
				if event.IndexedAt != nil {
					r.IndexedAt = event.IndexedAt.Format("2006-01-02 15:04:05")
				}
				var buf bytes.Buffer
				enc := json.NewEncoder(&buf)
				enc.SetEscapeHTML(false)
				_ = enc.Encode([]EventResult{r})
				return strings.TrimRight(buf.String(), "\n"), nil
			}

			// 只打印非空过滤条件，减少日志噪音
			var filters []string
			if input.Severity != "" {
				filters = append(filters, "severity="+input.Severity)
			}
			if input.Status != "" {
				filters = append(filters, "status="+input.Status)
			}
			if input.CveID != "" {
				filters = append(filters, "cve_id="+input.CveID)
			}
			if input.Keyword != "" {
				filters = append(filters, "keyword="+input.Keyword)
			}
			if input.TimeFrom != "" {
				filters = append(filters, "from="+input.TimeFrom)
			}
			if input.TimeTo != "" {
				filters = append(filters, "to="+input.TimeTo)
			}
			filterStr := "-"
			if len(filters) > 0 {
				filterStr = strings.Join(filters, " | ")
			}
			g.Log().Infof(ctx, "[Tool] query_events | filters=[%s] | limit=%d", filterStr, limit)

			// 构建查询条件
			q := db.Limit(limit).Order("created_at DESC")
			if input.Severity != "" {
				q = q.Where("severity = ?", input.Severity)
			}
			if input.Status != "" {
				q = q.Where("status = ?", input.Status)
			}
			if input.CveID != "" {
				q = q.Where("cve_id = ?", input.CveID)
			}
			if input.Keyword != "" {
				q = q.Where("title LIKE ?", "%"+input.Keyword+"%")
			}
			if input.TimeFrom != "" {
				if t, e := time.Parse("2006-01-02", input.TimeFrom); e == nil {
					q = q.Where("created_at >= ?", t)
				}
			}
			if input.TimeTo != "" {
				if t, e := time.Parse("2006-01-02", input.TimeTo); e == nil {
					q = q.Where("created_at < ?", t.AddDate(0, 0, 1))
				}
			}

			// 启用SQL日志记录
			q = q.Debug()

			var events []dao.Event
			if err = q.Find(&events).Error; err != nil {
				return "", fmt.Errorf("query events: %w", err)
			}

			if len(events) == 0 {
				return "[]", nil
			}

			// 收集已向量化事件的 ID，批量从 Milvus 获取完整内容
			var indexedIDs []string
			for _, e := range events {
				if e.IndexedAt != nil {
					indexedIDs = append(indexedIDs, e.ID)
				}
			}
			contentMap := fetchMilvusContentByIDs(ctx, indexedIDs)

			// 内容截断策略：结果集越大，单条内容越短，总体 token 保持在合理范围内
			//   ≤ 5  条：全量内容（深度分析场景）
			//   ≤ 20 条：最多 800 字符（中等分析）
			//   > 20 条：最多 200 字符（批量报告场景，以元数据为主）
			maxContentLen := 800
			switch {
			case len(events) <= 5:
				maxContentLen = -1 // 不截断
			case len(events) <= 20:
				maxContentLen = 800
			default:
				maxContentLen = 200
			}

			// 组装结果
			results := make([]EventResult, 0, len(events))
			for _, e := range events {
				content := contentMap[e.ID]
				if maxContentLen > 0 {
					if runes := []rune(content); len(runes) > maxContentLen {
						content = string(runes[:maxContentLen]) + "..."
					}
				}
				r := EventResult{
					ID:        e.ID,
					Title:     e.Title,
					Content:   content,
					EventType: e.EventType,
					Severity:  e.Severity,
					Status:    e.Status,
					CveID:     e.CVEID,
					RiskScore: e.RiskScore,
					Source:    e.Source,
					Metadata:  e.Metadata,
					CreatedAt: e.CreatedAt.Format("2006-01-02 15:04:05"),
				}
				if e.IndexedAt != nil {
					r.IndexedAt = e.IndexedAt.Format("2006-01-02 15:04:05")
				}
				results = append(results, r)
			}

			var buf bytes.Buffer
			enc := json.NewEncoder(&buf)
			enc.SetEscapeHTML(false)
			_ = enc.Encode(results)
			g.Log().Infof(ctx, "[Tool] query_events 完成 | 返回=%d 条 | 已索引=%d 条",
				len(results), len(indexedIDs))
			return strings.TrimRight(buf.String(), "\n"), nil
		})
	if err != nil {
		panic(err)
	}
	return t
}

// fetchMilvusContentByIDs 通过事件 ID 批量从 Milvus biz 集合查询事件完整内容。
// 返回 map[eventID]content，查询失败或 ID 不在 Milvus 中时对应 key 不存在。
func fetchMilvusContentByIDs(ctx context.Context, ids []string) map[string]string {
	result := make(map[string]string)
	if len(ids) == 0 {
		return result
	}

	milvusCli, err := milvus.GetClient(ctx)
	if err != nil {
		return result
	}

	// 构造过滤表达式：id in ["id1", "id2"]
	quoted := make([]string, len(ids))
	for i, id := range ids {
		quoted[i] = `"` + id + `"`
	}
	expr := fmt.Sprintf("id in [%s]", strings.Join(quoted, ", "))

	cols, err := milvusCli.Query(ctx, milvus.CollectionName, nil, expr, []string{"id", "content"})
	if err != nil {
		return result
	}

	// 解析列数据：id 列和 content 列长度一致，按下标对应
	var idData, contentData []string
	for _, col := range cols {
		vc, ok := col.(*entity.ColumnVarChar)
		if !ok {
			continue
		}
		switch col.Name() {
		case "id":
			idData = vc.Data()
		case "content":
			contentData = vc.Data()
		}
	}
	for i, id := range idData {
		if i < len(contentData) {
			result[id] = contentData[i]
		}
	}
	return result
}
