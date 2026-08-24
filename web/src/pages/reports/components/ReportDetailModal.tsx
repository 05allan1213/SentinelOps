import { X, Download, Copy, Check, Calendar, FileText, AlertTriangle } from 'lucide-react'
import { useState } from 'react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { formatDate, parseReportPayload, normalizeMarkdown } from '@/utils'
import ReportViewer from '@/components/report/ReportViewer'

interface ReportDetailModalProps {
  report: {
    id: string
    title: string
    type: string
    summary?: string
    content: string
    event_count: number
    critical_count?: number
    high_count?: number
    status?: string
    created_at: string
  } | null
  onClose: () => void
}

const reportTypeLabels: Record<string, string> = {
  vuln_alert: '漏洞告警',
  weekly: '周报',
  custom: '分析报告',
}

export default function ReportDetailModal({ report, onClose }: ReportDetailModalProps) {
  const [copied, setCopied] = useState(false)

  if (!report) return null

  // 解析结构化 payload；旧格式（纯 Markdown）降级处理
  const payload = parseReportPayload(report.content)
  const displayMarkdown = payload?.markdown ?? report.content
  const eventCount = payload?.meta.event_count ?? report.event_count
  const criticalCount = payload?.meta.critical_count ?? report.critical_count ?? 0
  const highCount = payload?.meta.high_count ?? 0

  const handleCopy = async () => {
    await navigator.clipboard.writeText(displayMarkdown)
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }

  const handleDownload = (format: 'md' | 'html' | 'json') => {
    if (!report) return
    let content = ''
    let mimeType = ''
    let extension = ''

    switch (format) {
      case 'md':
        content = displayMarkdown
        mimeType = 'text/markdown'
        extension = 'md'
        break
      case 'html':
        content = `<!DOCTYPE html>
<html>
<head>
<meta charset="UTF-8">
<title>${report.title}</title>
<style>body{font-family:system-ui;max-width:800px;margin:0 auto;padding:20px;}</style>
</head>
<body>
<h1>${report.title}</h1>
<p><strong>类型:</strong> ${reportTypeLabels[report.type]}</p>
<p><strong>生成时间:</strong> ${formatDate(report.created_at)}</p>
<hr>
${displayMarkdown.replace(/\n/g, '<br>')}
</body>
</html>`
        mimeType = 'text/html'
        extension = 'html'
        break
      case 'json':
        if (payload) {
          // 导出结构化数据，去掉冗余的 markdown 字段（可从 risk_data 重新生成）
          const { markdown: _, ...cleanPayload } = payload
          content = JSON.stringify(cleanPayload, null, 2)
        } else {
          content = JSON.stringify({ title: report.title, created_at: report.created_at, content: displayMarkdown }, null, 2)
        }
        mimeType = 'application/json'
        extension = 'json'
        break
    }

    const blob = new Blob([content], { type: mimeType })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `${report.title}.${extension}`
    document.body.appendChild(a)
    a.click()
    document.body.removeChild(a)
    URL.revokeObjectURL(url)
  }

  return (
    <div className="modal-overlay" onClick={onClose}>
      <div
        className="modal w-full max-w-4xl max-h-[90vh] flex flex-col"
        onClick={(e) => e.stopPropagation()}
      >
        {/* Header */}
        <div className="modal-header">
          <div className="flex-1 pr-4">
            <h2 className="text-lg font-medium text-gray-900">{report.title}</h2>
            <div className="flex items-center gap-4 mt-2 text-sm text-gray-500">
              <span className="flex items-center gap-1.5">
                <Calendar className="w-3.5 h-3.5" />
                {formatDate(report.created_at)}
              </span>
              <span className="flex items-center gap-1.5">
                <FileText className="w-3.5 h-3.5" />
                {eventCount} 个事件
              </span>
              {criticalCount > 0 && (
                <span className="text-red-600 font-medium">{criticalCount} 严重</span>
              )}
              {highCount > 0 && (
                <span className="text-orange-600 font-medium">{highCount} 高危</span>
              )}
            </div>
          </div>
          <button
            onClick={onClose}
            className="p-1.5 rounded text-gray-400 hover:text-gray-600 hover:bg-gray-100 transition-colors"
          >
            <X className="w-5 h-5" />
          </button>
        </div>

        {/* Content */}
        <div className="modal-body flex-1 overflow-y-auto">
          {/* 一句话风险概括 */}
          {report.summary && (
            <div className="mb-4 p-3 rounded-lg bg-amber-50 border border-amber-200 flex items-start gap-2">
              <AlertTriangle className="w-4 h-4 text-amber-600 mt-0.5 flex-shrink-0" />
              <p className="text-sm text-amber-800 font-medium">{report.summary}</p>
            </div>
          )}
          {payload
            ? <ReportViewer data={payload.risk_data} logs={payload.agent_logs} />
            : (
              <div className="prose max-w-none">
                <ReactMarkdown remarkPlugins={[remarkGfm]}>{normalizeMarkdown(displayMarkdown)}</ReactMarkdown>
              </div>
            )
          }
        </div>

        {/* Footer */}
        <div className="modal-footer">
          <button onClick={handleCopy} className="btn-default">
            {copied ? (
              <>
                <Check className="w-4 h-4 text-success-500" />
                已复制
              </>
            ) : (
              <>
                <Copy className="w-4 h-4" />
                复制内容
              </>
            )}
          </button>
          <div className="flex-1" />
          <div className="flex items-center gap-2">
            <button onClick={() => handleDownload('md')} className="btn-default">
              <Download className="w-4 h-4" />
              Markdown
            </button>
            <button onClick={() => handleDownload('html')} className="btn-default">
              <Download className="w-4 h-4" />
              HTML
            </button>
            <button onClick={() => handleDownload('json')} className="btn-default">
              <Download className="w-4 h-4" />
              JSON
            </button>
          </div>
        </div>
      </div>
    </div>
  )
}
