import { useState, useEffect } from 'react'
import {
  Search,
  RefreshCw,
  CheckCircle,
  XCircle,
  Clock,
  FileSearch,
  Loader2,
  AlertCircle,
} from 'lucide-react'
import { cn, formatDate } from '@/utils'
import { subscriptionService } from '@/services/subscription'
import type { Subscription, FetchLog } from '@/types'
import toast from 'react-hot-toast'
import CustomSelect, { type SelectOption } from '@/components/common/CustomSelect'

const sourceTypeLabels: Record<string, string> = {
  github_repo: 'GitHub',
  github: 'GitHub',
  rss: 'RSS',
}

const statusConfig: Record<string, { label: string; icon: typeof CheckCircle; class: string }> = {
  success: { label: '成功', icon: CheckCircle, class: 'text-emerald-600' },
  failed: { label: '失败', icon: XCircle, class: 'text-red-600' },
  timeout: { label: '超时', icon: Clock, class: 'text-amber-600' },
}

interface FetchLogWithSubscription extends FetchLog {
  subscription_name?: string
  source_type?: string
}

export default function Logs() {
  const [searchQuery, setSearchQuery] = useState('')
  const [statusFilter, setStatusFilter] = useState('all')
  const [expandedLog, setExpandedLog] = useState<number | null>(null)
  const [selectedSubscriptionId, setSelectedSubscriptionId] = useState<string | null>(null)

  const [subscriptions, setSubscriptions] = useState<Subscription[]>([])
  const [logs, setLogs] = useState<FetchLogWithSubscription[]>([])
  const [_loading, setLoading] = useState(true)
  const [logsLoading, setLogsLoading] = useState(false)
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [pageSize] = useState(20)

  // 获取订阅列表
  const fetchSubscriptions = async () => {
    try {
      const res = await subscriptionService.list(1, 100)
      setSubscriptions(res.list || [])
    } catch (error) {
      console.error(error)
    } finally {
      setLoading(false)
    }
  }

  // 获取日志
  const fetchLogs = async () => {
    if (!selectedSubscriptionId) {
      setLogs([])
      setTotal(0)
      return
    }

    try {
      setLogsLoading(true)
      const res = await subscriptionService.getFetchLogs(selectedSubscriptionId, page, pageSize)
      const subscription = subscriptions.find(s => s.id === selectedSubscriptionId)
      const logsWithSub = (res.list || []).map(log => ({
        ...log,
        subscription_name: subscription?.name,
        source_type: subscription?.source_type,
      }))
      setLogs(logsWithSub)
      setTotal(res.total || 0)
    } catch (error) {
      toast.error('获取日志失败')
      console.error(error)
    } finally {
      setLogsLoading(false)
    }
  }

  useEffect(() => {
    fetchSubscriptions()
  }, [])

  useEffect(() => {
    if (selectedSubscriptionId) {
      fetchLogs()
    }
  }, [selectedSubscriptionId, page])

  const filteredLogs = logs.filter((log) => {
    const matchesSearch = log.subscription_name?.toLowerCase().includes(searchQuery.toLowerCase()) ?? true
    const matchesStatus = statusFilter === 'all' || log.status === statusFilter
    return matchesSearch && matchesStatus
  })

  const successCount = logs.filter((l) => l.status === 'success').length
  const failedCount = logs.filter((l) => l.status === 'failed').length
  const totalEvents = logs.reduce((sum, l) => sum + (l.event_count || 0), 0)
  const totalPages = Math.ceil(total / pageSize)

  return (
    <div className="space-y-6">
      {/* Page Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold text-gray-900 tracking-tight">抓取日志</h1>
          <p className="text-sm text-gray-500 mt-1">
            查看订阅源的抓取执行记录
          </p>
        </div>
      </div>

      {/* Subscription Selector */}
      <div className="card card-body">
        <div className="flex items-center gap-4">
          <label className="text-sm text-gray-600">选择订阅：</label>
          <CustomSelect
            value={selectedSubscriptionId || ''}
            onChange={v => {
              setSelectedSubscriptionId(v || null)
              setPage(1)
            }}
            className="flex-1 max-w-md"
            placeholder="请选择订阅源"
            options={subscriptions.map(sub => ({
              value: String(sub.id),
              label: `${sub.name} (${sourceTypeLabels[sub.source_type] || sub.source_type})`,
            }))}
          />
          {selectedSubscriptionId && (
            <button
              onClick={fetchLogs}
              disabled={logsLoading}
              className="btn-default"
            >
              <RefreshCw className={cn('w-4 h-4', logsLoading && 'animate-spin')} />
              刷新
            </button>
          )}
        </div>
      </div>

      {!selectedSubscriptionId ? (
        <div className="card">
          <div className="empty py-16">
            <AlertCircle className="empty-icon" />
            <p className="empty-text">请先选择一个订阅源查看日志</p>
          </div>
        </div>
      ) : (
        <>
          {/* Stats */}
          <div className="grid grid-cols-4 gap-4">
            <div className="card card-body">
              <div className="flex items-center justify-between">
                <div>
                  <p className="text-sm text-gray-500">总执行次数</p>
                  <p className="text-2xl font-semibold text-gray-900 mt-1">{total}</p>
                </div>
                <div className="w-10 h-10 rounded-lg bg-gray-100 flex items-center justify-center">
                  <RefreshCw className="w-5 h-5 text-gray-500" />
                </div>
              </div>
            </div>
            <div className="card card-body">
              <div className="flex items-center justify-between">
                <div>
                  <p className="text-sm text-gray-500">成功次数</p>
                  <p className="text-2xl font-semibold text-emerald-600 mt-1">{successCount}</p>
                </div>
                <div className="w-10 h-10 rounded-lg bg-emerald-50 flex items-center justify-center">
                  <CheckCircle className="w-5 h-5 text-emerald-600" />
                </div>
              </div>
            </div>
            <div className="card card-body">
              <div className="flex items-center justify-between">
                <div>
                  <p className="text-sm text-gray-500">失败次数</p>
                  <p className="text-2xl font-semibold text-red-600 mt-1">{failedCount}</p>
                </div>
                <div className="w-10 h-10 rounded-lg bg-red-50 flex items-center justify-center">
                  <XCircle className="w-5 h-5 text-red-600" />
                </div>
              </div>
            </div>
            <div className="card card-body">
              <div className="flex items-center justify-between">
                <div>
                  <p className="text-sm text-gray-500">抓取事件数</p>
                  <p className="text-2xl font-semibold text-blue-600 mt-1">{totalEvents}</p>
                </div>
                <div className="w-10 h-10 rounded-lg bg-blue-50 flex items-center justify-center">
                  <FileSearch className="w-5 h-5 text-blue-600" />
                </div>
              </div>
            </div>
          </div>

          {/* Filters */}
          <div className="card card-body">
            <div className="flex flex-col lg:flex-row gap-4">
              {/* Search */}
              <div className="relative flex-1">
                <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-gray-500" />
                <input
                  type="text"
                  placeholder="搜索..."
                  value={searchQuery}
                  onChange={(e) => setSearchQuery(e.target.value)}
                  className="input pl-9"
                />
              </div>

              {/* Status Filter */}
              <CustomSelect
                value={statusFilter}
                onChange={v => setStatusFilter(v)}
                className="w-32"
                options={[
                  { value: 'all', label: '全部状态' },
                  { value: 'success', label: '成功' },
                  { value: 'failed', label: '失败' },
                  { value: 'timeout', label: '超时' },
                ] satisfies SelectOption[]}
              />
            </div>
          </div>

          {/* Logs Table */}
          <div className="card">
            {logsLoading ? (
              <div className="flex items-center justify-center py-12">
                <Loader2 className="w-8 h-8 animate-spin text-primary-500" />
              </div>
            ) : (
              <div className="table-container">
                <table className="table">
                  <thead>
                    <tr>
                      <th>订阅名称</th>
                      <th className="w-20">状态</th>
                      <th className="w-20">事件数</th>
                      <th className="w-20">耗时</th>
                      <th className="w-36">执行时间</th>
                    </tr>
                  </thead>
                  <tbody>
                    {filteredLogs.map((log) => {
                      const status = statusConfig[log.status] || statusConfig.success
                      const StatusIcon = status.icon

                      return (
                        <>
                          <tr
                            key={log.id}
                            className={cn(
                              'cursor-pointer',
                              log.status === 'failed' && 'bg-red-50'
                            )}
                            onClick={() => setExpandedLog(expandedLog === log.id ? null : log.id)}
                          >
                            <td>
                              <p className="font-medium text-gray-900">{log.subscription_name || '-'}</p>
                            </td>
                            <td>
                              <span className={cn('flex items-center gap-1.5 text-sm', status.class)}>
                                <StatusIcon className="w-3.5 h-3.5" />
                                {status.label}
                              </span>
                            </td>
                            <td className="text-gray-700">
                              {log.event_count > 0 ? (
                                <span className="text-emerald-600 font-medium">+{log.event_count}</span>
                              ) : (
                                <span className="text-gray-400">0</span>
                              )}
                            </td>
                            <td className="text-gray-600 text-sm">
                              {(log.duration / 1000).toFixed(1)}s
                            </td>
                            <td className="text-gray-600 text-sm">
                              {formatDate(log.created_at, 'YYYY-MM-DD HH:mm')}
                            </td>
                          </tr>
                          {expandedLog === log.id && log.error_msg && (
                            <tr key={`${log.id}-error`}>
                              <td colSpan={5} className="bg-red-50 border-t-0">
                                <div className="flex items-start gap-2 text-sm">
                                  <XCircle className="w-4 h-4 text-red-600 flex-shrink-0 mt-0.5" />
                                  <div>
                                    <p className="text-red-700 font-medium">错误信息</p>
                                    <p className="text-gray-600 mt-1 font-mono text-xs">
                                      {log.error_msg}
                                    </p>
                                  </div>
                                </div>
                              </td>
                            </tr>
                          )}
                        </>
                      )
                    })}
                  </tbody>
                </table>
              </div>
            )}

            {!logsLoading && filteredLogs.length === 0 && (
              <div className="empty">
                <FileSearch className="empty-icon" />
                <p className="empty-text">没有找到匹配的日志记录</p>
              </div>
            )}

            {/* Pagination */}
            {!logsLoading && filteredLogs.length > 0 && (
              <div className="px-4 py-3 border-t border-gray-200 flex items-center justify-between">
                <span className="text-sm text-gray-600">
                  共 {total} 条记录
                </span>
                <div className="pagination">
                  <button
                    className={cn('pagination-item', page <= 1 && 'disabled')}
                    onClick={() => setPage(p => Math.max(1, p - 1))}
                    disabled={page <= 1}
                  >
                    上一页
                  </button>
                  <button className="pagination-item active">{page}</button>
                  <button
                    className={cn('pagination-item', page >= totalPages && 'disabled')}
                    onClick={() => setPage(p => Math.min(totalPages, p + 1))}
                    disabled={page >= totalPages}
                  >
                    下一页
                  </button>
                </div>
              </div>
            )}
          </div>
        </>
      )}
    </div>
  )
}
