// 报告 Markdown 正文构建（遵循 NIST SP 800-61r3 格式）。
// 独立于 ReportModal：组件文件只导出组件，report 正文构建保持纯函数，
// 供 Event Analysis 页面、全局报告进度对话框与 ReportModal 共用。

export interface ReportEventDetail {
  id: number
  event_id: string
  title: string
  desc: string
  cve_id: string
  cvss: number
  severity: string
  vendor: string
  product: string
  source: string
  source_url: string
  recommendation?: string
  recommendationComplete?: boolean
}

export interface ReportBuildData {
  count: number
  maxCVSS: number
  avgRisk: number
  critical?: number
  highRisk?: number
  events?: ReportEventDetail[]
}

export type ReportBuildLog = { agent: string; message: string; status?: string; timestamp?: string }

const severityLabel: Record<string, string> = {
  critical: '严重', high: '高危', medium: '中危', low: '低危', info: '信息',
}
const SEVERITY_ORDER = ['critical', 'high', 'medium', 'low', 'info']

function groupBySeverity(events: ReportEventDetail[]): Record<string, ReportEventDetail[]> {
  const groups: Record<string, ReportEventDetail[]> = {}
  for (const ev of events) {
    const key = ev.severity || 'info'
    if (!groups[key]) groups[key] = []
    groups[key].push(ev)
  }
  return groups
}

export function buildMarkdown(data: ReportBuildData, logs: ReportBuildLog[]) {
  const now = new Date()
  const nowStr = now.toLocaleString('zh-CN', {
    year: 'numeric', month: '2-digit', day: '2-digit',
    hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false,
  })
  const reportId = `REPORT-${now.getFullYear()}${String(now.getMonth() + 1).padStart(2, '0')}${String(now.getDate()).padStart(2, '0')}-${String(now.getHours()).padStart(2, '0')}${String(now.getMinutes()).padStart(2, '0')}`
  const urgency = data.maxCVSS >= 9 ? '4小时内（P1）' : data.maxCVSS >= 7 ? '24小时内（P2）' : '72小时内（P3）'

  const events = data.events || []
  const groups = groupBySeverity(events)
  const criticalEvents = groups['critical'] || []
  const highEvents = groups['high'] || []
  const mediumEvents = groups['medium'] || []
  const lowEvents = [...(groups['low'] || []), ...(groups['info'] || [])]

  // ── 第二节：AI 解决方案（所有已生成方案的事件，按严重程度分组）──────
  const eventsWithSolution = events.filter(e => e.recommendationComplete && e.recommendation)
  const deepAnalysisSection = eventsWithSolution.length > 0
    ? SEVERITY_ORDER.flatMap(sev => {
        const sevEvts = eventsWithSolution.filter(e => e.severity === sev)
        if (sevEvts.length === 0) return []
        const label = severityLabel[sev] || sev
        return sevEvts.map((e) => [
          `### [${label.toUpperCase()}] ${e.title}`,
          '',
          `- **CVE：** ${e.cve_id || '暂无'} | **CVSS：** ${e.cvss || '-'} | **来源：** ${e.source || e.vendor || '-'}`,
          e.source_url ? `- **参考链接：** ${e.source_url}` : '',
          '',
          '**AI 应急处置方案：**',
          '',
          e.recommendation || '',
        ].filter(l => l !== undefined).join('\n'))
      }).join('\n\n')
    : '_当前无已完成的 AI 解决方案。_'

  // ── 第四节：完整事件清单（按严重程度分组）────────────────────────────
  const buildTable = (evts: ReportEventDetail[]) => {
    if (evts.length === 0) return ''
    return [
      '| # | 标题 | CVSS | CVE | 来源 | 修复方案 |',
      '|---|------|------|-----|------|---------|',
      ...evts.map((e, i) =>
        `| ${i + 1} | ${e.title} | ${e.cvss || '-'} | ${e.cve_id || '-'} | ${e.source || e.vendor || '-'} | ${e.recommendationComplete ? '✅ 已生成' : '-'} |`
      ),
    ].join('\n')
  }

  const eventListSection = [
    criticalEvents.length > 0 ? `### 严重漏洞（P1 - 4小时内响应）\n\n${buildTable(criticalEvents)}` : '',
    highEvents.length > 0 ? `### 高危漏洞（P2 - 24小时内响应）\n\n${buildTable(highEvents)}` : '',
    mediumEvents.length > 0 ? `### 中危漏洞（P3 - 72小时内响应）\n\n${buildTable(mediumEvents)}` : '',
    lowEvents.length > 0 ? `### 低危 / 信息（P4 - 计划处理）\n\n${buildTable(lowEvents)}` : '',
  ].filter(Boolean).join('\n\n') || '暂无事件数据'

  // ── 第五节：分阶段修复建议 ────────────────────────────────────────────
  const p1List = criticalEvents.map(e => `- [ ] ${e.title}${e.cve_id ? ` (${e.cve_id})` : ''}`).join('\n') || '- 无'
  const p2List = highEvents.map(e => `- [ ] ${e.title}${e.cve_id ? ` (${e.cve_id})` : ''}`).join('\n') || '- 无'
  const p3List = mediumEvents.map(e => `- [ ] ${e.title}`).join('\n') || '- 无'

  // ── 第六节：Agent 执行日志 ────────────────────────────────────────────
  const agentTrace = logs.length > 0
    ? [
        '| 时间 | Agent | 状态 | 消息 |',
        '|------|-------|------|------|',
        ...logs.map(log => {
          const icon = log.status === 'error' ? '失败' : '已完成'
          const timeStr = log.timestamp ? new Date(log.timestamp).toLocaleTimeString('zh-CN') : '-'
          return `| ${timeStr} | ${log.agent} | ${icon} | ${log.message} |`
        }),
      ].join('\n')
    : '| - | - | - | 暂无轨迹记录 |'

  return `# 安全事件分析报告

**报告编号：** ${reportId} | **生成时间：** ${nowStr} | **分析范围：** ${data.count} 个事件

---

## 一、执行摘要

| 指标 | 值 |
|------|-----|
| 分析事件总数 | ${data.count} 个 |
| 严重漏洞（P1）| ${data.critical ?? 0} 个 |
| 高危漏洞（P2）| ${data.highRisk ?? 0} 个 |
| 中危漏洞（P3）| ${(groups['medium'] || []).length} 个 |
| 最高 CVSS 评分 | ${data.maxCVSS} |
| 建议优先响应时间 | ${urgency} |

---

## 二、AI 解决方案

${deepAnalysisSection}

---

## 三、完整事件清单

${eventListSection}

---

## 四、分阶段修复建议

**P1 - 4小时内完成（严重漏洞）：**

${p1List}

**P2 - 24小时内完成（高危漏洞）：**

${p2List}

**P3 - 72小时内完成（中危漏洞）：**

${p3List}

---

## 五、Agent 执行日志

${agentTrace}

---

## 参考规范

NIST SP 800-61r3 | CVSS v3.1 | CWE Top 25 | ISO/IEC 27035

> 本报告有效期 30 天，请及时归档。`
}
