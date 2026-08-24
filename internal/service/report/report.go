// Package reportsvc 提供安全报告管理业务逻辑。
// 职责：报告和模板的 CRUD，不含 HTTP 层细节。
package reportsvc

import (
	"context"

	dao "SentinelOps/internal/dao/mysql"

	"github.com/google/uuid"
)

// ListReports 返回报告列表，limit <= 0 时兜底为 20 条。
func ListReports(ctx context.Context, limit, offset int, reportType string) ([]dao.Report, int64, error) {
	if limit <= 0 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	return dao.ListReports(ctx, limit, offset, reportType)
}

// CreateReport 创建报告，type 为空时默认 "custom"。
// 返回新报告的 ID。
func CreateReport(ctx context.Context, title, content, reportType string) (string, error) {
	if reportType == "" {
		reportType = "custom"
	}
	r := &dao.Report{
		ID:      uuid.New().String(),
		Title:   title,
		Content: content,
		Type:    reportType,
	}
	if err := dao.CreateReport(ctx, r); err != nil {
		return "", err
	}
	return r.ID, nil
}

// BuiltinTemplate 内置报告模板（不依赖数据库）。
type BuiltinTemplate struct {
	ID      string
	Name    string
	Type    string
	Content string
}

var builtinTemplates = []BuiltinTemplate{
	{
		ID:   "tpl-vuln-alert",
		Name: "漏洞告警模板",
		Type: "vuln_alert",
		Content: `# 漏洞告警报告

## 概述
本报告汇总近期发现的漏洞告警信息。

## 高危漏洞列表
| CVE编号 | 漏洞名称 | 危险等级 | 影响组件 | 修复状态 |
|---------|---------|---------|---------|---------|

## 处置建议
- 立即对高危漏洞进行补丁修复
- 对受影响系统进行隔离排查
- 更新安全基线配置

## 参考链接
`,
	},
	{
		ID:   "tpl-weekly",
		Name: "安全周报模板",
		Type: "weekly",
		Content: `# 安全周报

## 本周概况
- 新增事件：
- 已处置：
- 待处理：

## 重点事件
### 高危事件

### 中危事件

## 趋势分析

## 下周计划
`,
	},
	{
		ID:   "tpl-custom",
		Name: "分析报告模板",
		Type: "custom",
		Content: `# 安全分析报告

## 执行摘要

## 事件详情

## 风险评估
| 风险项 | 等级 | 影响范围 | 建议措施 |
|-------|------|---------|---------|

## 结论与建议

## 附录
`,
	},
}

// ListTemplates 返回内置模板列表，templateType 为空时返回全部。
func ListTemplates(_ context.Context, templateType string) ([]BuiltinTemplate, error) {
	if templateType == "" {
		return builtinTemplates, nil
	}
	var result []BuiltinTemplate
	for _, t := range builtinTemplates {
		if t.Type == templateType {
			result = append(result, t)
		}
	}
	return result, nil
}

// GetReport 按 ID 查询单条报告。
func GetReport(ctx context.Context, id string) (*dao.Report, error) {
	return dao.GetReportByID(ctx, id)
}

// DeleteReport 软删除安全报告（保留历史数据）。
func DeleteReport(ctx context.Context, id string) error {
	return dao.DeleteReport(ctx, id)
}
