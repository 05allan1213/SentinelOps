import { buildMarkdown } from '@/pages/event-analysis/reportMarkdown'

const data = {
  count: 2,
  maxCVSS: 9.8,
  avgRisk: 74,
  critical: 1,
  highRisk: 1,
  events: [
    {
      id: 1, event_id: 'evt-1', title: 'Log4Shell 利用尝试', desc: 'd', cve_id: 'CVE-2021-44228',
      cvss: 9.8, severity: 'critical', vendor: 'Apache', product: 'Log4j', source: 'NVD', source_url: 'https://nvd.example/CVE-2021-44228',
      recommendation: '升级到 2.17.1 并轮换密钥', recommendationComplete: true,
    },
    {
      id: 2, event_id: 'evt-2', title: '可疑登录', desc: 'd', cve_id: '', cvss: 7.1, severity: 'high',
      vendor: 'Contoso', product: 'VPN', source: '', source_url: '', recommendationComplete: false,
    },
  ],
}

it('keeps the report structure, severity grouping and agent trace table', () => {
  const md = buildMarkdown(data, [
    { agent: 'planner', message: 'plan ready', status: 'success', timestamp: '2026-09-14T08:00:00Z' },
    { agent: 'executor', message: 'tool failed', status: 'error' },
  ])

  expect(md).toContain('# 安全事件分析报告')
  expect(md).toMatch(/\*\*报告编号：\*\* REPORT-\d{8}-\d{4}/)
  expect(md).toContain('| 分析事件总数 | 2 个 |')
  expect(md).toContain('| 最高 CVSS 评分 | 9.8 |')
  expect(md).toContain('| 建议优先响应时间 | 4小时内（P1） |')
  expect(md).toContain('### [严重] Log4Shell 利用尝试')
  expect(md).toContain('升级到 2.17.1 并轮换密钥')
  expect(md).toContain('### 严重漏洞（P1 - 4小时内响应）')
  expect(md).toContain('### 高危漏洞（P2 - 24小时内响应）')
  expect(md).toContain('| 1 | Log4Shell 利用尝试 | 9.8 | CVE-2021-44228 | NVD | ✅ 已生成 |')
  expect(md).toContain('- [ ] Log4Shell 利用尝试 (CVE-2021-44228)')
  expect(md).toContain('| planner | 已完成 | plan ready |')
  expect(md).toContain('| executor | 失败 | tool failed |')
  expect(md).toContain('NIST SP 800-61r3 | CVSS v3.1 | CWE Top 25 | ISO/IEC 27035')
})

it('falls back to the empty-state text when nothing was analysed', () => {
  const md = buildMarkdown({ count: 0, maxCVSS: 0, avgRisk: 0 }, [])
  expect(md).toContain('_当前无已完成的 AI 解决方案。_')
  expect(md).toContain('暂无事件数据')
  expect(md).toContain('| - | - | - | 暂无轨迹记录 |')
  expect(md).toContain('| 建议优先响应时间 | 72小时内（P3） |')
})
