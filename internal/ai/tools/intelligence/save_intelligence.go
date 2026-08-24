// Package intelligence 情报收集工具：联网搜索、情报沉淀
package intelligence

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"SentinelOps/internal/ai/policy"
	"SentinelOps/internal/service/pipeline"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/google/uuid"

	dao "SentinelOps/internal/dao/mysql"
)

// SaveIntelligenceInput save_intelligence 工具输入参数
type SaveIntelligenceInput struct {
	Title     string `json:"title" jsonschema:"description=情报标题，简洁明确，包含 CVE ID 或关键词"`
	Content   string `json:"content" jsonschema:"description=威胁分析核心摘要（必须控制在600字以内），包含：漏洞原理/攻击向量/影响版本/修复建议，禁止传入完整长报告"`
	Severity  string `json:"severity" jsonschema:"description=严重程度：critical（CVSS 9.0+）/ high（7.0-8.9）/ medium（4.0-6.9）/ low（0-3.9）"`
	CVEID     string `json:"cve_id,omitempty" jsonschema:"description=CVE 编号，如 CVE-2024-1234，可选"`
	Source    string `json:"source,omitempty" jsonschema:"description=情报来源标识，由系统自动填充，无需手动指定"`
	SourceURL string `json:"source_url,omitempty" jsonschema:"description=原始来源 URL，如 NVD 漏洞详情页链接"`
}

// SaveIntelligenceOutput save_intelligence 工具输出
type SaveIntelligenceOutput struct {
	ID      string `json:"id"`
	Message string `json:"message"`
}

// NewSaveIntelligenceTool 创建情报沉淀工具（写入 MySQL，触发异步 Milvus 向量化）
func NewSaveIntelligenceTool() tool.InvokableTool {
	t, err := utils.InferOptionableTool(
		"save_intelligence",
		"Save analyzed threat intelligence to the local knowledge base (MySQL). The data will be automatically vectorized and indexed into Milvus, making it available for future semantic searches. If a record with the same CVE ID already exists, it will be updated with the latest analysis instead of creating a duplicate.",
		func(ctx context.Context, input *SaveIntelligenceInput, opts ...tool.Option) (string, error) {
			if err := policy.Authorize(ctx, policy.PermissionBusinessWrite, policy.Resource{}); err != nil {
				return "", err
			}
			// 参数验证
			if strings.TrimSpace(input.Title) == "" {
				return "", fmt.Errorf("title 不能为空")
			}
			if strings.TrimSpace(input.Content) == "" {
				return "", fmt.Errorf("content 不能为空")
			}
			// 服务端截断兜底：防止 LLM 生成超长 content 导致工具调用 JSON 被截断
			if runes := []rune(input.Content); len(runes) > 800 {
				input.Content = string(runes[:800]) + "…（内容已截断）"
			}

			// 规范化 severity
			severity := strings.ToLower(strings.TrimSpace(input.Severity))
			switch severity {
			case "critical", "high", "medium", "low":
				// 合法值，保持原值
			default:
				severity = "medium" // 未知等级默认 medium
			}

			// 来源标识固定为 web_search（情报工具通过联网搜索获取）
			source := strings.TrimSpace(input.Source)
			if source == "" {
				source = "Web Search"
			}

			g.Log().Infof(ctx, "[Tool] save_intelligence | title=%s | severity=%s | cve=%s",
				input.Title, severity, input.CVEID)

			// 构造 metadata，包含原始来源 URL
			metaMap := map[string]string{}
			if input.SourceURL != "" {
				metaMap["link"] = input.SourceURL
				metaMap["source_url"] = input.SourceURL
			}
			metaJSON := "{}"
			if len(metaMap) > 0 {
				if b, err := json.Marshal(metaMap); err == nil {
					metaJSON = string(b)
				}
			}

			riskScore := pipeline.SeverityToRiskScore(severity)
			cveID := strings.TrimSpace(input.CVEID)

			// ── CVEID 去重更新逻辑 ────────────────────────────────────────────
			// 若提供了 CVE ID，先查库是否已有同 CVE 的情报记录：
			// 有 → 更新分析内容（severity、risk_score、metadata），触发 Milvus 重新向量化
			// 无 → 走正常入库流程
			if cveID != "" {
				existing, err := dao.FindIntelligenceByCVEID(ctx, cveID)
				if err != nil {
					g.Log().Warningf(ctx, "[Tool] save_intelligence 查询已有记录失败，降级为插入: %v", err)
				} else if existing != nil {
					// 已有记录：更新字段
					if err = dao.UpdateIntelligenceFields(ctx, existing.ID, severity, riskScore, metaJSON); err != nil {
						g.Log().Warningf(ctx, "[Tool] save_intelligence 更新失败: %v", err)
						return "", fmt.Errorf("情报更新失败: %w", err)
					}
					// 更新最新的内容，同步 DB 已写入的字段，确保 Milvus 向量元数据与 DB 一致
					existing.Content = strings.TrimSpace(input.Content)
					existing.Severity = severity
					existing.EventType = "web"

					// 向量内容异步更新
					pipeline.IndexDocumentsAsync(ctx, []dao.Event{*existing})

					out := SaveIntelligenceOutput{
						ID:      existing.ID,
						Message: fmt.Sprintf("情报已更新（ID: %s，CVE: %s），已触发重新向量索引", existing.ID, cveID),
					}
					b, _ := json.MarshalIndent(out, "", "  ")
					g.Log().Infof(ctx, "[Tool] save_intelligence 更新成功 | id=%s | cve=%s", existing.ID, cveID)
					return string(b), nil
				}
			}

			// ── 首次入库流程 ──────────────────────────────────────────────────
			e := dao.Event{
				ID:        uuid.New().String(),
				Title:     strings.TrimSpace(input.Title),
				Content:   strings.TrimSpace(input.Content),
				EventType: "web",
				Severity:  severity,
				Source:    source,
				Status:    "new",
				CVEID:     cveID,
				RiskScore: riskScore,
				Metadata:  metaJSON,
			}

			// 去重入库（DedupAndInsert 按 title|source|content 前500字 SHA256 去重）
			inserted, err := pipeline.DedupAndInsert(ctx, []dao.Event{e})
			if err != nil {
				g.Log().Warningf(ctx, "[Tool] save_intelligence 入库失败: %v", err)
				return "", fmt.Errorf("情报入库失败: %w", err)
			}

			if len(inserted) == 0 {
				out := SaveIntelligenceOutput{
					ID:      "",
					Message: "情报已存在（相同内容已记录在知识库中），无需重复保存",
				}
				b, _ := json.MarshalIndent(out, "", "  ")
				return string(b), nil
			}

			// 触发异步 Milvus 向量化
			pipeline.IndexDocumentsAsync(ctx, inserted)

			out := SaveIntelligenceOutput{
				ID:      inserted[0].ID,
				Message: fmt.Sprintf("情报已保存（ID: %s），已触发异步向量索引，即将在知识库中可用", inserted[0].ID),
			}
			b, _ := json.MarshalIndent(out, "", "  ")

			g.Log().Infof(ctx, "[Tool] save_intelligence 成功 | id=%s | title=%s", inserted[0].ID, input.Title)
			return string(b), nil
		})
	if err != nil {
		panic(err)
	}
	return t
}
