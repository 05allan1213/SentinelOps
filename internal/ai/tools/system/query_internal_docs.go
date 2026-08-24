package system

import (
	"SentinelOps/internal/ai/retrieval"
	"context"
	"encoding/json"
	"fmt"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/gogf/gf/v2/frame/g"
)

// QueryInternalDocsInput 内部文档检索参数
type QueryInternalDocsInput struct {
	Query string `json:"query" jsonschema:"description=The query string to search in internal documentation for relevant information and processing steps"`
}

// NewQueryInternalDocsTool 创建 query_internal_docs 工具，从知识库（Milvus）检索相关文档
func NewQueryInternalDocsTool() tool.InvokableTool {
	t, err := utils.InferOptionableTool(
		"query_internal_docs",
		"Use this tool to search internal documentation and knowledge base for relevant information. It performs RAG (Retrieval-Augmented Generation) to find similar documents and extract processing steps. This is useful when you need to understand internal procedures, best practices, or step-by-step guides stored in the company's documentation.",
		func(ctx context.Context, input *QueryInternalDocsInput, opts ...tool.Option) (output string, err error) {
			g.Log().Infof(ctx, "[Tool] query_internal_docs 开始 | query=%q", input.Query)
			// GetDocumentsRetriever 只检索 documents 分区，避免混入安全事件内容。
			rr := retrieval.GetDocumentsRetriever()
			resp, err := rr.Retrieve(ctx, input.Query)
			if err != nil {
				return "", fmt.Errorf("retrieve docs: %w", err)
			}
			// 过滤掉已禁用文档的分块
			resp = retrieval.FilterDisabledDocs(ctx, resp)
			g.Log().Infof(ctx, "[Tool] query_internal_docs 完成 | 返回=%d 条", len(resp))
			respBytes, _ := json.Marshal(resp)
			return string(respBytes), nil
		})
	if err != nil {
		panic(err)
	}
	return t
}
