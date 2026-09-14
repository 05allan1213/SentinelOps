import { Dialog, DialogContent, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import Button from '@/components/common/Button'
import { X, Download, Copy, Loader2, Save } from 'lucide-react'
import ReportViewer from '@/components/report/ReportViewer'
import type { ReportViewerData, ReportViewerLog } from '@/components/report/ReportViewer'
import { buildMarkdown, type ReportBuildData, type ReportBuildLog } from '../reportMarkdown'

interface Props {
  visible: boolean
  onClose: () => void
  data: ReportBuildData | null
  logs: ReportBuildLog[]
  analysisText: string
  onSave?: () => void
  saving?: boolean
}

export default function ReportModal({ visible, onClose, data, logs, onSave, saving }: Props) {
  if (!visible || !data) return null

  const now = new Date()
  const dateStr = `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, '0')}-${String(now.getDate()).padStart(2, '0')}`
  const timeStr = `${String(now.getHours()).padStart(2, '0')}:${String(now.getMinutes()).padStart(2, '0')}`
  const reportId = `REPORT-${dateStr.replace(/-/g, '')}-${timeStr.replace(':', '')}`

  const handleDownload = () => {
    const md = buildMarkdown(data, logs)
    const blob = new Blob([md], { type: 'text/markdown' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `安全分析报告_${dateStr}_${timeStr.replace(':', '')}.md`
    a.click()
    URL.revokeObjectURL(url)
  }

  const handleCopy = () => {
    const md = buildMarkdown(data, logs)
    navigator.clipboard.writeText(md).catch(() => {/* 静默 */})
  }

  return (
    <Dialog open onOpenChange={open => { if (!open) onClose() }}>
      <DialogContent aria-describedby={undefined} className="max-w-4xl overflow-y-auto p-4 sm:p-6">
        {/* 头部 */}
        <DialogHeader className="flex items-start justify-between gap-3 space-y-0">
          <div>
            <DialogTitle>安全事件分析报告</DialogTitle>
            <p className="text-xs text-gray-400 mt-0.5">{reportId} · 共分析 {data.count} 个事件 · 报告有效期 30 天</p>
          </div>
          <Button variant="icon" aria-label="关闭" onClick={onClose}>
            <X className="w-5 h-5" />
          </Button>
        </DialogHeader>

        {/* 内容区 */}
        <div className="flex-1 overflow-auto px-6 py-4">
          <ReportViewer data={data as ReportViewerData} logs={logs as ReportViewerLog[]} />
        </div>

        {/* 底部操作 */}
        <div className="px-6 py-4 border-t border-gray-100 flex items-center justify-between">
          <button onClick={onClose} className="btn-default">关闭</button>
          <div className="flex items-center gap-2">
            <button onClick={handleCopy} className="btn-default">
              <Copy className="w-4 h-4" />
              复制内容
            </button>
            <button onClick={handleDownload} className="btn-default">
              <Download className="w-4 h-4" />
              下载 Markdown
            </button>
            {onSave && (
              <button onClick={onSave} disabled={saving} className="btn-primary flex items-center gap-1.5">
                {saving ? <Loader2 className="w-4 h-4 animate-spin" /> : <Save className="w-4 h-4" />}
                {saving ? '保存中...' : '保存到报告库'}
              </button>
            )}
          </div>
        </div>
      </DialogContent>
    </Dialog>
  )
}
