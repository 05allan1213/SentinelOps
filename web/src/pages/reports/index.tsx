import { useState, useEffect, useMemo } from 'react'
import {
  Search,
  FileText,
  Download,
  Trash2,
  RefreshCw,
  Loader2,
  CheckCircle2,
  Clock,
  AlertOctagon,
  Inbox,
} from 'lucide-react'
import { cn, formatDate } from '@/utils'
import Pagination from '@/components/common/Pagination'
import StatCard from '@/components/common/StatCard'
import ConfirmDialog from '@/components/common/ConfirmDialog'
import ReportDetailModal from './components/ReportDetailModal'
import { reportService } from '@/services/report'
import type { Report } from '@/types'
import toast from 'react-hot-toast'
import CustomSelect, { type SelectOption } from '@/components/common/CustomSelect'

const reportTypeConfig: Record<string, { label: string; class: string }> = {
  vuln_alert: { label: '漏洞告警', class: 'tag-danger' },
  weekly:     { label: '周报',     class: 'tag-success' },
  custom:     { label: '分析报告', class: 'tag-default' },
}

const statusConfig: Record<string, { label: string; class: string }> = {
  pending:    { label: '待生成', class: 'tag-default' },
  generating: { label: '生成中', class: 'tag-warning' },
  completed:  { label: '已完成', class: 'tag-success' },
  failed:     { label: '失败',   class: 'tag-danger' },
}

export default function Reports() {
  const [searchQuery, setSearchQuery] = useState('')
  const [typeFilter, setTypeFilter] = useState('all')
  const [selectedReport, setSelectedReport] = useState<Report | null>(null)
  const [confirmDelete, setConfirmDelete] = useState<{ open: boolean; id?: string; isBatch?: boolean }>({ open: false })
  const [selected, setSelected] = useState<Set<string>>(new Set())

  const [reports, setReports] = useState<Report[]>([])
  const [loading, setLoading] = useState(true)
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(10)

  const fetchReports = async () => {
    try {
      setLoading(true)
      const res = await reportService.list(page, pageSize, typeFilter)
      setReports(res.list || [])
      setTotal(res.total || 0)
    } catch (error) {
      console.error('[Reports] 获取报告列表失败:', error)
      setReports([])
      setTotal(0)
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => { fetchReports() }, [page, pageSize, typeFilter])

  // 有生成中的报告时每 5s 自动刷新
  useEffect(() => {
    const hasGenerating = reports.some(r => r.status === 'generating')
    if (!hasGenerating) return
    const interval = setInterval(() => { fetchReports() }, 5000)
    return () => clearInterval(interval)
  }, [reports])

  const handleDelete = async (id: string) => {
    try {
      await reportService.delete(id)
      toast.success('已删除报告')
      setSelected(prev => { const n = new Set(prev); n.delete(id); return n })
      fetchReports()
    } catch {
      toast.error('删除失败')
    }
  }

  const handleBatchDelete = async () => {
    const ids = [...selected]
    try {
      await reportService.batchDelete(ids)
      toast.success(`已删除 ${ids.length} 份报告`)
      setSelected(new Set())
      fetchReports()
    } catch {
      toast.error('批量删除失败')
    }
  }

  const handleDownload = (report: Report) => {
    const content = report.content || `# ${report.title}\n\n（报告内容为空）`
    const blob = new Blob([content], { type: 'text/markdown' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `${report.title}.md`
    document.body.appendChild(a)
    a.click()
    document.body.removeChild(a)
    URL.revokeObjectURL(url)
  }

  const handleBatchDownload = () => {
    filteredReports
      .filter(r => selected.has(r.id))
      .forEach(r => handleDownload(r))
  }

  const toggleSelect = (id: string) => {
    setSelected(prev => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  const toggleSelectAll = () => {
    const pageIds = filteredReports.map(r => r.id)
    const allSelected = pageIds.length > 0 && pageIds.every(id => selected.has(id))
    setSelected(prev => {
      const next = new Set(prev)
      if (allSelected) pageIds.forEach(id => next.delete(id))
      else pageIds.forEach(id => next.add(id))
      return next
    })
  }

  const filteredReports = useMemo(
    () => reports.filter(r => r.title.toLowerCase().includes(searchQuery.toLowerCase())),
    [reports, searchQuery]
  )

  const pageAllSelected = filteredReports.length > 0 && filteredReports.every(r => selected.has(r.id))
  const pagePartialSelected = !pageAllSelected && filteredReports.some(r => selected.has(r.id))

  const totalPages = Math.ceil(total / pageSize)

  const completedCount = reports.filter(r => r.status === 'completed').length
  const generatingCount = reports.filter(r => r.status === 'generating').length
  const failedCount = reports.filter(r => r.status === 'failed').length

  return (
    <div className="flex flex-col gap-5 h-full">

      {/* 页面标题栏 */}
      <div className="flex items-center justify-between flex-shrink-0">
        <div>
          <h1 className="text-2xl font-semibold text-gray-900 tracking-tight">分析报告</h1>
          <p className="text-sm text-gray-500 mt-1">共 {total} 份报告</p>
        </div>
        <div className="flex items-center gap-2">
          <div className="relative">
            <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-gray-400" />
            <input
              type="text"
              placeholder="搜索报告标题..."
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              className="input pl-9 w-52"
            />
          </div>
          <CustomSelect
            value={typeFilter}
            onChange={v => { setTypeFilter(v); setPage(1) }}
            className="w-32"
            options={[
              { value: 'all', label: '全部类型' },
              { value: 'vuln_alert', label: '漏洞告警' },
              { value: 'weekly', label: '周报' },
              { value: 'custom', label: '分析报告' },
            ] satisfies SelectOption[]}
          />
          <button onClick={fetchReports} disabled={loading} className="btn-default">
            <RefreshCw className={cn('w-4 h-4', loading && 'animate-spin')} />
            刷新
          </button>
        </div>
      </div>

      {/* 统计卡片 */}
      <div className="grid grid-cols-2 sm:grid-cols-4 gap-4 flex-shrink-0">
        <StatCard label="报告总数" value={total} Icon={FileText} tone="gray" />
        <StatCard label="已完成" value={completedCount} Icon={CheckCircle2} tone="emerald" />
        <StatCard label="生成中" value={generatingCount} Icon={Clock} tone="blue"
          sub={generatingCount > 0 ? '每 5s 自动刷新' : undefined}
        />
        <StatCard label="失败" value={failedCount} Icon={AlertOctagon} tone={failedCount > 0 ? 'red' : 'gray'} />
      </div>

      {/* 批量操作栏 */}
      {selected.size > 0 && (
        <div className="flex items-center gap-3 border border-slate-200 bg-slate-50 px-4 py-2 rounded-lg text-sm flex-shrink-0">
          <span className="text-slate-700 font-medium">已选 {selected.size} 份</span>
          <div className="flex gap-2 ml-2">
            <button
              onClick={handleBatchDownload}
              className="px-3 py-1.5 rounded-lg bg-blue-100 text-blue-700 hover:bg-blue-200 text-xs font-medium transition-colors"
            >
              批量下载
            </button>
            <button
              onClick={() => setConfirmDelete({ open: true, isBatch: true })}
              className="px-3 py-1.5 rounded-lg bg-red-100 text-red-700 hover:bg-red-200 text-xs font-medium transition-colors"
            >
              批量删除
            </button>
          </div>
          <button
            onClick={() => setSelected(new Set())}
            className="ml-auto text-xs text-gray-500 hover:text-gray-700 px-2 py-1 rounded hover:bg-gray-100 transition-colors"
          >
            取消选择
          </button>
        </div>
      )}

      {/* 报告表格卡片 */}
      <div className="card flex flex-col flex-1 min-h-0">
        {loading ? (
          <div className="flex items-center justify-center py-12">
            <Loader2 className="w-8 h-8 animate-spin text-gray-400" />
          </div>
        ) : (
          <>
          <div className="overflow-x-auto flex-1 min-h-0">
            <table className="table w-full" style={{ tableLayout: 'fixed', minWidth: '720px' }}>
              <colgroup>
                <col style={{ width: '44px' }} />
                <col style={{ width: '36%' }} />
                <col style={{ width: '11%' }} />
                <col style={{ width: '11%' }} />
                <col style={{ width: '17%' }} />
                <col style={{ width: '100px' }} />
              </colgroup>
              <thead>
                <tr>
                  <th className="pl-4 w-11">
                    <input
                      type="checkbox"
                      className="rounded border-gray-300"
                      checked={pageAllSelected}
                      ref={el => { if (el) el.indeterminate = pagePartialSelected }}
                      onChange={toggleSelectAll}
                    />
                  </th>
                  <th className="text-left px-3">报告标题</th>
                  <th className="text-left px-3">类型</th>
                  <th className="text-left px-3">状态</th>
                  <th className="text-left px-3">创建时间</th>
                  <th className="text-left pl-2 pr-4">操作</th>
                </tr>
              </thead>
              <tbody>
                {filteredReports.length === 0 ? (
                  <tr>
                    <td colSpan={6}>
                      <div className="py-20 flex flex-col items-center text-gray-400">
                        <Inbox className="w-16 h-16 mb-4 text-gray-200" />
                        <p className="text-base font-medium text-gray-500 mb-1">暂无报告</p>
                        <p className="text-sm text-gray-400">
                          前往 <strong className="text-gray-600">Agent 事件分析</strong> 页面进行分析后，点击「保存到报告库」即可
                        </p>
                      </div>
                    </td>
                  </tr>
                ) : filteredReports.map((report) => {
                    const typeConf = reportTypeConfig[report.type] || reportTypeConfig.custom
                    const stConf = statusConfig[report.status] || statusConfig.pending
                    const isSelected = selected.has(report.id)
                    return (
                      <tr
                        key={report.id}
                        className={cn('cursor-pointer group', isSelected && 'bg-blue-50/60')}
                        onClick={() => setSelectedReport(report)}
                      >
                        <td className="pl-4 py-3.5" onClick={e => { e.stopPropagation(); toggleSelect(report.id) }}>
                          <input
                            type="checkbox"
                            className="rounded border-gray-300"
                            checked={isSelected}
                            onChange={() => toggleSelect(report.id)}
                          />
                        </td>
                        <td className="py-3.5 px-3 min-w-0">
                          <div className="min-w-0">
                            <p className="text-sm font-medium text-gray-900 truncate group-hover:text-indigo-600 transition-colors">
                              {report.title}
                            </p>
                            {report.summary && (
                              <p className="text-xs text-gray-500 truncate mt-0.5">{report.summary}</p>
                            )}
                          </div>
                        </td>
                        <td className="py-3.5 px-3 whitespace-nowrap">
                          <span className={cn('tag', typeConf.class)}>{typeConf.label}</span>
                        </td>
                        <td className="py-3.5 px-3 whitespace-nowrap">
                          <span className={cn('tag inline-flex items-center gap-1', stConf.class)}>
                            {report.status === 'generating' && <Loader2 className="w-3 h-3 animate-spin" />}
                            {stConf.label}
                          </span>
                        </td>
                        <td className="py-3.5 px-3 text-gray-500 whitespace-nowrap text-xs">
                          {formatDate(report.created_at, 'YYYY-MM-DD HH:mm')}
                        </td>
                        <td className="py-3.5 pl-2 pr-4" onClick={(e) => e.stopPropagation()}>
                          <div className="flex items-center gap-1.5">
                            <button
                              onClick={() => handleDownload(report)}
                              className="inline-flex items-center justify-center w-7 h-7 rounded-md border border-gray-200 bg-gray-50 text-gray-600 hover:bg-gray-100 transition-all"
                              title="下载 Markdown"
                            >
                              <Download className="w-3.5 h-3.5" />
                            </button>
                            <button
                              onClick={() => setConfirmDelete({ open: true, id: report.id })}
                              className="inline-flex items-center justify-center w-7 h-7 rounded-md border border-red-200 bg-red-50 text-red-600 hover:bg-red-100 transition-all"
                              title="删除"
                            >
                              <Trash2 className="w-3.5 h-3.5" />
                            </button>
                          </div>
                        </td>
                      </tr>
                    )
                  })}
                </tbody>
              </table>
          </div>

          {total > 0 && (
            <Pagination
              page={page}
              totalPages={totalPages}
              total={total}
              pageSize={pageSize}
              onChange={setPage}
              onPageSizeChange={size => { setPageSize(size); setPage(1) }}
            />
          )}
        </>
        )}
      </div>

      {/* 报告详情弹窗 */}
      <ReportDetailModal report={selectedReport} onClose={() => setSelectedReport(null)} />

      {/* 删除确认弹窗 */}
      <ConfirmDialog
        open={confirmDelete.open}
        title={confirmDelete.isBatch ? '批量删除报告' : '删除报告'}
        description={
          confirmDelete.isBatch
            ? `确定要删除已选的 ${selected.size} 份报告吗？删除后数据无法恢复。`
            : '确定要删除这份报告吗？删除后数据无法恢复。'
        }
        onConfirm={() => {
          if (confirmDelete.isBatch) handleBatchDelete()
          else if (confirmDelete.id) handleDelete(confirmDelete.id)
        }}
        onClose={() => setConfirmDelete({ open: false })}
      />
    </div>
  )
}
