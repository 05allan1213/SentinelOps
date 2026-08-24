// Package intelligence 情报收集工具：联网搜索、情报沉淀
package intelligence

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/gogf/gf/v2/frame/g"
)

// WebSearchEnabledKey 联网搜索开关的 context key。
// 控制器在 context 中注入 true 时，工具才会执行实际搜索；否则返回提示信息。
type WebSearchEnabledKey struct{}

// WebSearchInput web_search 工具输入参数
type WebSearchInput struct {
	Query      string `json:"query" jsonschema:"description=搜索关键词，如 CVE 编号、漏洞名称、攻击组织、恶意 IP 等"`
	MaxResults int    `json:"max_results,omitempty" jsonschema:"description=返回结果数量，默认 5，最大 10"`
}

// WebSearchResult 单条搜索结果，字段可直接用于情报入库
type WebSearchResult struct {
	Title         string `json:"title"`
	URL           string `json:"url"`
	Content       string `json:"content"`                  // Tavily AI 精选摘要（通常 200-800 字）
	PublishedDate string `json:"published_date,omitempty"` // 发布日期（格式 YYYY-MM-DD），情报时效性判断
	Answer        string `json:"answer,omitempty"`         // Tavily 对本次查询的综合摘要，所有结果共享同一值
}

// WebSearchOutput web_search 工具输出，除结果列表外附带 Tavily AI 总结
type WebSearchOutput struct {
	Answer  string            `json:"answer,omitempty"` // Tavily 对本次查询的 AI 综合摘要（需配置 include_answer: true）
	Results []WebSearchResult `json:"results"`
}

// NewWebSearchTool 创建联网搜索工具（基于 Tavily API）。
// 配置项（manifest/config/config.yaml）：
//   - tools.web_search.tavily_api_key  — Tavily API Key（必填）
//   - tools.web_search.tavily_base_url — 自定义端点，默认 https://api.tavily.com/search
//   - tools.web_search.tavily_search_depth — basic | advanced，默认 advanced
func NewWebSearchTool() tool.InvokableTool {
	t, err := utils.InferOptionableTool(
		"web_search",
		"Search the internet for security threat intelligence, CVE details, vulnerability information, or attack group activity. Returns structured results with titles, URLs, AI-curated content, relevance scores, and publish dates — suitable for storage and downstream analysis.",
		func(ctx context.Context, input *WebSearchInput, opts ...tool.Option) (string, error) {
			// 门控：仅在前端开启联网搜索时才实际调用 Tavily，否则返回提示让 LLM 基于本地知识作答
			if enabled, _ := ctx.Value(WebSearchEnabledKey{}).(bool); !enabled {
				return "联网搜索未开启，请仅基于本地知识库回答，不要尝试再次调用此工具。", nil
			}

			maxResults := input.MaxResults
			if maxResults <= 0 {
				maxResults = 5
			}
			if maxResults > 10 {
				maxResults = 10
			}

			tavilyKey, _ := g.Cfg().Get(ctx, "tools.web_search.tavily_api_key")
			if tavilyKey.String() == "" {
				return "", fmt.Errorf("Tavily API Key 未配置（tools.web_search.tavily_api_key）")
			}

			g.Log().Infof(ctx, "[Tool] web_search | query=%s | max=%d", input.Query, maxResults)

			baseURL, _ := g.Cfg().Get(ctx, "tools.web_search.tavily_base_url")
			depth, _ := g.Cfg().Get(ctx, "tools.web_search.tavily_search_depth")
			out, err := tavilySearch(ctx, tavilyKey.String(), baseURL.String(), depth.String(),
				input.Query, maxResults)
			if err != nil {
				g.Log().Warningf(ctx, "[Tool] web_search 搜索失败: %v", err)
				return "", fmt.Errorf("搜索失败: %w", err)
			}

			return formatSearchOutput(out), nil
		})
	if err != nil {
		panic(err)
	}
	return t
}

// tavilySearch 调用 Tavily Search API，返回结构化情报结果。
// 结果包含 AI 精选摘要（content）、相关性评分（score）、发布日期（published_date），可直接用于入库分析。
func tavilySearch(ctx context.Context, apiKey, baseURL, searchDepth, query string, maxResults int) (*WebSearchOutput, error) {
	if baseURL == "" {
		baseURL = "https://api.tavily.com/search"
	}
	if searchDepth == "" {
		searchDepth = "advanced"
	}

	reqBody := map[string]any{
		"query":          query,
		"max_results":    maxResults,
		"search_depth":   searchDepth,
		"include_answer": true,
	}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("构建请求体失败: %w", err)
	}

	client := &http.Client{Timeout: 20 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("tavily 请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("tavily HTTP %d: %s", resp.StatusCode, string(body))
	}

	// 限制响应体 512KB
	body, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	if err != nil {
		return nil, fmt.Errorf("读取 Tavily 响应失败: %w", err)
	}

	var tavilyResp struct {
		Answer  string `json:"answer"`
		Results []struct {
			Title   string  `json:"title"`
			URL     string  `json:"url"`
			Content string  `json:"content"`
			Score   float64 `json:"score"`
		} `json:"results"`
	}
	if err = json.Unmarshal(body, &tavilyResp); err != nil {
		return nil, fmt.Errorf("解析 Tavily 响应失败: %w", err)
	}

	output := &WebSearchOutput{
		Answer:  tavilyResp.Answer,
		Results: make([]WebSearchResult, 0, len(tavilyResp.Results)),
	}
	for _, r := range tavilyResp.Results {
		// 过滤低质量结果：相关性评分过低或正文内容过短（导航文本/空页面）
		if r.Score < 0.5 || len([]rune(r.Content)) < 100 {
			continue
		}
		output.Results = append(output.Results, WebSearchResult{
			Answer:  tavilyResp.Answer,
			Title:   r.Title,
			URL:     r.URL,
			Content: r.Content,
		})
	}
	return output, nil
}

// formatSearchOutput 将搜索结果格式化为结构化文本，供 LLM 作为工具返回值读取。
//
// 格式设计原则：
//   - answer（Tavily AI 综合摘要）置于最顶部，清晰标注为"综合摘要"
//     → LLM 可直接将其作为情报分析的主体内容写入 save_intelligence.content
//   - 详细来源编号排列，便于 LLM 引用（如"根据来源[2]..."）
//   - 纯文本格式比 JSON 更节省 token，且 LLM 对自然语言结构理解更好
func formatSearchOutput(out *WebSearchOutput) string {
	var sb strings.Builder

	// ── 综合摘要（最高优先级，直接可用于情报分析） ─────────────────────────
	if out.Answer != "" {
		sb.WriteString("【综合摘要】\n")
		sb.WriteString(out.Answer)
		sb.WriteString("\n\n")
	}

	if len(out.Results) == 0 {
		sb.WriteString("【详细来源】\n未找到相关结果。\n")
		return sb.String()
	}

	// ── 逐条来源 ──────────────────────────────────────────────────────────
	sb.WriteString(fmt.Sprintf("【详细来源（共 %d 条）】\n", len(out.Results)))
	for i, r := range out.Results {
		sb.WriteString(fmt.Sprintf("\n[%d] %s\n", i+1, r.Title))
		sb.WriteString(fmt.Sprintf("    链接：%s\n", r.URL))
		if r.PublishedDate != "" {
			sb.WriteString(fmt.Sprintf("    日期：%s\n", r.PublishedDate))
		}
		sb.WriteString(fmt.Sprintf("    内容：%s\n", r.Content))
	}

	return sb.String()
}
