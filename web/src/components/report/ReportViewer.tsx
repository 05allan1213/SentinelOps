import { ExternalLink } from 'lucide-react'
import ReactMarkdown from 'react-markdown'
import { cn, normalizeMarkdown } from '@/utils'

export interface ReportViewerEvent {
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

export interface ReportViewerData {
  count: number
  maxCVSS: number
  avgRisk: number
  critical?: number
  highRisk?: number
  events?: ReportViewerEvent[]
}

export interface ReportViewerLog {
  agent: string
  message: string
  status?: string
  timestamp?: string
}

const severityLabel: Record<string, string> = {
  critical: '严重', high: '高危', medium: '中危', low: '低危', info: '信息',
}

const severityClass: Record<string, string> = {
  critical: 'severity-critical',
  high: 'severity-high',
  medium: 'severity-medium',
  low: 'severity-low',
}

const SEVERITY_ORDER = ['critical', 'high', 'medium', 'low', 'info']

function groupBySeverity(events: ReportViewerEvent[]): Record<string, ReportViewerEvent[]> {
  const groups: Record<string, ReportViewerEvent[]> = {}
  for (const ev of events) {
    const key = ev.severity || 'info'
    if (!groups[key]) groups[key] = []
    groups[key].push(ev)
  }
  return groups
}

interface Props {
  data: ReportViewerData
  logs: ReportViewerLog[]
}

export default function ReportViewer({ data, logs }: Props) {
  const events = data.events || []
  const groups = groupBySeverity(events)
  const urgency = data.maxCVSS >= 9 ? '4小时内（P1）' : data.maxCVSS >= 7 ? '24小时内（P2）' : '72小时内（P3）'

  return (
    <div className="space-y-5">
      {/* 一、执行摘要 */}
      <div>
        <div className="text-sm font-semibold text-gray-800 mb-2">一、执行摘要</div>
        <div className={cn(
          'p-4 rounded-xl border grid grid-cols-2 sm:grid-cols-4 gap-3',
          data.maxCVSS >= 9 ? 'bg-red-50 border-red-200' : data.maxCVSS >= 7 ? 'bg-orange-50 border-orange-200' : 'bg-blue-50 border-blue-200'
        )}>
          {[
            { label: '分析事件', value: `${data.count} 个` },
            { label: '严重漏洞（P1）', value: `${data.critical ?? 0} 个`, highlight: (data.critical ?? 0) > 0 },
            { label: '高危漏洞（P2）', value: `${data.highRisk ?? 0} 个`, highlight: (data.highRisk ?? 0) > 0 },
            { label: '建议响应', value: urgency },
          ].map(({ label, value, highlight }) => (
            <div key={label} className="text-center">
              <div className="text-xs text-gray-500 mb-1">{label}</div>
              <div className={cn('text-sm font-bold', highlight ? 'text-red-600' : 'text-gray-800')}>{value}</div>
            </div>
          ))}
        </div>
      </div>

      {/* 二、AI 解决方案 */}
      {(() => {
        const eventsWithSolution = events.filter(e => e.recommendationComplete && e.recommendation)
        const noSolutionKeyEvents = [...(groups['critical'] || []), ...(groups['high'] || [])].filter(e => !e.recommendationComplete)
        if (eventsWithSolution.length === 0 && noSolutionKeyEvents.length === 0) return null
        return (
          <div>
            <div className="text-sm font-semibold text-gray-800 mb-2">
              二、AI 解决方案
              <span className="ml-2 text-xs font-normal text-gray-400">（{eventsWithSolution.length} 个事件已生成方案）</span>
            </div>
            <div className="space-y-3">
              {SEVERITY_ORDER.flatMap(sev => {
                const sevEvts = eventsWithSolution.filter(e => e.severity === sev)
                return sevEvts.map((e) => (
                  <div key={e.event_id} className="rounded-xl border border-gray-200 overflow-hidden">
                    <div className={cn(
                      'px-4 py-2.5 flex items-center gap-2 border-b border-gray-100',
                      e.severity === 'critical' ? 'bg-red-50' :
                      e.severity === 'high' ? 'bg-orange-50' :
                      e.severity === 'medium' ? 'bg-yellow-50' : 'bg-gray-50'
                    )}>
                      <span className={severityClass[e.severity] || 'tag-default'}>
                        {severityLabel[e.severity] || e.severity}
                      </span>
                      <span className="text-sm font-medium text-gray-800 flex-1 truncate">{e.title}</span>
                      {e.cvss && <span className="text-xs text-gray-500 shrink-0">CVSS {e.cvss}</span>}
                      {e.cve_id && <span className="text-xs font-mono text-gray-400 shrink-0">{e.cve_id}</span>}
                      {e.source_url && (
                        <a href={e.source_url} target="_blank" rel="noopener noreferrer"
                          className="text-gray-300 hover:text-primary-500 transition-colors shrink-0">
                          <ExternalLink className="w-3.5 h-3.5" />
                        </a>
                      )}
                    </div>
                    <div className="px-4 py-3">
                      <div className="text-xs text-gray-500 font-medium mb-2">AI 应急处置方案</div>
                      <div className="prose prose-sm max-w-none text-xs text-gray-700 leading-relaxed">
                        <ReactMarkdown>{normalizeMarkdown(e.recommendation || '')}</ReactMarkdown>
                      </div>
                    </div>
                  </div>
                ))
              })}
              {noSolutionKeyEvents.map((e) => (
                <div key={e.event_id} className="rounded-xl border border-gray-200 overflow-hidden opacity-60">
                  <div className={cn(
                    'px-4 py-2.5 flex items-center gap-2',
                    e.severity === 'critical' ? 'bg-red-50' : 'bg-orange-50'
                  )}>
                    <span className={severityClass[e.severity] || 'tag-default'}>
                      {severityLabel[e.severity] || e.severity}
                    </span>
                    <span className="text-sm font-medium text-gray-800 flex-1 truncate">{e.title}</span>
                    <span className="text-xs text-gray-400 italic shrink-0">方案未生成</span>
                  </div>
                </div>
              ))}
            </div>
          </div>
        )
      })()}

      {/* 三、完整事件清单（按严重程度分组） */}
      {events.length > 0 && (
        <div>
          <div className="text-sm font-semibold text-gray-800 mb-2">三、完整事件清单</div>
          <div className="space-y-3">
            {SEVERITY_ORDER.map(sev => {
              const grpEvents = groups[sev]
              if (!grpEvents?.length) return null
              const grpLabel = sev === 'critical' ? '严重漏洞（P1）'
                : sev === 'high' ? '高危漏洞（P2）'
                : sev === 'medium' ? '中危漏洞（P3）'
                : '低危 / 信息（P4）'
              return (
                <div key={sev}>
                  <div className="text-xs font-medium text-gray-800 mb-1.5">{grpLabel}</div>
                  <div className="rounded-xl border border-gray-300 overflow-hidden">
                    <table className="w-full text-xs">
                      <thead className="bg-gray-50 border-b border-gray-200">
                        <tr>
                          <th className="px-3 py-2 text-left text-gray-800 font-medium w-7">#</th>
                          <th className="px-3 py-2 text-left text-gray-800 font-medium">标题</th>
                          <th className="px-3 py-2 text-left text-gray-800 font-medium w-12">CVSS</th>
                          <th className="px-3 py-2 text-left text-gray-800 font-medium w-28">CVE</th>
                          <th className="px-3 py-2 text-left text-gray-800 font-medium w-20">来源</th>
                          <th className="px-3 py-2 text-left text-gray-800 font-medium w-20">修复方案</th>
                          <th className="px-3 py-2 w-6"></th>
                        </tr>
                      </thead>
                      <tbody className="divide-y divide-gray-100">
                        {grpEvents.map((e, i) => (
                          <tr key={e.id} className="hover:bg-gray-50">
                            <td className="px-3 py-2 text-gray-400">{i + 1}</td>
                            <td className="px-3 py-2 max-w-[220px]">
                              <span className="truncate block text-gray-800">{e.title}</span>
                            </td>
                            <td className="px-3 py-2 text-gray-800">{e.cvss || '-'}</td>
                            <td className="px-3 py-2 font-mono text-gray-800 truncate">{e.cve_id || '-'}</td>
                            <td className="px-3 py-2 text-gray-800 truncate">{e.source || e.vendor || '-'}</td>
                            <td className="px-3 py-2">
                              {e.recommendationComplete
                                ? <span className="text-green-600 font-medium">已生成</span>
                                : <span className="text-gray-800">-</span>
                              }
                            </td>
                            <td className="px-3 py-2">
                              {e.source_url && (
                                <a href={e.source_url} target="_blank" rel="noopener noreferrer"
                                  className="text-gray-300 hover:text-primary-500 transition-colors">
                                  <ExternalLink className="w-3 h-3" />
                                </a>
                              )}
                            </td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                </div>
              )
            })}
          </div>
        </div>
      )}

      {/* 四、Agent 执行日志 */}
      {logs.length > 0 && (
        <div>
          <div className="text-sm font-semibold text-gray-800 mb-2">四、Agent 执行日志</div>
          <div className="rounded-xl border border-gray-200 overflow-hidden">
            <table className="w-full text-xs">
              <thead className="bg-gray-50 border-b border-gray-200">
                <tr>
                  <th className="px-3 py-2 text-left text-gray-800 font-medium w-20">时间</th>
                  <th className="px-3 py-2 text-left text-gray-800 font-medium w-28">Agent</th>
                  <th className="px-3 py-2 text-left text-gray-800 font-medium w-12">状态</th>
                  <th className="px-3 py-2 text-left text-gray-800 font-medium">消息</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-gray-100">
                {logs.map((log, i) => {
                  const statusText = log.status === 'error' ? '失败' : '已完成'
                  const timeStr = log.timestamp ? new Date(log.timestamp).toLocaleTimeString('zh-CN') : '-'
                  return (
                    <tr key={i} className="hover:bg-gray-50">
                      <td className="px-3 py-2 text-gray-800 font-mono whitespace-nowrap">{timeStr}</td>
                      <td className="px-3 py-2 text-gray-800 font-medium">{log.agent}</td>
                      <td className="px-3 py-2 text-green-600 font-medium whitespace-nowrap">{statusText}</td>
                      <td className="px-3 py-2 text-gray-800">{log.message}</td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
        </div>
      )}

      {/* 参考规范 */}
      <div className="text-xs text-gray-400 border-t border-gray-100 pt-3">
        参考规范：NIST SP 800-61r3 | CVSS v3.1 | CWE Top 25 | ISO/IEC 27035 &nbsp;·&nbsp; 本报告有效期 30 天
      </div>
    </div>
  )
}
