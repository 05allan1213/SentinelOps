import { useState, useEffect, useCallback, useMemo, useRef } from 'react'
import { useNavigate, useLocation } from 'react-router-dom'
import {
  RefreshCw, CheckCircle2, XCircle, Clock,
  TrendingUp, ExternalLink,
  ThumbsUp, ThumbsDown, FlaskConical, Loader2,
  Activity, Filter, BookOpen, Star, Trash2, X,
} from 'lucide-react'
import ReactECharts from 'echarts-for-react'
import StatCard from '@/components/common/StatCard'
import Pagination from '@/components/common/Pagination'
import ConfirmDialog from '@/components/common/ConfirmDialog'
import { ragevalService, type DashboardMetrics, type TraceItem, type FeedbackStats, type TraceDetail } from '@/services/rageval'
import { cn } from '@/utils'
import toast from 'react-hot-toast'
import TraceDetailModal from './components/TraceDetailModal'
import CustomSelect, { type SelectOption } from '@/components/common/CustomSelect'

type Window = '24h' | '7d' | '30d'

function fmtMs(ms: number) {
  if (ms <= 0) return '—'
  if (ms < 1000) return `${ms}ms`
  return `${(ms / 1000).toFixed(1)}s`
}

function fmtPct(v: number) {
  return `${(v * 100).toFixed(1)}%`
}

const statusConfig: Record<string, { label: string; class: string; icon: typeof CheckCircle2 }> = {
  success: { label: '成功', class: 'bg-emerald-50 text-emerald-700 border-emerald-200', icon: CheckCircle2 },
  error:   { label: '失败', class: 'bg-red-50 text-red-700 border-red-200', icon: XCircle },
  running: { label: '运行中', class: 'bg-amber-50 text-amber-700 border-amber-200', icon: Clock },
}

function statusTone(status?: string): 'emerald' | 'amber' | 'red' | 'gray' {
  if (status === 'good') return 'emerald'
  if (status === 'warning') return 'amber'
  if (status === 'bad') return 'red'
  return 'gray'
}

// ── ECharts 趋势配置 ──────────────────────────────────────────────────────────

function buildChartOption(metrics: DashboardMetrics) {
  const labels = metrics.trends.map(t => t.timestamp)
  const latencyValues = metrics.trends.map(t => t.avg_latency_ms)
  const maxLatency = Math.max(...latencyValues, 1)
  // 右轴上限取最大值的 1.3 倍，让延迟线有足够空间展示
  const latencyMax = Math.ceil(maxLatency * 1.3 / 1000) * 1000

  return {
    backgroundColor: 'transparent',
    tooltip: {
      trigger: 'axis',
      axisPointer: { type: 'line', lineStyle: { color: '#e2e8f0', type: 'dashed' } },
      backgroundColor: '#1e293b',
      borderColor: '#334155',
      borderWidth: 1,
      textStyle: { color: '#f1f5f9', fontSize: 12 },
      extraCssText: 'box-shadow: 0 4px 16px rgba(0,0,0,0.3); border-radius: 8px;',
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      formatter: (params: any[]) => {
        if (!params.length) return ''
        const header = `<div style="font-weight:600;margin-bottom:6px;color:#94a3b8">${params[0].name}</div>`
        const rows = params.map((p: { marker: string; seriesName: string; value: number; seriesIndex: number }) => {
          const val = p.seriesIndex === 2 ? fmtMs(p.value) : fmtPct(p.value)
          return `<div style="display:flex;align-items:center;gap:8px;margin-bottom:2px">${p.marker}<span style="color:#cbd5e1">${p.seriesName}</span><strong>${val}</strong></div>`
        })
        return header + rows.join('')
      },
    },
    legend: {
      data: ['成功率', '平均延迟'],
      bottom: 0,
      textStyle: { color: '#94a3b8', fontSize: 11 },
      icon: 'circle',
      itemWidth: 8,
      itemHeight: 8,
    },
    grid: { left: 12, right: 20, bottom: 36, top: 12, containLabel: true },
    xAxis: {
      type: 'category',
      boundaryGap: false,
      data: labels,
      axisLine: { show: false },
      axisTick: { show: false },
      axisLabel: { color: '#94a3b8', fontSize: 11 },
      splitLine: { show: false },
    },
    yAxis: [
      {
        type: 'value',
        min: 0,
        max: 1,
        axisLine: { show: false },
        axisTick: { show: false },
        axisLabel: { color: '#94a3b8', fontSize: 11, formatter: (v: number) => `${Math.round(v * 100)}%` },
        splitLine: { lineStyle: { color: '#f1f5f9', type: 'dashed' } },
      },
      {
        type: 'value',
        min: 0,
        max: latencyMax,
        axisLine: { show: false },
        axisTick: { show: false },
        axisLabel: { color: '#94a3b8', fontSize: 11, formatter: (v: number) => fmtMs(v) },
        splitLine: { show: false },
      },
    ],
    series: [
      {
        name: '成功率',
        type: 'line',
        yAxisIndex: 0,
        data: metrics.trends.map(t => t.success_rate),
        smooth: 0.3,
        symbol: 'none',
        lineStyle: { color: '#6366F1', width: 2.5 },
        areaStyle: {
          color: {
            type: 'linear', x: 0, y: 0, x2: 0, y2: 1,
            colorStops: [
              { offset: 0, color: 'rgba(99,102,241,0.15)' },
              { offset: 1, color: 'rgba(99,102,241,0.01)' },
            ],
          },
        },
      },
      {
        name: '平均延迟',
        type: 'line',
        yAxisIndex: 1,
        data: latencyValues,
        smooth: 0.3,
        symbol: 'none',
        lineStyle: { color: '#10B981', width: 2 },
        areaStyle: {
          color: {
            type: 'linear', x: 0, y: 0, x2: 0, y2: 1,
            colorStops: [
              { offset: 0, color: 'rgba(16,185,129,0.12)' },
              { offset: 1, color: 'rgba(16,185,129,0.01)' },
            ],
          },
        },
      },
    ],
    animation: true,
    animationDuration: 500,
  }
}

// ── 主页面 ───────────────────────────────────────────────────────────────────

export default function RagEvalDashboard() {
  const navigate = useNavigate()
  const location = useLocation()
  const [window_, setWindow] = useState<Window>('24h')
  const [loading, setLoading] = useState(false)
  const [metrics, setMetrics] = useState<DashboardMetrics | null>(null)
  const [traces, setTraces] = useState<TraceItem[]>([])
  const [tracesTotal, setTracesTotal] = useState(0)
  const [tracesPage, setTracesPage] = useState(1)
  const [tracesPageSize, setTracesPageSize] = useState(5)
  const [feedbackStats, setFeedbackStats] = useState<FeedbackStats | null>(null)
  const [filterStatus, setFilterStatus] = useState('')
  // P0: 详情 modal
  const [detailTrace, setDetailTrace] = useState<TraceDetail | null>(null)
  const [detailLoading, setDetailLoading] = useState(false)
  const [deletingId, setDeletingId] = useState<string | null>(null)
  const [confirmDelete, setConfirmDelete] = useState<{ open: boolean; id?: string; isBatch?: boolean }>({ open: false })
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const pollingRef = useRef<ReturnType<typeof setInterval> | null>(null)

  const refresh = useCallback(async () => {
    setLoading(true)
    try {
      const [m, tRes, f] = await Promise.all([
        ragevalService.getDashboard(window_),
        ragevalService.listTraces({
          page: tracesPage,
          pageSize: tracesPageSize,
          status: filterStatus,
        }),
        ragevalService.getFeedbackStats(),
      ])
      setMetrics(m)
      setTraces(tRes.list)
      setTracesTotal(tRes.total)
      setFeedbackStats(f)
    } catch {
      // 静默失败，避免后端未启动时弹窗
    } finally {
      setLoading(false)
    }
  }, [window_, tracesPage, tracesPageSize, filterStatus])

  useEffect(() => { refresh() }, [refresh])

  // 路由变化监听：切换到此页面时自动刷新（仅当路径是 /rag-eval 时）
  useEffect(() => {
    if (location.pathname === '/rag-eval') {
      const timer = setTimeout(() => refresh(), 100)
      return () => clearTimeout(timer)
    }
  }, [location.pathname, refresh])

  // 页面可见性监听：切换回页面时自动刷新
  useEffect(() => {
    const handleVisibilityChange = () => {
      if (document.visibilityState === 'visible') {
        refresh()
      }
    }
    document.addEventListener('visibilitychange', handleVisibilityChange)
    return () => document.removeEventListener('visibilitychange', handleVisibilityChange)
  }, [refresh])

  // 自动轮询：有 running 状态的链路时，每 5s 刷新一次
  useEffect(() => {
    const hasRunning = traces.some(t => t.status === 'running')
    if (pollingRef.current) {
      clearInterval(pollingRef.current)
      pollingRef.current = null
    }
    if (hasRunning) {
      pollingRef.current = setInterval(() => { refresh() }, 5000)
    }
    return () => {
      if (pollingRef.current) clearInterval(pollingRef.current)
    }
  }, [traces, refresh])

  const chartOption = useMemo(
    () => (metrics ? buildChartOption(metrics) : null),
    [metrics],
  )

  const openDetail = async (traceId: string) => {
    setDetailLoading(true)
    setDetailTrace(null)
    try {
      const d = await ragevalService.getTraceDetail(traceId)
      setDetailTrace(d)
    } catch {
      // 静默失败
    } finally {
      setDetailLoading(false)
    }
  }

  const deleteTrace = async (traceId: string) => {
    setDeletingId(traceId)
    try {
      await ragevalService.deleteTrace(traceId)
      toast.success('已删除')
      setSelected(prev => { const s = new Set(prev); s.delete(traceId); return s })
      refresh()
    } catch {
      toast.error('删除失败')
    } finally {
      setDeletingId(null)
    }
  }

  const deleteBatch = async () => {
    const ids = Array.from(selected)
    setDeletingId('batch')
    try {
      await Promise.all(ids.map(id => ragevalService.deleteTrace(id)))
      toast.success(`已删除 ${ids.length} 条`)
      setSelected(new Set())
      refresh()
    } catch {
      toast.error('批量删除失败')
    } finally {
      setDeletingId(null)
    }
  }

  const toggleSelect = (id: string) => {
    setSelected(prev => {
      const s = new Set(prev)
      s.has(id) ? s.delete(id) : s.add(id)
      return s
    })
  }

  const toggleSelectAll = () => {
    if (traces.every(t => selected.has(t.trace_id))) {
      setSelected(new Set())
    } else {
      setSelected(new Set(traces.map(t => t.trace_id)))
    }
  }

  const pageAllSelected = traces.length > 0 && traces.every(t => selected.has(t.trace_id))
  const pagePartialSelected = traces.some(t => selected.has(t.trace_id)) && !pageAllSelected

  return (
    <div className="flex flex-col gap-5">

      {/* ── 页面标题栏 ── */}
      <div className="flex items-center justify-between flex-shrink-0">
        <div>
          <h1 className="text-2xl font-semibold text-gray-900 tracking-tight">RAG 检索质量评估</h1>
          <p className="text-sm text-gray-500 mt-1">监控检索成功率、链路延迟与用户满意度</p>
        </div>
        <div className="flex items-center gap-3">
          {/* 时间窗口切换 */}
          <div className="inline-flex rounded-lg bg-white p-1 shadow-sm border border-gray-200">
            {(['24h', '7d', '30d'] as Window[]).map(w => (
              <button
                key={w}
                onClick={() => setWindow(w)}
                className={cn(
                  'rounded-md px-3 py-1.5 text-sm font-medium transition-all',
                  window_ === w ? 'bg-gray-900 text-white' : 'text-gray-500 hover:text-gray-700',
                )}
              >
                {w}
              </button>
            ))}
          </div>
          <button onClick={refresh} disabled={loading} className="btn-default">
            <RefreshCw className={cn('w-4 h-4', loading && 'animate-spin')} />
            刷新
          </button>
        </div>
      </div>

      {/* ── 区块1：KPI 卡片（3个主指标一行） ── */}
      <div className="grid grid-cols-2 sm:grid-cols-3 gap-4 flex-shrink-0">
        {loading && !metrics ? (
          Array.from({ length: 3 }).map((_, i) => (
            <div key={i} className="rounded-xl border border-gray-100 bg-gray-50 p-4 h-24 animate-pulse" />
          ))
        ) : (
          <>
            <StatCard
              label="成功率"
              value={metrics ? fmtPct(metrics.success_rate) : '—'}
              Icon={CheckCircle2}
              tone={statusTone(metrics?.success_rate_status)}
              sub={`共 ${metrics?.total_runs ?? 0} 次请求`}
            />
            <StatCard
              label="平均延迟"
              value={metrics ? fmtMs(metrics.avg_latency_ms) : '—'}
              Icon={Clock}
              tone={statusTone(metrics?.latency_status)}
            />
            <StatCard
              label="P95 延迟"
              value={metrics ? fmtMs(metrics.p95_latency_ms) : '—'}
              Icon={TrendingUp}
              tone={statusTone(metrics?.latency_status)}
            />
          </>
        )}
      </div>

      {/* ── 区块2：趋势图 + 检索质量小指标 + 用户满意度 ── */}
      <div className="grid grid-cols-1 lg:grid-cols-3 gap-5">
        {/* 趋势图 */}
        <div className="lg:col-span-2 card p-5 flex flex-col">
          <div className="mb-4 flex items-center justify-between">
            <div>
              <p className="text-sm font-semibold text-slate-700">质量趋势</p>
              <p className="mt-0.5 text-xs text-slate-400">成功率 · 无文档率 · 平均延迟</p>
            </div>
          </div>
          <div className="flex-1" style={{ minHeight: 220 }}>
            {loading && !metrics ? (
              <div className="flex h-full items-center justify-center">
                <Loader2 className="w-5 h-5 animate-spin text-gray-300" />
              </div>
            ) : chartOption ? (
              <ReactECharts option={chartOption} style={{ height: '100%', minHeight: 220 }} opts={{ renderer: 'svg' }} />
            ) : (
              <div className="flex h-full items-center justify-center text-sm text-gray-400">
                暂无趋势数据
              </div>
            )}
          </div>
        </div>

        {/* 右侧：检索质量小指标 + 用户满意度 */}
        <div className="flex flex-col gap-4">
          {/* 检索质量小指标 */}
          <div className="card p-4 grid grid-cols-2 gap-3">
            <div className="flex flex-col gap-1">
              <div className="flex items-center gap-1.5">
                <BookOpen className="w-3.5 h-3.5 text-indigo-400" />
                <span className="text-xs text-slate-500">平均召回数</span>
              </div>
              <span className="text-2xl font-bold text-slate-800 tabular-nums">
                {metrics ? metrics.avg_retrieved_docs.toFixed(1) : '—'}
              </span>
              <span className="text-[11px] text-slate-400">每次检索平均召回文档数</span>
            </div>
            <div className="flex flex-col gap-1">
              <div className="flex items-center gap-1.5">
                <Star className="w-3.5 h-3.5 text-indigo-400" />
                <span className="text-xs text-slate-500">平均相似度</span>
              </div>
              <span className="text-2xl font-bold text-slate-800 tabular-nums">
                {metrics ? fmtPct(metrics.avg_top_score) : '—'}
              </span>
              <span className="text-[11px] text-slate-400">最高向量相似度均值</span>
            </div>
          </div>

          {/* 用户满意度 */}
          <div className="card p-5 flex flex-col gap-4 flex-1">
          <div>
            <p className="text-sm font-semibold text-slate-700">用户满意度</p>
            <p className="mt-0.5 text-xs text-slate-400">来自 AI 回答气泡的点赞/踩统计</p>
          </div>

          {feedbackStats ? (
            <>
              {/* 满意度柱 */}
              <div className="space-y-3">
                <FeedbackBar
                  icon={<ThumbsUp className="w-3.5 h-3.5" />}
                  label="点赞"
                  rate={feedbackStats.like_rate}
                  colorClass="bg-emerald-500"
                  textClass="text-emerald-600"
                />
                <FeedbackBar
                  icon={<ThumbsDown className="w-3.5 h-3.5" />}
                  label="点踩"
                  rate={feedbackStats.dislike_rate}
                  colorClass="bg-red-400"
                  textClass="text-red-500"
                />
                <p className="text-xs text-gray-400 pt-1 border-t border-gray-100">
                  共 <strong className="text-gray-600">{feedbackStats.total}</strong> 条反馈
                </p>
              </div>

              {/* 最近反馈 */}
              {feedbackStats.recent && feedbackStats.recent.length > 0 && (
                <div className="flex-1 space-y-1">
                  <p className="text-xs font-medium text-gray-500 mb-2">最近反馈</p>
                  {feedbackStats.recent.map((r, i) => (
                    <div
                      key={i}
                      className={cn(
                        'flex items-center gap-2 text-xs rounded-lg px-2.5 py-1.5',
                        r.vote === 1 ? 'bg-emerald-50' : 'bg-red-50',
                      )}
                    >
                      {r.vote === 1
                        ? <ThumbsUp className="w-3 h-3 text-emerald-500 flex-shrink-0" />
                        : <ThumbsDown className="w-3 h-3 text-red-400 flex-shrink-0" />
                      }
                      <span className={cn('truncate flex-1', r.vote === 1 ? 'text-emerald-700' : 'text-red-600')}>
                        {r.reason || (r.vote === 1 ? '有帮助' : '没帮助')}
                      </span>
                      <span className="text-gray-400 flex-shrink-0">{r.created_at.slice(5, 16)}</span>
                    </div>
                  ))}
                </div>
              )}
            </>
          ) : (
            <div className="flex-1 flex items-center justify-center text-sm text-gray-400">
              {loading ? <Loader2 className="w-4 h-4 animate-spin" /> : '暂无反馈数据'}
            </div>
          )}
        </div>

        </div>
      </div>

      {/* ── 区块3：最近 RAG 链路 ── */}
      <div className="card flex flex-col">
        <div className="flex items-center justify-between px-5 py-4 border-b border-gray-100 flex-shrink-0">
          <div className="flex items-center gap-2">
            <Activity className="w-4 h-4 text-indigo-500" />
            <h2 className="text-sm font-semibold text-gray-900">最近 RAG 链路</h2>
            {tracesTotal > 0 && (
              <span className="text-xs text-gray-400 bg-gray-100 rounded-full px-2 py-0.5">
                {tracesTotal} 条
              </span>
            )}
          </div>
          <button
            onClick={() => navigate('/knowledge')}
            className="flex items-center gap-1.5 text-xs font-medium text-indigo-600 hover:text-indigo-700 transition-colors"
          >
            <FlaskConical className="w-3.5 h-3.5" />
            去知识库检索测试
          </button>
        </div>

        {/* P2: 过滤栏 */}
        <div className="flex items-center gap-2.5 px-5 py-3 border-b border-gray-100 bg-gray-50/60">
          <div className="flex items-center gap-1.5 text-xs text-gray-400 font-medium mr-1">
            <Filter className="w-3.5 h-3.5" />
            筛选
          </div>
          <CustomSelect
            value={filterStatus}
            onChange={v => { setFilterStatus(v); setTracesPage(1) }}
            className={cn(filterStatus && '!border-indigo-400 !text-indigo-700 bg-indigo-50/60')}
            options={[
              { value: '', label: '全部状态' },
              { value: 'success', label: '成功' },
              { value: 'error', label: '失败' },
            ] satisfies SelectOption[]}
          />
          {filterStatus && (
            <button
              onClick={() => { setFilterStatus(''); setTracesPage(1) }}
              className="ml-1 inline-flex items-center gap-1 text-xs text-indigo-500 hover:text-indigo-700 bg-indigo-50 hover:bg-indigo-100 px-2 py-1 rounded-md transition-colors font-medium"
            >
              <X className="w-3 h-3" />
              清除
            </button>
          )}
        </div>

        {/* 批量操作栏 */}
        {selected.size > 0 && (
          <div className="flex items-center gap-3 border-b border-slate-200 bg-slate-50 px-5 py-2 text-sm flex-shrink-0">
            <span className="text-slate-700 font-medium">已选 {selected.size} 条</span>
            <button
              onClick={() => setConfirmDelete({ open: true, isBatch: true })}
              className="px-3 py-1.5 rounded-lg bg-red-100 text-red-700 hover:bg-red-200 text-xs font-medium transition-colors"
            >
              批量删除
            </button>
            <button
              onClick={() => setSelected(new Set())}
              className="ml-auto text-xs text-gray-500 hover:text-gray-700 px-2 py-1 rounded hover:bg-gray-100 transition-colors"
            >
              取消选择
            </button>
          </div>
        )}

        <div className="overflow-x-auto">
          <table className="w-full text-sm" style={{ minWidth: '840px' }}>
            <thead>
              <tr className="border-b border-gray-100">
                <th className="pl-4 pr-2 py-3 w-10">
                  <input
                    type="checkbox"
                    checked={pageAllSelected}
                    ref={el => { if (el) el.indeterminate = pagePartialSelected }}
                    onChange={toggleSelectAll}
                    className="rounded border-gray-300"
                  />
                </th>
                {['时间', '链路名称', '会话 ID', '状态', '耗时', '反馈', '操作'].map(h => (
                  <th key={h} className="px-4 py-3 text-left text-xs font-semibold text-gray-800 whitespace-nowrap">
                    {h}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody className="divide-y divide-gray-50">
              {loading && traces.length === 0 ? (
                <tr>
                  <td colSpan={8} className="px-4 py-12 text-center">
                    <Loader2 className="w-6 h-6 animate-spin mx-auto text-gray-300" />
                  </td>
                </tr>
              ) : traces.length === 0 ? (
                <tr>
                  <td colSpan={8} className="px-4 py-12 text-center text-sm text-gray-400">
                    暂无链路数据
                  </td>
                </tr>
              ) : (
                traces.map(t => {
                  const sc = statusConfig[t.status] || statusConfig.running
                  const StatusIcon = sc.icon
                  return (
                  <tr key={t.trace_id} className={cn('hover:bg-gray-50 transition-colors', selected.has(t.trace_id) && 'bg-blue-50/60')}>
                    <td className="pl-4 pr-2 py-3" onClick={e => e.stopPropagation()}>
                      <input
                        type="checkbox"
                        checked={selected.has(t.trace_id)}
                        onChange={() => toggleSelect(t.trace_id)}
                        className="rounded border-gray-300"
                      />
                    </td>
                    <td className="px-4 py-3 text-xs text-gray-500 whitespace-nowrap">{t.start_time}</td>
                    <td className="px-4 py-3 max-w-[160px]">
                      <p className="text-xs text-gray-700 truncate" title={t.trace_name}>{t.trace_name || '—'}</p>
                    </td>
                    <td className="px-4 py-3">
                      {t.session_id ? (
                        <code className="text-xs font-mono bg-indigo-50 text-indigo-600 px-1.5 py-0.5 rounded">
                          {t.session_id.slice(-8)}
                        </code>
                      ) : <span className="text-gray-300 text-xs">—</span>}
                    </td>
                    <td className="px-4 py-3">
                      <span className={cn('inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-xs font-medium border', sc.class)}>
                        <StatusIcon className="w-3 h-3" />
                        {sc.label}
                      </span>
                    </td>
                    <td className="px-4 py-3 text-xs text-gray-600 tabular-nums">{fmtMs(t.duration_ms)}</td>
                    <td className="px-4 py-3 text-xs">
                      {t.feedback_vote === 1
                        ? <ThumbsUp className="w-3.5 h-3.5 text-emerald-500" />
                        : t.feedback_vote === -1
                          ? <ThumbsDown className="w-3.5 h-3.5 text-red-400" />
                          : <span className="text-gray-300">—</span>
                      }
                    </td>
                    <td className="px-4 py-3" onClick={e => e.stopPropagation()}>
                      <div className="flex items-center gap-1.5">
                        <button
                          onClick={() => openDetail(t.trace_id)}
                          className="inline-flex items-center justify-center w-7 h-7 rounded-md border border-gray-200 bg-gray-50 text-gray-600 hover:bg-gray-100 transition-all"
                          title="查看链路详情"
                        >
                          <ExternalLink className="w-3.5 h-3.5" />
                        </button>
                        <button
                          onClick={() => setConfirmDelete({ open: true, id: t.trace_id })}
                          disabled={deletingId === t.trace_id}
                          className="inline-flex items-center justify-center w-7 h-7 rounded-md border border-red-200 bg-red-50 text-red-600 hover:bg-red-100 transition-all disabled:opacity-40"
                          title="删除"
                        >
                          {deletingId === t.trace_id
                            ? <Loader2 className="w-3.5 h-3.5 animate-spin" />
                            : <Trash2 className="w-3.5 h-3.5" />
                          }
                        </button>
                      </div>
                    </td>
                  </tr>
                  )
                })
              )}
            </tbody>
          </table>
        </div>

        {!loading && tracesTotal > 0 && (
          <Pagination
            page={tracesPage}
            totalPages={Math.ceil(tracesTotal / tracesPageSize)}
            total={tracesTotal}
            pageSize={tracesPageSize}
            onChange={setTracesPage}
            onPageSizeChange={size => { setTracesPageSize(size); setTracesPage(1) }}
          />
        )}
      </div>

      {/* P0: Trace 详情 Modal */}
      {(detailLoading || detailTrace) && (
        <TraceDetailModal
          detail={detailTrace}
          loading={detailLoading}
          onClose={() => setDetailTrace(null)}
        />
      )}

      {/* 删除确认 */}
      <ConfirmDialog
        open={confirmDelete.open}
        title={confirmDelete.isBatch ? `删除 ${selected.size} 条链路` : '删除链路记录'}
        description={confirmDelete.isBatch ? '批量删除后不可恢复，确认继续？' : '删除后不可恢复，确认继续？'}
        confirmLabel="删除"
        onConfirm={() => {
          if (confirmDelete.isBatch) deleteBatch()
          else if (confirmDelete.id) deleteTrace(confirmDelete.id)
        }}
        onClose={() => setConfirmDelete({ open: false })}
      />
    </div>
  )
}

// ── 满意度进度条 ─────────────────────────────────────────────────────────────

interface FeedbackBarProps {
  icon: React.ReactNode
  label: string
  rate: number
  colorClass: string
  textClass: string
}

function FeedbackBar({ icon, label, rate, colorClass, textClass }: FeedbackBarProps) {
  return (
    <div className="space-y-1.5">
      <div className="flex items-center justify-between text-xs">
        <div className="flex items-center gap-1.5 text-gray-600">
          {icon}
          <span>{label}</span>
        </div>
        <span className={cn('font-semibold tabular-nums', textClass)}>{fmtPct(rate)}</span>
      </div>
      <div className="h-2 rounded-full bg-gray-100 overflow-hidden">
        <div
          className={cn('h-full rounded-full transition-all duration-700', colorClass)}
          style={{ width: `${Math.round(rate * 100)}%` }}
        />
      </div>
    </div>
  )
}
