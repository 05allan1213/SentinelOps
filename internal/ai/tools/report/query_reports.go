package report

import (
	dao "SentinelOps/internal/dao/mysql"
	"context"
	"encoding/json"
	"fmt"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/gogf/gf/v2/frame/g"
)

// QueryReportsInput 查询报告参数
type QueryReportsInput struct {
	Type  string `json:"type" jsonschema:"description=报告类型: weekly, monthly, custom，空则不过滤"`
	Limit int    `json:"limit" jsonschema:"description=返回条数，默认10"`
}

// NewQueryReportsTool 创建 query_reports 工具，从 MySQL 查询分析报告
func NewQueryReportsTool() tool.InvokableTool {
	t, err := utils.InferOptionableTool(
		"query_reports",
		"Query analysis reports from the dao. Use when user asks about reports, weekly/monthly summaries, or report history.",
		func(ctx context.Context, input *QueryReportsInput, opts ...tool.Option) (output string, err error) {
			db, err := dao.DB(ctx)
			if err != nil {
				return "", fmt.Errorf("database not available: %w", err)
			}
			if input.Limit <= 0 {
				input.Limit = 10
			}
			g.Log().Infof(ctx, "[Tool] query_reports 开始 | type=%s | limit=%d", input.Type, input.Limit)
			var reports []dao.Report
			q := db.Limit(input.Limit).Order("created_at DESC")
			if input.Type != "" {
				q = q.Where("type = ?", input.Type)
			}
			if err = q.Find(&reports).Error; err != nil {
				return "", fmt.Errorf("query reports: %w", err)
			}
			g.Log().Infof(ctx, "[Tool] query_reports 完成 | 返回=%d 条", len(reports))
			b, _ := json.Marshal(reports)
			return string(b), nil
		})
	if err != nil {
		panic(err)
	}
	return t
}
