package report

import (
	"context"
	"encoding/json"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
)

// QueryReportTemplatesInput 查询报告模板参数
type QueryReportTemplatesInput struct {
	Type string `json:"type" jsonschema:"description=模板类型: weekly, vuln_alert, custom，空则返回全部"`
}

// reportTemplate 内置报告模板（不依赖数据库，硬编码维护）
type reportTemplate struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	Content string `json:"content"`
}

// builtinTemplates 内置模板列表，Report Agent 生成报告时参考格式
var builtinTemplates = []reportTemplate{
	{
		Name: "漏洞告警模板",
		Type: "vuln_alert",
		Content: `# 漏洞告警报告

> 生成时间：{date} | 严重等级：{severity}

---

## 一、漏洞概述

| 字段 | 值 |
|------|-----|
| CVE 编号 | {cve_id} |
| 严重程度 | {severity} |
| CVSS 评分 | {cvss} |
| 影响厂商 | {vendor} |

---

## 二、漏洞详情

{description}

---

## 三、受影响范围

{affected_scope}

---

## 四、修复建议

{recommendation}

---

## 五、参考链接

{references}`,
	},
	{
		Name: "周报模板",
		Type: "weekly",
		Content: `# 安全周报

> 统计周期：{start_date} ~ {end_date}

---

## 一、本周概况

| 指标 | 本周 | 上周 | 变化 |
|------|------|------|------|
| 新增事件 | {new_count} | - | - |
| 严重漏洞 | {critical_count} | - | - |
| 高危漏洞 | {high_count} | - | - |
| 已处置 | {resolved_count} | - | - |

---

## 二、重点事件

{top_events}

---

## 三、趋势分析

{trend_analysis}

---

## 四、下周关注点

{next_week_focus}`,
	},
	{
		Name: "分析报告模板",
		Type: "custom",
		Content: `# 安全分析报告

> 生成时间：{date}

---

## 一、执行摘要

{summary}

---

## 二、风险评估

{risk_assessment}

---

## 三、事件详情

{event_details}

---

## 四、处置建议

{recommendations}

---

## 五、结论

{conclusion}`,
	},
}

// NewQueryReportTemplatesTool 创建 query_report_templates 工具。
// 模板内容硬编码在代码中，无需数据库，Report Agent 可直接参考生成结构化报告。
func NewQueryReportTemplatesTool() tool.InvokableTool {
	t, err := utils.InferOptionableTool(
		"query_report_templates",
		"Query built-in report templates. Use when generating reports to reference template structure and format.",
		func(ctx context.Context, input *QueryReportTemplatesInput, opts ...tool.Option) (string, error) {
			result := builtinTemplates
			if input.Type != "" {
				filtered := make([]reportTemplate, 0)
				for _, tpl := range builtinTemplates {
					if tpl.Type == input.Type {
						filtered = append(filtered, tpl)
					}
				}
				result = filtered
			}
			b, _ := json.Marshal(result)
			return string(b), nil
		})
	if err != nil {
		panic(err)
	}
	return t
}
