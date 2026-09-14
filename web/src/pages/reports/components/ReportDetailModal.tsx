import { X, Download, Copy, Check, Calendar, FileText } from 'lucide-react'
import { useState } from 'react'
import { formatDate, parseReportPayload, normalizeMarkdown } from '@/utils'
import ReportViewer from '@/components/report/ReportViewer'
import { MarkdownRenderer } from '@/components/markdown'
import Alert from '@/components/common/Alert'
import Button from '@/components/common/Button'
import { Dialog, DialogBody, DialogClose, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'

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
    <Dialog open onOpenChange={open => { if (!open) onClose() }}>
      <DialogContent className="max-w-4xl overflow-hidden p-4 sm:p-6">
        {/* 弹窗标题 */}
        <DialogHeader className="flex items-start gap-3 space-y-0">
          <div className="min-w-0 flex-1">
            <DialogTitle>{report.title}</DialogTitle>
            <DialogDescription asChild>
              <div className="mt-2 flex flex-wrap items-center gap-x-4 gap-y-2">
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
            </DialogDescription>
          </div>
          <DialogClose asChild>
            <Button variant="icon" aria-label="关闭报告"><X aria-hidden="true" className="h-5 w-5" /></Button>
          </DialogClose>
        </DialogHeader>

        {/* 报告内容 */}
        <DialogBody>
          {/* 一句话风险概括 */}
          {report.summary && (
            <Alert tone="warning" className="mb-4">{report.summary}</Alert>
          )}
          {payload
            ? <ReportViewer data={payload.risk_data} logs={payload.agent_logs} />
            : <MarkdownRenderer content={normalizeMarkdown(displayMarkdown)} variant="report" />
          }
        </DialogBody>

        {/* 底部操作区 */}
        <DialogFooter>
          <Button onClick={handleCopy}>
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
          </Button>
          <div className="flex-1" />
          <div className="flex flex-wrap items-center gap-2">
            <Button onClick={() => handleDownload('md')}>
              <Download className="w-4 h-4" />
              Markdown
            </Button>
            <Button onClick={() => handleDownload('html')}>
              <Download className="w-4 h-4" />
              HTML
            </Button>
            <Button onClick={() => handleDownload('json')}>
              <Download className="w-4 h-4" />
              JSON
            </Button>
          </div>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
