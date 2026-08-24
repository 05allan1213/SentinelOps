// Package rerank 提供由 Routing 配置驱动的文档重排序客户端。
//
// 向量检索基于余弦相似度，擅长模糊语义匹配但不擅长精确相关性排序。
// Rerank 模型专为「查询-文档相关性」任务训练，从候选池中精选最相关文档，
// 实现「召回率+精准度」双提升。
//
// 架构位置：
//
//	MultiRetrieve → [候选文档池，~10个] → Rerank → [精选文档，3个] → LLM Prompt
package rerank

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"sync"

	aitrace "SentinelOps/internal/ai/trace"
	appconfig "SentinelOps/internal/config"

	"github.com/cloudwego/eino/schema"
	"github.com/gogf/gf/v2/frame/g"
)

// RerankResult 重排结果，携带文档和相关性分数。
type RerankResult struct {
	Doc   *schema.Document
	Score float64 // relevance_score，由当前 Rerank Provider 返回
}

var (
	globalRerankClient *Client
	rerankOnce         sync.Once
	rerankInitErr      error
)

// Client 文档重排序客户端（单例）。
type Client struct {
	apiKey   string
	baseURL  string
	model    string
	instruct string
}

func clientFromConfig(cfg *appconfig.Config) (*Client, error) {
	route, ok := cfg.Routing.Rerank["default"]
	if !ok {
		return nil, fmt.Errorf("routing.rerank.default is not configured")
	}
	provider, model, err := cfg.Resolve(route)
	if err != nil {
		return nil, err
	}
	if model.Driver != "dashscope_rerank" {
		return nil, fmt.Errorf("routing.rerank.default uses driver %s, want dashscope_rerank", model.Driver)
	}
	// Provider、端点和厂商模型 ID 均由当前路由解析，客户端不保存固定供应商名称。
	return &Client{
		apiKey:   provider.APIKey,
		baseURL:  provider.Endpoints.DashScopeRerank,
		model:    model.ModelID,
		instruct: route.Options.Instruct,
	}, nil
}

// GetClient 根据 routing.rerank.default 延迟初始化全局客户端。
func GetClient(ctx context.Context) (*Client, error) {
	rerankOnce.Do(func() {
		cfg, err := appconfig.Current()
		if err != nil {
			rerankInitErr = err
			return
		}
		client, err := clientFromConfig(cfg)
		if err != nil {
			rerankInitErr = err
			return
		}
		globalRerankClient = client
		g.Log().Infof(ctx, "[Rerank] client initialized | model=%s | baseURL=%s", client.model, client.baseURL)
	})
	return globalRerankClient, rerankInitErr
}

// ── API 请求/响应结构 ──────────────────────────────────────────────────────────
// 官方格式：扁平请求体，output.results 响应

type rerankRequest struct {
	Model     string   `json:"model"`
	Query     string   `json:"query"`
	Documents []string `json:"documents"`
	TopN      int      `json:"top_n"`
	Instruct  string   `json:"instruct,omitempty"`
}

type rerankDocument struct {
	Text string `json:"text"`
}

type rerankResult struct {
	Index          int            `json:"index"`
	RelevanceScore float64        `json:"relevance_score"`
	Document       rerankDocument `json:"document"`
}

type rerankResponse struct {
	Results []rerankResult `json:"results"`
	Output  struct {
		Results []rerankResult `json:"results"`
	} `json:"output"`
	Usage struct {
		TotalTokens int `json:"total_tokens"`
	} `json:"usage"`
	ID string `json:"id"`
}

type rerankErrorResponse struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
}

// Rerank API 限制常量
const (
	rerankMaxDocs       = 500     // 每次请求最大文档数
	rerankMaxDocTokens  = 4_000   // 单条文档最大 Token 数（超出由 API 自动截断）
	rerankMaxReqTokens  = 120_000 // 请求级最大 Token 数（Query×N + 所有文档 Token 之和）
	rerankCharsPerToken = 2       // 保守估算：2 字符 ≈ 1 token（中英混合场景）
)

// Rerank 调用当前路由选择的模型对文档按相关性重排序，返回 topN 个最相关文档。
// 任意环节失败时降级返回原始列表前 topN 个，保证主流程不中断。
func (r *Client) Rerank(ctx context.Context, query string, docs []*schema.Document, topN int) []RerankResult {
	if len(docs) == 0 {
		fallback := make([]RerankResult, len(docs))
		for i, d := range docs {
			fallback[i] = RerankResult{Doc: d, Score: 0}
		}
		return fallback
	}

	// ── 文档数量限制：超过 500 截断，避免 API 报错 ─────────────────────────────
	if len(docs) > rerankMaxDocs {
		g.Log().Warningf(ctx, "[Rerank] 文档数超限，截断 | %d → %d", len(docs), rerankMaxDocs)
		docs = docs[:rerankMaxDocs]
	}

	// ── 请求级 Token 总量估算：Query×N + 所有文档字符之和 ──────────────────────
	// 公式：Query Tokens × Document数量 + Document Tokens总和
	queryTokens := len([]rune(query)) / rerankCharsPerToken
	totalEstTokens := queryTokens * len(docs)
	for _, d := range docs {
		totalEstTokens += len([]rune(d.Content)) / rerankCharsPerToken
	}
	if totalEstTokens > rerankMaxReqTokens {
		// 按比例缩减文档数，确保估算 Token 不超限
		ratio := float64(rerankMaxReqTokens) / float64(totalEstTokens)
		newDocCount := int(float64(len(docs)) * ratio)
		if newDocCount < 1 {
			newDocCount = 1
		}
		g.Log().Warningf(ctx, "[Rerank] 请求 Token 估算超限，缩减文档 | 估算=%d tokens | docs %d → %d",
			totalEstTokens, len(docs), newDocCount)
		docs = docs[:newDocCount]
	}

	// topN <= 0 无意义；topN >= len(docs) 无需截断也无需重排，直接返回
	if topN <= 0 || topN >= len(docs) {
		g.Log().Debugf(ctx, "[Rerank] 跳过重排 | topN=%d | docs=%d", topN, len(docs))
		fallback := make([]RerankResult, len(docs))
		for i, d := range docs {
			fallback[i] = RerankResult{Doc: d, Score: 0}
		}
		return fallback
	}

	// ── 以下需要调用 Rerank API，开启 trace span ──────────────────────────────
	spanCtx, spanID := aitrace.StartSpan(ctx, aitrace.NodeTypeRerank, r.model)
	var traceTokens int
	var traceErr error
	defer func() {
		g.Log().Debugf(ctx, "[Rerank] FinishSpanWithCost 调用 | model=%s | tokens=%d | spanID=%s", r.model, traceTokens, spanID)
		aitrace.FinishSpanWithCost(spanCtx, spanID, r.model, traceTokens, 0, traceErr)
	}()

	texts := make([]string, len(docs))
	for i, d := range docs {
		texts[i] = d.Content
	}

	body, err := json.Marshal(rerankRequest{
		Model:     r.model,
		Query:     query,
		Documents: texts,
		TopN:      topN,
		Instruct:  r.instruct,
	})
	if err != nil {
		g.Log().Warningf(ctx, "[Rerank] 序列化失败，降级: %v", err)
		traceErr = err
		n := topN
		if n > len(docs) {
			n = len(docs)
		}
		fallback := make([]RerankResult, n)
		for i := range fallback {
			fallback[i] = RerankResult{Doc: docs[i], Score: 0}
		}
		return fallback
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.baseURL+"/reranks", bytes.NewReader(body))
	if err != nil {
		g.Log().Warningf(ctx, "[Rerank] 构建请求失败，降级: %v", err)
		traceErr = err
		n := topN
		if n > len(docs) {
			n = len(docs)
		}
		fallback := make([]RerankResult, n)
		for i := range fallback {
			fallback[i] = RerankResult{Doc: docs[i], Score: 0}
		}
		return fallback
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+r.apiKey)
	g.Log().Debugf(ctx, "[Rerank] 发送请求 | url=%s | body_len=%d",
		req.URL.String(), len(body))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		g.Log().Warningf(ctx, "[Rerank] 请求失败，降级: %v", err)
		traceErr = err
		n := topN
		if n > len(docs) {
			n = len(docs)
		}
		fallback := make([]RerankResult, n)
		for i := range fallback {
			fallback[i] = RerankResult{Doc: docs[i], Score: 0}
		}
		return fallback
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		g.Log().Warningf(ctx, "[Rerank] 读取响应失败，降级: %v", err)
		traceErr = err
		n := topN
		if n > len(docs) {
			n = len(docs)
		}
		fallback := make([]RerankResult, n)
		for i := range fallback {
			fallback[i] = RerankResult{Doc: docs[i], Score: 0}
		}
		return fallback
	}

	// 非 200 时打印完整响应体，便于排查问题
	if resp.StatusCode != http.StatusOK {
		var errResp rerankErrorResponse
		if json.Unmarshal(data, &errResp) == nil && errResp.Code != "" {
			g.Log().Warningf(ctx, "[Rerank] API错误，降级 | status=%d | code=%s | msg=%s | requestId=%s | rawBody=%s",
				resp.StatusCode, errResp.Code, errResp.Message, errResp.RequestID, string(data))
		} else {
			g.Log().Warningf(ctx, "[Rerank] API错误，降级 | status=%d | rawBody=%s", resp.StatusCode, string(data))
		}
		traceErr = fmt.Errorf("[Rerank] API error status=%d", resp.StatusCode)
		n := topN
		if n > len(docs) {
			n = len(docs)
		}
		fallback := make([]RerankResult, n)
		for i := range fallback {
			fallback[i] = RerankResult{Doc: docs[i], Score: 0}
		}
		return fallback
	}

	var rerankResp rerankResponse
	if err := json.Unmarshal(data, &rerankResp); err != nil {
		g.Log().Warningf(ctx, "[Rerank] 解析响应失败，降级: %v", err)
		traceErr = err
		n := topN
		if n > len(docs) {
			n = len(docs)
		}
		fallback := make([]RerankResult, n)
		for i := range fallback {
			fallback[i] = RerankResult{Doc: docs[i], Score: 0}
		}
		return fallback
	}

	results := rerankResp.Results
	if len(results) == 0 {
		results = rerankResp.Output.Results
	}
	if len(results) == 0 {
		g.Log().Warningf(ctx, "[Rerank] 响应结果为空，降级")
		// API 已成功响应并消耗了 token，仍记录用量
		traceTokens = rerankResp.Usage.TotalTokens
		n := topN
		if n > len(docs) {
			n = len(docs)
		}
		fallback := make([]RerankResult, n)
		for i := range fallback {
			fallback[i] = RerankResult{Doc: docs[i], Score: 0}
		}
		return fallback
	}

	// 按 relevance_score 降序排列（API 通常已排好序，此处做保险排序）
	sort.Slice(results, func(i, j int) bool {
		return results[i].RelevanceScore > results[j].RelevanceScore
	})

	// 按排序后的 index 从原始文档列表中映射回 RerankResult
	result := make([]RerankResult, 0, topN)
	for _, res := range results {
		if res.Index >= 0 && res.Index < len(docs) {
			result = append(result, RerankResult{Doc: docs[res.Index], Score: res.RelevanceScore})
		}
		if len(result) >= topN {
			break
		}
	}

	g.Log().Debugf(ctx, "[Rerank] 重排完成 | 输入=%d | 输出=%d | tokens=%d | id=%s",
		len(docs), len(result), rerankResp.Usage.TotalTokens, rerankResp.ID)
	// 记录成功的 token 消耗（由 defer 中的 FinishSpanWithCost 写入 trace）
	traceTokens = rerankResp.Usage.TotalTokens
	return result
}
