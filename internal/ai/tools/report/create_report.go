package report

import (
	dao "SentinelOps/internal/dao/mysql"
	"context"
	"fmt"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/google/uuid"
)

// CreateReportInput 创建报告参数
type CreateReportInput struct {
	Title   string `json:"title" jsonschema:"description=报告标题"`
	Content string `json:"content" jsonschema:"description=报告正文内容"`
	Type    string `json:"type" jsonschema:"description=报告类型: weekly, monthly, custom"`
}

// NewCreateReportTool 创建 create_report 工具，将报告写入 MySQL
func NewCreateReportTool() tool.InvokableTool {
	t, err := utils.InferOptionableTool(
		"create_report",
		"Create and save an analysis report to the dao. Use after generating report content. Required: title, content. Optional: type (weekly/monthly/custom), period.",
		func(ctx context.Context, input *CreateReportInput, opts ...tool.Option) (output string, err error) {
			db, err := dao.DB(ctx)
			if err != nil {
				return "", fmt.Errorf("database not available: %w", err)
			}
			if input.Title == "" {
				return "", fmt.Errorf("title is required")
			}
			if input.Type == "" {
				input.Type = "custom"
			}
			r := dao.Report{
				ID:      uuid.New().String(),
				Title:   input.Title,
				Content: input.Content,
				Type:    input.Type,
			}
			if err = db.Create(&r).Error; err != nil {
				return "", fmt.Errorf("create report: %w", err)
			}
			return fmt.Sprintf("Report created: id=%s, title=%s", r.ID, r.Title), nil
		})
	if err != nil {
		panic(err)
	}
	return t
}
