import { useState, useEffect, useCallback, useRef } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import {
  Activity,
  Search,
  RefreshCw,
  Loader2,
  CheckCircle2,
  XCircle,
  Clock,
  Zap,
  Trash2,
  Copy,
  Check,
  GitCommit,
  BarChart2,
  Cpu,
  Tag,
  CalendarDays,
  TrendingUp,
  Banknote,
} from 'lucide-react'
import ReactECharts from 'echarts-for-react'
import { cn } from '@/utils'
import StatCard from '@/components/common/StatCard'
import CustomSelect from '@/components/common/CustomSelect'
import Pagination from '@/components/common/Pagination'
import ConfirmDialog from '@/components/common/ConfirmDialog'
import {
  traceService,
  type TraceRun,
  type TraceStats,
  type SessionTimelineSummary,
  type CostOverview,
  type TokenTrendPoint,
} from '@/services/trace'
import toast from 'react-hot-toast'

// ── 格式化工具 ─────────────────────────────────────────────────────────────────

function formatDuration(ms: number): string {
  if (ms <= 0) return '-'
  if (ms < 1000) return `${ms}ms`
  return `${(ms / 1000).toFixed(1)}s`
}

function formatTokens(n: number): string {
  if (!n) return '0'
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(2)}M`
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}k`
  return String(n)
}

function fmtCost(cny: number): string {
  if (cny === 0) return '¥0.000'
  if (cny < 0.001) return `¥${(cny * 1000).toFixed(3)}m`
  if (cny < 1) return `¥${cny.toFixed(4)}`
  return `¥${cny.toFixed(3)}`
}

function fmtPct(v: number): string {
  return `${v >= 0 ? '+' : ''}${v.toFixed(1)}%`
}

function truncateQuery(text: string, maxLen = 60): string {
  if (!text) return '-'
  if (text.length <= maxLen) return text
  return text.slice(0, maxLen) + '…'
}

function formatTime(iso: string): string {
  if (!iso) return '-'
  try {
    return new Date(iso).toLocaleString('zh-CN', {
      month: '2-digit', day: '2-digit',
      hour: '2-digit', minute: '2-digit', second: '2-digit',
    })
  } catch { return iso }
}

const statusConfig: Record<string, { label: string; class: string }> = {
  success: { label: '成功', class: 'bg-emerald-50 text-emerald-700 border border-emerald-200' },
  error:   { label: '失败', class: 'bg-red-50 text-red-700 border border-red-200' },
  running: { label: '运行中', class: 'bg-amber-50 text-amber-700 border border-amber-200' },
}

// 时间范围选项
type CostRange = '24h' | '7d' | '30d' | '90d'
const COST_RANGES: { value: CostRange; label: string; days: number; hours?: number }[] = [
  { value: '24h', label: '今日',   days: 1,  hours: 24 },
  { value: '7d',  label: '7 天',   days: 7 },
  { value: '30d', label: '30 天',  days: 30 },
  { value: '90d', label: '90 天',  days: 90 },
]

// ── 图表日期格式化（ISO → MM-DD）───────────────────────────────────────────────
function formatChartDate(iso: string): string {
  try {
    const d = new Date(iso)
    const m = String(d.getMonth() + 1).padStart(2, '0')
    const day = String(d.getDate()).padStart(2, '0')
    return `${m}-${day}`
  } catch {
    return iso.slice(0, 10)
  }
}

// ── ECharts 配置 ───────────────────────────────────────────────────────────────

function buildDailyTrendOption(data: CostOverview['dailyTrend']) {
  const rawDates = data.map(d => d.date)
  const dates = rawDates.map(formatChartDate)
  const costs = data.map(d => +(d.costCny).toFixed(4))
  const inputs = data.map(d => d.inputTokens)
  const outputs = data.map(d => d.outputTokens)
  return {
    backgroundColor: 'transparent',
    tooltip: {
      trigger: 'axis',
      backgroundColor: 'rgba(255,255,255,0.96)',
      borderColor: '#e5e7eb',
      borderWidth: 1,
      padding: [10, 14],
      textStyle: { color: '#374151', fontSize: 12 },
      axisPointer: { type: 'cross', crossStyle: { color: '#cbd5e1', width: 1 } },
      formatter(params: any[]) {
        const idx = params[0]?.dataIndex ?? 0
        const dateStr = formatChartDate(rawDates[idx] || '')
        const lines = params.map((p: any) => {
          const val = p.seriesName === '成本(CNY)' ? `¥${p.value}` : formatTokens(p.value)
          return `<div style="display:flex;align-items:center;justify-content:space-between;gap:12px;margin:4px 0">
            <span style="display:flex;align-items:center;gap:6px">
              <span style="display:inline-block;width:10px;height:3px;border-radius:2px;background:${p.color}"></span>
              <span style="color:#6b7280">${p.seriesName}</span>
            </span>
            <span style="font-weight:600;color:#111827">${val}</span>
          </div>`
        }).join('')
        return `<div style="min-width:180px"><div style="font-weight:600;color:#111827;margin-bottom:8px;padding-bottom:6px;border-bottom:1px solid #e5e7eb">${dateStr}</div>${lines}</div>`
      },
    },
    legend: {
      bottom: 0,
      textStyle: { color: '#4b5563', fontSize: 12 },
      itemWidth: 14,
      itemHeight: 10,
      itemGap: 16,
      data: ['成本(CNY)', '输入 Token', '输出 Token'],
    },
    grid: { top: 30, left: 70, right: 70, bottom: 50 },
    xAxis: {
      type: 'category',
      data: dates,
      boundaryGap: true,
      axisLine: { lineStyle: { color: '#e5e7eb', width: 1 } },
      axisLabel: { color: '#6b7280', fontSize: 11, margin: 10, interval: 0 },
      axisTick: { show: true, lineStyle: { color: '#e5e7eb' } },
      splitLine: { show: false },
    },
    yAxis: [
      {
        type: 'value', name: 'CNY', nameTextStyle: { color: '#6b7280', fontSize: 11, padding: [0, 0, 0, 10] },
        axisLabel: { color: '#6b7280', fontSize: 11, formatter: (v: number) => `¥${v.toFixed(3)}` },
        splitLine: { lineStyle: { color: '#f3f4f6', type: 'dashed', width: 1 } },
        position: 'left',
      },
      {
        type: 'value', name: 'Tokens', nameTextStyle: { color: '#6b7280', fontSize: 11, padding: [0, 10, 0, 0] },
        axisLabel: { color: '#6b7280', fontSize: 11, formatter: (v: number) => formatTokens(v) },
        splitLine: { show: false },
        position: 'right',
      },
    ],
    series: [
      {
        name: '成本(CNY)', type: 'line', yAxisIndex: 0, data: costs,
        smooth: true, symbol: 'circle', symbolSize: 6,
        lineStyle: { width: 2.5, color: '#10b981', type: 'solid' },
        itemStyle: { color: '#10b981', borderWidth: 2, borderColor: '#fff' },
        areaStyle: {
          color: {
            type: 'linear', x: 0, y: 0, x2: 0, y2: 1,
            colorStops: [
              { offset: 0, color: 'rgba(16,185,129,0.25)' },
              { offset: 1, color: 'rgba(16,185,129,0.02)' }
            ]
          }
        },
        emphasis: { focus: 'series', itemStyle: { borderWidth: 3, shadowBlur: 8, shadowColor: 'rgba(16,185,129,0.4)' } },
      },
      {
        name: '输入 Token', type: 'bar', yAxisIndex: 1, data: inputs,
        barMaxWidth: 28,
        barGap: '30%',
        itemStyle: { color: '#3b82f6', borderRadius: [4, 4, 0, 0] },
        stack: 'tokens',
        emphasis: { focus: 'series', itemStyle: { color: '#2563eb' } },
      },
      {
        name: '输出 Token', type: 'bar', yAxisIndex: 1, data: outputs,
        barMaxWidth: 28,
        itemStyle: { color: '#8b5cf6', borderRadius: [4, 4, 0, 0] },
        stack: 'tokens',
        emphasis: { focus: 'series', itemStyle: { color: '#7c3aed' } },
      },
    ],
  }
}

function buildTokenTrendOption(points: TokenTrendPoint[]) {
  const hours   = points.map(p => p.hour.slice(11))
  const inputs  = points.map(p => p.inputTokens)
  const outputs = points.map(p => p.outputTokens)
  const reqs    = points.map(p => p.requestCount)
  return {
    backgroundColor: 'transparent',
    tooltip: {
      trigger: 'axis',
      backgroundColor: 'rgba(255, 255, 255, 0.95)',
      borderColor: '#e5e7eb',
      borderWidth: 1,
      textStyle: { color: '#374151', fontSize: 12 },
      axisPointer: { type: 'shadow', shadowStyle: { color: 'rgba(99,102,241,0.05)' } },
      formatter(params: any[]) {
        const hour = params[0]?.axisValue || ''
        const lines = params.map((p: any) => {
          const val = p.seriesName === '请求数' ? p.value : formatTokens(p.value)
          return `<div style="display:flex;align-items:center;justify-content:space-between;gap:12px;margin:4px 0">
            <span style="display:flex;align-items:center;gap:6px">
              <span style="display:inline-block;width:10px;height:10px;border-radius:2px;background:${p.color}"></span>
              <span style="color:#6b7280">${p.seriesName}</span>
            </span>
            <span style="font-weight:600;color:#111827">${val}</span>
          </div>`
        }).join('')
        return `<div style="min-width:180px"><div style="font-weight:600;color:#111827;margin-bottom:8px;padding-bottom:6px;border-bottom:1px solid #e5e7eb">${hour}:00</div>${lines}</div>`
      },
    },
    legend: {
      bottom: 0,
      textStyle: { color: '#4b5563', fontSize: 12 },
      itemWidth: 14,
      itemHeight: 10,
      itemGap: 16,
      data: ['输入', '输出', '请求数'],
    },
    grid: { top: 35, left: 60, right: 60, bottom: 50 },
    xAxis: {
      type: 'category', data: hours,
      axisLabel: { color: '#6b7280', fontSize: 11, margin: 12, formatter: (v: string) => `${v}:00` },
      axisLine: { lineStyle: { color: '#e5e7eb', width: 1 } },
      splitLine: { show: false },
    },
    yAxis: [
      {
        type: 'value', name: 'Tokens',
        nameTextStyle: { color: '#6b7280', fontSize: 11, padding: [0, 0, 0, 10] },
        axisLabel: { color: '#6b7280', fontSize: 11, formatter: (v: number) => formatTokens(v) },
        splitLine: { lineStyle: { color: '#f3f4f6', type: 'dashed', width: 1 } },
      },
      {
        type: 'value', name: '请求数', min: 0,
        nameTextStyle: { color: '#6b7280', fontSize: 11, padding: [0, 10, 0, 0] },
        axisLabel: { color: '#6b7280', fontSize: 11 },
        splitLine: { show: false },
        position: 'right',
      },
    ],
    series: [
      {
        name: '输入', type: 'bar', stack: 'tokens', data: inputs,
        barMaxWidth: 28,
        itemStyle: { color: '#3b82f6', borderRadius: [4, 4, 0, 0] },
        emphasis: { focus: 'series', itemStyle: { color: '#2563eb' } },
      },
      {
        name: '输出', type: 'bar', stack: 'tokens', data: outputs,
        barMaxWidth: 28,
        itemStyle: { color: '#8b5cf6', borderRadius: [4, 4, 0, 0] },
        emphasis: { focus: 'series', itemStyle: { color: '#7c3aed' } },
      },
      {
        name: '请求数', type: 'line', yAxisIndex: 1, data: reqs,
        smooth: true, symbol: 'circle', symbolSize: 6,
        lineStyle: { color: '#f59e0b', width: 2.5 },
        itemStyle: { color: '#f59e0b', borderWidth: 2, borderColor: '#fff' },
        emphasis: { focus: 'series', itemStyle: { borderWidth: 3, shadowBlur: 8, shadowColor: 'rgba(245,158,11,0.5)' } },
      },
    ],
  }
}

function buildModelPieOption(items: CostOverview['modelBreakdown']) {
  const data = items.map(m => ({ name: m.modelName || '未知模型', value: +m.totalCostCny.toFixed(5) }))
  return {
    backgroundColor: 'transparent',
    tooltip: {
      trigger: 'item',
      backgroundColor: 'rgba(255, 255, 255, 0.95)',
      borderColor: '#e5e7eb',
      borderWidth: 1,
      textStyle: { color: '#374151', fontSize: 12 },
      formatter: (p: any) => `<div style="padding:4px">
        <div style="font-weight:600;color:#111827;margin-bottom:6px">${p.name}</div>
        <div style="display:flex;justify-content:space-between;gap:16px">
          <span style="color:#6b7280">成本</span>
          <span style="font-weight:600;color:#6366f1">¥${p.value.toFixed(5)}</span>
        </div>
        <div style="display:flex;justify-content:space-between;gap:16px;margin-top:4px">
          <span style="color:#6b7280">占比</span>
          <span style="font-weight:600;color:#111827">${p.percent}%</span>
        </div>
      </div>`,
    },
    legend: {
      type: 'scroll', orient: 'vertical', right: 10, top: 'center',
      textStyle: { color: '#4b5563', fontSize: 12 },
      itemWidth: 12,
      itemHeight: 12,
      itemGap: 10,
      formatter: (name: string) => name.length > 18 ? name.slice(0, 18) + '…' : name,
    },
    series: [{
      type: 'pie', radius: ['48%', '72%'], center: ['38%', '50%'],
      data,
      label: { show: false },
      labelLine: { show: false },
      emphasis: {
        itemStyle: {
          shadowBlur: 15,
          shadowOffsetX: 0,
          shadowColor: 'rgba(0,0,0,0.15)',
          borderWidth: 2,
          borderColor: '#fff',
        },
        label: {
          show: true,
          fontSize: 13,
          fontWeight: 'bold',
          color: '#111827',
          formatter: '{b}\n{d}%',
          lineHeight: 18,
        },
        scale: true,
        scaleSize: 8,
      },
      color: ['#6366f1','#8b5cf6','#3b82f6','#10b981','#f59e0b','#ef4444','#06b6d4','#84cc16'],
    }],
  }
}

function buildIntentBarOption(items: CostOverview['intentBreakdown']) {
  const names = items.map(i => i.traceName || '未知')
  const costs = items.map(i => +i.totalCostCny.toFixed(5))
  const reqs  = items.map(i => i.requestCount)
  return {
    backgroundColor: 'transparent',
    tooltip: {
      trigger: 'axis',
      backgroundColor: 'rgba(255, 255, 255, 0.95)',
      borderColor: '#e5e7eb',
      borderWidth: 1,
      textStyle: { color: '#374151', fontSize: 12 },
      axisPointer: { type: 'shadow', shadowStyle: { color: 'rgba(99,102,241,0.05)' } },
      formatter(params: any[]) {
        const name = params[0]?.axisValue || ''
        const cost = params[0]?.value ?? 0
        const req  = params[1]?.value ?? 0
        return `<div style="padding:4px">
          <div style="font-weight:600;color:#111827;margin-bottom:8px;padding-bottom:6px;border-bottom:1px solid #e5e7eb">${name}</div>
          <div style="display:flex;justify-content:space-between;gap:16px;margin:4px 0">
            <span style="color:#6b7280">成本</span>
            <span style="font-weight:600;color:#6366f1">¥${cost}</span>
          </div>
          <div style="display:flex;justify-content:space-between;gap:16px;margin:4px 0">
            <span style="color:#6b7280">请求数</span>
            <span style="font-weight:600;color:#111827">${req}</span>
          </div>
        </div>`
      },
    },
    grid: { top: 20, left: 130, right: 60, bottom: 25 },
    xAxis: [
      {
        type: 'value',
        axisLabel: { color: '#6b7280', fontSize: 11, formatter: (v: number) => `¥${v}` },
        splitLine: { lineStyle: { color: '#f3f4f6', type: 'dashed', width: 1 } },
      },
      {
        type: 'value',
        position: 'top',
        axisLabel: { color: '#6b7280', fontSize: 11 },
        splitLine: { show: false },
      },
    ],
    yAxis: {
      type: 'category', data: names.slice().reverse(),
      axisLabel: { color: '#4b5563', fontSize: 12, width: 120, overflow: 'truncate' },
      axisLine: { lineStyle: { color: '#e5e7eb', width: 1 } },
    },
    series: [
      {
        name: '成本(CNY)', type: 'bar', xAxisIndex: 0, data: costs.slice().reverse(),
        barMaxWidth: 20,
        itemStyle: {
          color: (p: any) => {
            const colors = ['#6366f1','#8b5cf6','#3b82f6','#10b981','#f59e0b']
            return colors[p.dataIndex % colors.length]
          },
          borderRadius: [0, 4, 4, 0],
        },
        label: {
          show: true,
          position: 'right',
          formatter: (p: any) => `¥${p.value}`,
          color: '#6b7280',
          fontSize: 11,
          fontWeight: 600,
        },
        emphasis: {
          focus: 'series',
          itemStyle: {
            shadowBlur: 8,
            shadowColor: 'rgba(99,102,241,0.3)',
          },
        },
      },
      {
        name: '请求数', type: 'bar', xAxisIndex: 1, data: reqs.slice().reverse(),
        barMaxWidth: 10,
        itemStyle: { color: 'rgba(245,158,11,0.35)', borderRadius: [0, 4, 4, 0] },
        emphasis: { focus: 'series', itemStyle: { color: 'rgba(245,158,11,0.5)' } },
      },
    ],
  }
}

// ── CopyButton ─────────────────────────────────────────────────────────────────

function CopyButton({ text, className }: { text: string; className?: string }) {
  const [copied, setCopied] = useState(false)
  return (
    <button
      onClick={e => {
        e.stopPropagation()
        navigator.clipboard.writeText(text).then(() => {
          setCopied(true)
          setTimeout(() => setCopied(false), 1500)
        })
      }}
      title={`复制: ${text}`}
      className={cn(
        'p-0.5 rounded transition-all flex-shrink-0',
        copied ? 'text-emerald-500 opacity-100' : 'text-gray-300 hover:text-gray-500',
        className,
      )}
    >
      {copied ? <Check className="w-3 h-3" /> : <Copy className="w-3 h-3" />}
    </button>
  )
}

// ── 主组件 ─────────────────────────────────────────────────────────────────────

type TabType = 'overview' | 'list' | 'session'

export default function Traces() {
  const navigate = useNavigate()
  const [searchParams, setSearchParams] = useSearchParams()

  // Tab 状态（URL 同步）
  const tabFromUrl = (searchParams.get('tab') as TabType) || 'list'
  const [activeTab, setActiveTab] = useState<TabType>(tabFromUrl)

  const switchTab = (tab: TabType) => {
    setActiveTab(tab)
    setSearchParams({ tab }, { replace: true })
  }

  // ── 链路列表状态 ────────────────────────────────────────────────────────────
  const [runs, setRuns] = useState<TraceRun[]>([])
  const [total, setTotal] = useState(0)
  const [stats, setStats] = useState<TraceStats | null>(null)
  const [loading, setLoading] = useState(true)
  const [statsLoading, setStatsLoading] = useState(true)
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(10)
  const [statusFilter, setStatusFilter] = useState('')
  const [moduleFilter, setModuleFilter] = useState('')
  const [traceIdFilter, setTraceIdFilter] = useState('')
  const [searchInput, setSearchInput] = useState('')
  const [sessionFilter, setSessionFilter] = useState('')
  const [sessionInput, setSessionInput] = useState('')
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [confirmDelete, setConfirmDelete] = useState<{ open: boolean; id?: string; isBatch?: boolean }>({ open: false })

  // ── 会话时间线状态 ──────────────────────────────────────────────────────────
  const [sessionTimeline, setSessionTimeline] = useState<SessionTimelineSummary[]>([])
  const [sessionTimelineLoading, setSessionTimelineLoading] = useState(false)

  // ── 成本概览状态 ────────────────────────────────────────────────────────────
  const [costRange, setCostRange] = useState<CostRange>('7d')
  const [overview, setOverview] = useState<CostOverview | null>(null)
  const [trendPoints, setTrendPoints] = useState<TokenTrendPoint[]>([])
  const [costLoading, setCostLoading] = useState(false)
  const [trendLoading, setTrendLoading] = useState(false)
  const costTimerRef = useRef<ReturnType<typeof setInterval> | null>(null)

  // ── 数据加载 ────────────────────────────────────────────────────────────────

  const loadStats = useCallback(async () => {
    setStatsLoading(true)
    try { setStats(await traceService.stats(7)) }
    catch (e) { console.error('加载统计数据失败', e) }
    finally { setStatsLoading(false) }
  }, [])

  const loadRuns = useCallback(async () => {
    setLoading(true)
    try {
      const res = await traceService.list({
        page, pageSize,
        status: statusFilter || undefined,
        traceId: traceIdFilter || undefined,
        sessionId: sessionFilter || undefined,
      })
      let list = res.list || []

      // 客户端模块筛选
      if (moduleFilter === 'chat') {
        list = list.filter(r => r.traceName?.startsWith('chat.'))
      } else if (moduleFilter === 'event') {
        list = list.filter(r => r.traceName?.startsWith('event.'))
      }

      setRuns(list)
      setTotal(res.total || 0)
    } catch (e) { console.error('加载链路数据失败', e) }
    finally { setLoading(false) }
  }, [page, pageSize, statusFilter, traceIdFilter, sessionFilter, moduleFilter])

  const loadCostOverview = useCallback(async (r: CostRange) => {
    const cfg = COST_RANGES.find(x => x.value === r)!
    setCostLoading(true)
    try { setOverview(await traceService.costOverview({ days: cfg.days })) }
    catch { /* silent */ }
    finally { setCostLoading(false) }
  }, [])

  const loadTokenTrend = useCallback(async (r: CostRange) => {
    const cfg = COST_RANGES.find(x => x.value === r)!
    const hours = cfg.hours ?? cfg.days * 24
    setTrendLoading(true)
    try {
      const data = await traceService.tokenTrend(Math.min(hours, 72))
      setTrendPoints(data.points || [])
    } catch { /* silent */ }
    finally { setTrendLoading(false) }
  }, [])

  const loadSessionTimeline = async (sid: string) => {
    if (!sid) return
    setSessionTimelineLoading(true)
    try {
      const res = await traceService.sessionTimeline(sid)
      setSessionTimeline(res.runs || [])
    } catch { setSessionTimeline([]) }
    finally { setSessionTimelineLoading(false) }
  }

  useEffect(() => { loadStats() }, [loadStats])
  useEffect(() => { loadRuns() }, [loadRuns])

  // 成本概览：仅在概览 Tab 激活时加载
  useEffect(() => {
    if (activeTab === 'overview') {
      loadCostOverview(costRange)
      loadTokenTrend(costRange)
    }
  }, [activeTab, costRange, loadCostOverview, loadTokenTrend])

  // 24h 时每 5min 自动刷新 Token 趋势
  useEffect(() => {
    if (activeTab !== 'overview' || costRange !== '24h') return
    costTimerRef.current = setInterval(() => loadTokenTrend('24h'), 5 * 60 * 1000)
    return () => { if (costTimerRef.current) clearInterval(costTimerRef.current) }
  }, [activeTab, costRange, loadTokenTrend])

  // 链路自动轮询（有 running 时）
  useEffect(() => {
    const hasRunning = runs.some(r => r.status === 'running')
    if (!hasRunning) return
    const timer = setInterval(() => { loadRuns() }, 5000)
    return () => clearInterval(timer)
  }, [runs, loadRuns])

  // 会话时间线自动加载
  useEffect(() => {
    if (activeTab === 'session' && sessionFilter) {
      setSessionInput(sessionFilter)
      loadSessionTimeline(sessionFilter)
    }
  }, [activeTab, sessionFilter])

  // ── 事件处理 ────────────────────────────────────────────────────────────────

  const handleSearch = () => { setTraceIdFilter(searchInput.trim()); setPage(1) }

  const handleSessionSearch = () => {
    setSessionFilter(sessionInput.trim())
    setPage(1)
    if (activeTab === 'session' && sessionInput.trim()) {
      loadSessionTimeline(sessionInput.trim())
    }
  }

  const toggleSelect = (id: string) => {
    setSelected(prev => {
      const next = new Set(prev)
      next.has(id) ? next.delete(id) : next.add(id)
      return next
    })
  }
  const toggleSelectAll = () => {
    const pageIds = runs.map(r => r.traceId)
    const allSel = pageIds.length > 0 && pageIds.every(id => selected.has(id))
    setSelected(prev => {
      const next = new Set(prev)
      if (allSel) pageIds.forEach(id => next.delete(id))
      else pageIds.forEach(id => next.add(id))
      return next
    })
  }

  const pageAllSelected = runs.length > 0 && runs.every(r => selected.has(r.traceId))
  const pagePartialSelected = !pageAllSelected && runs.some(r => selected.has(r.traceId))

  const handleBatchDelete = async () => {
    const ids = [...selected]
    if (ids.length === 0) return
    try {
      const res = await traceService.batchDelete(ids)
      toast.success(`已删除 ${res.deleted} 条链路记录`)
      setSelected(new Set())
      loadRuns(); loadStats()
    } catch { toast.error('删除失败') }
  }

  const handleDelete = async (traceId: string) => {
    try {
      const res = await traceService.batchDelete([traceId])
      toast.success(`已删除 ${res.deleted} 条链路记录`)
      loadRuns(); loadStats()
    } catch { toast.error('删除失败') }
  }

  const totalPages = Math.ceil(total / pageSize)
  const rv = overview

  // ── 渲染 ────────────────────────────────────────────────────────────────────

  return (
    <div className="flex flex-col gap-5 h-full overflow-auto">

      {/* 标题栏 */}
      <div className="flex items-center justify-between flex-shrink-0">
        <div>
          <h1 className="text-2xl font-semibold text-gray-900 tracking-tight">Agent 全链路观测</h1>
          <p className="text-sm text-gray-500 mt-1">全链路追踪 · Token 消耗 · AI 调用成本分析</p>
        </div>
        <div className="flex items-center gap-2">
          <button
            onClick={() => {
              loadStats(); loadRuns()
              if (activeTab === 'overview') { loadCostOverview(costRange); loadTokenTrend(costRange) }
            }}
            disabled={loading || costLoading}
            className="btn-default"
          >
            <RefreshCw className={cn('w-4 h-4', (loading || costLoading) && 'animate-spin')} />
            刷新
          </button>
        </div>
      </div>

      {/* KPI 卡片（始终可见） */}
      <div className="grid grid-cols-2 sm:grid-cols-4 gap-4 flex-shrink-0">
        {statsLoading ? (
          <div className="col-span-4 flex justify-center py-4">
            <Loader2 className="w-5 h-5 animate-spin text-gray-400" />
          </div>
        ) : (
          <>
            <StatCard label="总请求数（7天）" value={stats?.totalRuns ?? 0} Icon={Activity} tone="blue" />
            <StatCard
              label="成功率"
              value={stats ? `${((1 - stats.errorRate) * 100).toFixed(1)}%` : '-'}
              Icon={CheckCircle2}
              tone="emerald"
              sub={`${stats?.successRuns ?? 0} 成功 / ${stats?.errorRuns ?? 0} 失败`}
            />
            <StatCard
              label="P95 耗时"
              value={formatDuration(stats?.p95DurationMs ?? 0)}
              Icon={Clock}
              tone="amber"
              sub={`均值 ${formatDuration(Math.round(stats?.avgDurationMs ?? 0))}`}
            />
            <StatCard
              label="Token 消耗"
              value={formatTokens((stats?.totalInputTokens ?? 0) + (stats?.totalOutputTokens ?? 0))}
              Icon={Zap}
              tone="blue"
              sub={`↑${formatTokens(stats?.totalInputTokens ?? 0)} ↓${formatTokens(stats?.totalOutputTokens ?? 0)}`}
            />
          </>
        )}
      </div>

      {/* Tab 导航 */}
      <div className="flex items-center gap-1 border-b border-gray-200 flex-shrink-0">
        {([
          { key: 'list',     icon: Activity,    label: '链路列表' },
          { key: 'overview', icon: Banknote,   label: '成本概览' },
          { key: 'session',  icon: GitCommit,    label: '会话时间线' },
        ] as { key: TabType; icon: typeof Activity; label: string }[]).map(({ key, icon: Icon, label }) => (
          <button
            key={key}
            onClick={() => switchTab(key)}
            className={cn(
              'flex items-center gap-1.5 px-4 py-2.5 text-sm font-medium border-b-2 -mb-px transition-colors',
              activeTab === key
                ? 'border-indigo-500 text-indigo-600'
                : 'border-transparent text-gray-500 hover:text-gray-700',
            )}
          >
            <Icon className="w-4 h-4" />
            {label}
          </button>
        ))}
      </div>

      {/* ── Tab: 链路列表 ────────────────────────────────────────────────────── */}
      {activeTab === 'list' && (
        <>
          {/* 过滤栏 */}
          <div className="flex flex-wrap items-center gap-3 flex-shrink-0">
            <CustomSelect
              value={moduleFilter}
              onChange={v => { setModuleFilter(v); setPage(1) }}
              options={[
                { value: '', label: '全部模块' },
                { value: 'chat', label: '智能对话' },
                { value: 'event', label: '事件分析' },
              ]}
            />
            <CustomSelect
              value={statusFilter}
              onChange={v => { setStatusFilter(v); setPage(1) }}
              options={[
                { value: '', label: '全部状态' },
                { value: 'success', label: '成功' },
                { value: 'error', label: '失败' },
                { value: 'running', label: '运行中' },
              ]}
            />
            <div className="flex gap-2 ml-auto">
              {moduleFilter === 'chat' && (
                <>
                  <div className="relative">
                    <input
                      value={sessionInput}
                      onChange={e => setSessionInput(e.target.value)}
                      onKeyDown={e => e.key === 'Enter' && handleSessionSearch()}
                      placeholder="筛选会话 ID..."
                      className="input w-44 text-xs"
                    />
                  </div>
                  <button onClick={handleSessionSearch} className="btn-default text-xs px-2">筛选</button>
                  {sessionFilter && (
                    <button
                      onClick={() => { setSessionFilter(''); setSessionInput(''); setPage(1) }}
                      className="text-xs text-gray-400 hover:text-gray-600 underline"
                    >
                      清除
                    </button>
                  )}
                </>
              )}
              <div className="relative">
                <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-gray-400" />
                <input
                  value={searchInput}
                  onChange={e => setSearchInput(e.target.value)}
                  onKeyDown={e => e.key === 'Enter' && handleSearch()}
                  placeholder="搜索 TraceId..."
                  className="input pl-9 w-52"
                />
              </div>
              <button onClick={handleSearch} className="btn-default">搜索</button>
            </div>
          </div>

          {/* 批量操作栏 */}
          {selected.size > 0 && (
            <div className="flex items-center gap-3 border border-slate-200 bg-slate-50 px-4 py-2 rounded-lg text-sm flex-shrink-0">
              <span className="text-slate-700 font-medium">已选 {selected.size} 条</span>
              <button
                onClick={() => setConfirmDelete({ open: true, isBatch: true })}
                className="px-3 py-1.5 rounded-lg bg-red-100 text-red-700 hover:bg-red-200 text-xs font-medium transition-colors"
              >
                批量删除
              </button>
              <button
                onClick={() => setSelected(new Set())}
                className="ml-auto text-xs text-gray-500 hover:text-gray-700 px-2 py-1 rounded hover:bg-gray-100"
              >
                取消选择
              </button>
            </div>
          )}

          {/* 数据表格 */}
          <div className="card flex-1 min-h-0 flex flex-col overflow-hidden">
            <div className="overflow-auto flex-1">
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b border-gray-100">
                    <th className="px-4 py-3 w-10">
                      <input
                        type="checkbox"
                        checked={pageAllSelected}
                        ref={el => { if (el) el.indeterminate = pagePartialSelected }}
                        onChange={toggleSelectAll}
                        className="w-4 h-4 rounded border-gray-300 text-gray-900 cursor-pointer"
                      />
                    </th>
                    {['TraceID', moduleFilter === 'chat' ? '会话 ID' : moduleFilter === 'event' ? '事件 ID' : '功能模块', '查询内容', '状态', '耗时', 'Token', '成本', '时间'].map(h => (
                      <th key={h} className="px-4 py-3 text-left text-xs font-semibold text-gray-800 whitespace-nowrap">
                        {h}
                      </th>
                    ))}
                    <th className="px-4 py-3 text-center text-xs font-semibold text-gray-800 w-16">操作</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-gray-50">
                  {loading ? (
                    <tr>
                      <td colSpan={10} className="px-4 py-12 text-center">
                        <Loader2 className="w-6 h-6 animate-spin mx-auto text-gray-400" />
                      </td>
                    </tr>
                  ) : runs.length === 0 ? (
                    <tr>
                      <td colSpan={10} className="px-4 py-12 text-center text-sm text-gray-400">
                        暂无链路数据
                      </td>
                    </tr>
                  ) : (
                    runs.map(run => {
                      const sc = statusConfig[run.status] || statusConfig.running
                      const isSelected = selected.has(run.traceId)
                      return (
                        <tr
                          key={run.traceId}
                          onClick={() => navigate(`/traces/${run.traceId}`)}
                          className={cn(
                            'hover:bg-gray-50 cursor-pointer transition-colors',
                            isSelected && 'bg-indigo-50/50',
                          )}
                        >
                          <td className="px-4 py-3 w-10" onClick={e => e.stopPropagation()}>
                            <input
                              type="checkbox"
                              checked={isSelected}
                              onChange={() => toggleSelect(run.traceId)}
                              className="w-4 h-4 rounded border-gray-300 text-gray-900 cursor-pointer"
                            />
                          </td>
                          <td className="px-4 py-3">
                            <div className="flex items-center gap-1.5 group/tid">
                              <code
                                className="text-xs font-mono bg-indigo-50 text-indigo-600 px-1.5 py-0.5 rounded"
                                title={run.traceId}
                              >
                                {run.traceId.slice(0, 8)}…
                              </code>
                              <CopyButton text={run.traceId} className="opacity-0 group-hover/tid:opacity-100" />
                            </div>
                          </td>
                          <td className="px-4 py-3 min-w-[140px]" onClick={e => e.stopPropagation()}>
                            {run.sessionId ? (
                              moduleFilter === 'chat' ? (
                                <div className="flex items-center gap-1.5 group/sid">
                                  <button
                                    onClick={() => {
                                      setSessionInput(run.sessionId)
                                      setSessionFilter(run.sessionId)
                                      setPage(1)
                                    }}
                                    className="text-xs font-mono text-indigo-500 hover:text-indigo-700 bg-indigo-50 px-1.5 py-0.5 rounded"
                                    title={run.sessionId}
                                  >
                                    {run.sessionId.split('-').pop() ?? run.sessionId.slice(-10)}
                                  </button>
                                  <CopyButton text={run.sessionId} className="opacity-0 group-hover/sid:opacity-100" />
                                </div>
                              ) : (
                                <button
                                  onClick={(e) => { e.stopPropagation(); setModuleFilter('chat') }}
                                  className="inline-flex items-center gap-1 px-2 py-1 rounded-full text-xs font-medium bg-blue-50 text-blue-700 border border-blue-200 hover:bg-blue-100 transition-colors cursor-pointer"
                                  title="筛选智能对话"
                                >
                                  智能对话
                                </button>
                              )
                            ) : (
                              (() => {
                                try {
                                  const tags = run.tags ? JSON.parse(run.tags) : {}
                                  if (tags.context === 'standalone_event_analysis') {
                                    return (
                                      <button
                                        onClick={(e) => { e.stopPropagation(); setModuleFilter('event') }}
                                        className="inline-flex items-center gap-1 px-2 py-1 rounded-full text-xs font-medium bg-amber-50 text-amber-700 border border-amber-200 hover:bg-amber-100 transition-colors cursor-pointer"
                                        title="筛选事件分析"
                                      >
                                        事件分析
                                      </button>
                                    )
                                  }
                                } catch {}
                                return <span className="text-gray-300 text-xs">-</span>
                              })()
                            )}
                          </td>
                          <td className="px-4 py-3 max-w-xs">
                            <p className="text-gray-800 truncate">{truncateQuery(run.queryText)}</p>
                            {run.traceName && (
                              <p className="text-xs text-gray-400 mt-0.5">{run.traceName}</p>
                            )}
                          </td>
                          <td className="px-4 py-3">
                            <span className={cn('inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-xs font-medium', sc.class)}>
                              {run.status === 'success' && <CheckCircle2 className="w-3 h-3" />}
                              {run.status === 'error' && <XCircle className="w-3 h-3" />}
                              {run.status === 'running' && <Clock className="w-3 h-3" />}
                              {sc.label}
                              {run.errorCode && <span className="opacity-60 ml-1">·{run.errorCode}</span>}
                            </span>
                          </td>
                          <td className="px-4 py-3 text-gray-600 tabular-nums whitespace-nowrap">
                            {formatDuration(run.durationMs)}
                          </td>
                          <td className="px-4 py-3 text-xs tabular-nums whitespace-nowrap">
                            <span className="text-blue-600">↑{formatTokens(run.totalInputTokens)}</span>
                            <span className="text-gray-300 mx-1">/</span>
                            <span className="text-violet-600">↓{formatTokens(run.totalOutputTokens)}</span>
                          </td>
                          {/* 成本列（新增） */}
                          <td className="px-4 py-3 text-xs tabular-nums whitespace-nowrap">
                            {run.estimatedCostCny != null && run.estimatedCostCny > 0 ? (
                              <span className="text-indigo-600 font-medium">{fmtCost(run.estimatedCostCny)}</span>
                            ) : (
                              <span className="text-gray-300">-</span>
                            )}
                          </td>
                          <td className="px-4 py-3 text-xs text-gray-400 whitespace-nowrap">
                            {formatTime(run.startTime)}
                          </td>
                          <td className="px-4 py-3 text-center" onClick={e => e.stopPropagation()}>
                            <button
                              onClick={e => { e.stopPropagation(); setConfirmDelete({ open: true, id: run.traceId }) }}
                              className="inline-flex items-center justify-center w-7 h-7 rounded-md border border-red-200 bg-red-50 text-red-600 hover:bg-red-100 transition-all"
                              title="删除"
                            >
                              <Trash2 className="w-3.5 h-3.5" />
                            </button>
                          </td>
                        </tr>
                      )
                    })
                  )}
                </tbody>
              </table>
            </div>
            {!loading && total > 0 && (
              <Pagination
                page={page}
                totalPages={totalPages}
                total={total}
                pageSize={pageSize}
                onChange={setPage}
                onPageSizeChange={size => { setPageSize(size); setPage(1) }}
              />
            )}
          </div>
        </>
      )}

      {/* ── Tab: 成本概览 ────────────────────────────────────────────────────── */}
      {activeTab === 'overview' && (
        <>
          {/* 时间范围 + KPI 成本卡片 */}
          <div className="flex items-center justify-between flex-shrink-0">
            <div className="flex items-center bg-gray-100 rounded-lg p-1 gap-0.5">
              {COST_RANGES.map(r => (
                <button
                  key={r.value}
                  onClick={() => setCostRange(r.value)}
                  className={cn(
                    'px-3 py-1.5 rounded-md text-xs font-medium transition-all',
                    costRange === r.value ? 'bg-white shadow text-gray-900' : 'text-gray-500 hover:text-gray-700',
                  )}
                >
                  {r.label}
                </button>
              ))}
            </div>
          </div>

          {costLoading ? (
            <div className="flex justify-center py-8 flex-shrink-0">
              <Loader2 className="w-6 h-6 animate-spin text-gray-400" />
            </div>
          ) : (
            <>
              {/* 成本 KPI 卡片行 */}
              <div className="grid grid-cols-2 sm:grid-cols-3 gap-4 flex-shrink-0">
                <StatCard
                  label="总成本"
                  value={rv ? fmtCost(rv.totalCostCny) : '-'}
                  Icon={Banknote}
                  tone="blue"
                  sub={rv && rv.prevTotalCostCny > 0
                    ? <span className={cn('text-xs', rv.costChangePct >= 0 ? 'text-red-500' : 'text-emerald-500')}>
                        {fmtPct(rv.costChangePct)} 较上周期
                      </span>
                    : undefined
                  }
                />
                <StatCard
                  label="总 Token 消耗"
                  value={rv ? formatTokens((rv.totalInputTokens ?? 0) + (rv.totalOutputTokens ?? 0)) : '-'}
                  Icon={Zap}
                  tone="blue"
                  sub={rv ? `↑${formatTokens(rv.totalInputTokens)} ↓${formatTokens(rv.totalOutputTokens)}` : undefined}
                />
                <StatCard
                  label="请求次数"
                  value={rv?.totalRequests ?? 0}
                  Icon={BarChart2}
                  tone="emerald"
                  sub={rv ? `均 ${fmtCost(rv.avgCostPerReq)}/次` : undefined}
                />
              </div>

              {/* 趋势图行 */}
              <div className="grid grid-cols-1 lg:grid-cols-2 gap-4 flex-shrink-0">
                <div className="card card-body">
                  <div className="flex items-center gap-2 mb-3">
                    <CalendarDays className="w-4 h-4 text-indigo-500" />
                    <h2 className="text-sm font-semibold text-gray-800">每日成本趋势</h2>
                  </div>
                  {(rv?.dailyTrend?.length ?? 0) > 0 ? (
                    <ReactECharts option={buildDailyTrendOption(rv!.dailyTrend)} style={{ height: 220 }} notMerge />
                  ) : (
                    <div className="flex items-center justify-center h-[220px] text-sm text-gray-400">暂无数据</div>
                  )}
                </div>
                <div className="card card-body">
                  <div className="flex items-center gap-2 mb-3">
                    <TrendingUp className="w-4 h-4 text-violet-500" />
                    <h2 className="text-sm font-semibold text-gray-800">
                      实时 Token 趋势
                      <span className="text-xs text-gray-400 font-normal ml-2">（最近 {costRange === '24h' ? '24h' : '72h'}，小时粒度）</span>
                    </h2>
                    {costRange === '24h' && (
                      <span className="ml-auto flex items-center gap-1 text-xs text-emerald-600">
                        <span className="inline-block w-2 h-2 rounded-full bg-emerald-400 animate-pulse" />
                        实时
                      </span>
                    )}
                  </div>
                  {trendLoading ? (
                    <div className="flex items-center justify-center h-[220px]">
                      <Loader2 className="w-5 h-5 animate-spin text-gray-400" />
                    </div>
                  ) : trendPoints.length > 0 ? (
                    <ReactECharts option={buildTokenTrendOption(trendPoints)} style={{ height: 220 }} notMerge />
                  ) : (
                    <div className="flex items-center justify-center h-[220px] text-sm text-gray-400">暂无数据</div>
                  )}
                </div>
              </div>

              {/* 模型成本 + 意图分布 */}
              <div className="grid grid-cols-1 lg:grid-cols-2 gap-4 flex-shrink-0">
                <div className="card card-body">
                  <div className="flex items-center gap-2 mb-3">
                    <Cpu className="w-4 h-4 text-blue-500" />
                    <h2 className="text-sm font-semibold text-gray-800">模型成本占比</h2>
                  </div>
                  {(rv?.modelBreakdown?.length ?? 0) > 0 ? (
                    <>
                      {rv!.modelBreakdown.every(m => m.totalCostCny === 0) ? (
                        <div className="flex flex-col items-center justify-center h-[200px] text-sm text-gray-400 gap-2">
                          <span>模型成本数据待采集</span>
                          <span className="text-xs text-gray-300">节点级成本将在下次请求后更新</span>
                        </div>
                      ) : (
                        <ReactECharts option={buildModelPieOption(rv!.modelBreakdown)} style={{ height: 200 }} notMerge />
                      )}
                      <div className="mt-3 space-y-1.5">
                        {rv!.modelBreakdown.slice(0, 5).map((m, i) => (
                          <div key={i} className="flex items-center gap-2 text-xs">
                            <span className="flex-1 text-gray-700 truncate" title={m.modelName}>{m.modelName || '未知'}</span>
                            <span className="text-gray-400 tabular-nums">↑{formatTokens(m.inputTokens)} ↓{formatTokens(m.outputTokens)}</span>
                            <span className="text-indigo-600 font-medium w-16 text-right tabular-nums">{fmtCost(m.totalCostCny)}</span>
                            <span className="text-gray-400 w-10 text-right tabular-nums">{m.costPct.toFixed(1)}%</span>
                          </div>
                        ))}
                      </div>
                    </>
                  ) : (
                    <div className="flex items-center justify-center h-[200px] text-sm text-gray-400">暂无模型数据</div>
                  )}
                </div>
                <div className="card card-body">
                  <div className="flex items-center gap-2 mb-3">
                    <Tag className="w-4 h-4 text-emerald-500" />
                    <h2 className="text-sm font-semibold text-gray-800">意图成本分布 <span className="text-xs text-gray-400 font-normal">TOP 10</span></h2>
                  </div>
                  {(rv?.intentBreakdown?.length ?? 0) > 0 ? (
                    <ReactECharts option={buildIntentBarOption(rv!.intentBreakdown)} style={{ height: 280 }} notMerge />
                  ) : (
                    <div className="flex items-center justify-center h-[280px] text-sm text-gray-400">暂无数据</div>
                  )}
                </div>
              </div>

              {/* 意图成本明细表 */}
              {(rv?.intentBreakdown?.length ?? 0) > 0 && (
                <div className="card flex-shrink-0">
                  <div className="flex items-center gap-2 px-5 py-3 border-b border-gray-100">
                    <Banknote className="w-4 h-4 text-gray-500" />
                    <h2 className="text-sm font-semibold text-gray-800">意图成本明细</h2>
                  </div>
                  <div className="overflow-x-auto">
                    <table className="w-full text-sm">
                      <thead>
                        <tr className="border-b border-gray-50">
                          {['链路类型', '请求数', '总成本', '均次成本', '占比'].map(h => (
                            <th key={h} className="px-4 py-2.5 text-left text-xs font-semibold text-gray-500 whitespace-nowrap">{h}</th>
                          ))}
                        </tr>
                      </thead>
                      <tbody className="divide-y divide-gray-50">
                        {rv!.intentBreakdown.map((item, i) => (
                          <tr key={i} className="hover:bg-gray-50 transition-colors">
                            <td className="px-4 py-2.5 text-gray-800 font-medium">{item.traceName || '—'}</td>
                            <td className="px-4 py-2.5 text-gray-600 tabular-nums">{item.requestCount}</td>
                            <td className="px-4 py-2.5 text-indigo-600 font-medium tabular-nums">{fmtCost(item.totalCostCny)}</td>
                            <td className="px-4 py-2.5 text-gray-600 tabular-nums">{fmtCost(item.avgCostCny)}</td>
                            <td className="px-4 py-2.5">
                              <div className="flex items-center gap-2">
                                <div className="h-1.5 w-20 rounded-full bg-gray-100">
                                  <div
                                    className="h-full rounded-full bg-indigo-400 transition-all"
                                    style={{ width: `${Math.min(100, item.costPct)}%` }}
                                  />
                                </div>
                                <span className="text-xs text-gray-500 tabular-nums w-10">{item.costPct.toFixed(1)}%</span>
                              </div>
                            </td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                </div>
              )}
            </>
          )}
        </>
      )}

      {/* ── Tab: 会话时间线 ──────────────────────────────────────────────────── */}
      {activeTab === 'session' && (
        <>
          <div className="flex gap-2 flex-shrink-0">
            <div className="relative flex-1 max-w-sm">
              <input
                value={sessionInput}
                onChange={e => setSessionInput(e.target.value)}
                onKeyDown={e => e.key === 'Enter' && handleSessionSearch()}
                placeholder="输入会话 ID 查看时间线..."
                className="input w-full text-sm"
              />
            </div>
            <button onClick={handleSessionSearch} className="btn-default">查询</button>
            {sessionFilter && (
              <button
                onClick={() => { setSessionFilter(''); setSessionInput('') }}
                className="text-xs text-gray-400 hover:text-gray-600 underline"
              >
                清除
              </button>
            )}
          </div>
          <div className="card card-body flex-1 overflow-auto">
            {!sessionFilter ? (
              <div className="flex flex-col items-center justify-center py-16 text-gray-400">
                <GitCommit className="w-10 h-10 mb-3 opacity-40" />
                <p className="text-sm">请输入会话 ID 查看该会话的完整链路时间线</p>
                <p className="text-xs mt-1 text-gray-300">在链路列表中点击会话 ID 可快速跳转并筛选</p>
              </div>
            ) : sessionTimelineLoading ? (
              <div className="flex items-center justify-center py-16">
                <Loader2 className="w-6 h-6 animate-spin text-gray-400" />
              </div>
            ) : sessionTimeline.length === 0 ? (
              <div className="flex flex-col items-center justify-center py-16 text-gray-400">
                <GitCommit className="w-10 h-10 mb-3 opacity-40" />
                <p className="text-sm">该会话下没有链路记录</p>
              </div>
            ) : (
              <div className="max-w-2xl mx-auto">
                <p className="text-xs text-gray-400 mb-4">
                  会话 <code className="font-mono bg-gray-100 px-1.5 py-0.5 rounded">{sessionFilter}</code> 共 {sessionTimeline.length} 条链路
                </p>
                <div className="relative">
                  <div className="absolute left-4 top-2 bottom-2 w-0.5 bg-gray-200" />
                  <div className="flex flex-col gap-3">
                    {sessionTimeline.map((run, i) => {
                      const sc = statusConfig[run.status] || statusConfig.running
                      return (
                        <div key={run.traceId} className="flex gap-4">
                          <div className="w-9 flex-shrink-0 flex items-start justify-center pt-2.5">
                            <div className={cn(
                              'w-3 h-3 rounded-full border-2 border-white z-10',
                              run.status === 'success' ? 'bg-emerald-400' : run.status === 'error' ? 'bg-red-400' : 'bg-amber-400',
                            )} />
                          </div>
                          <div
                            className="flex-1 bg-white border border-gray-200 rounded-lg p-3 cursor-pointer hover:shadow-sm transition-shadow mb-1"
                            onClick={() => navigate(`/traces/${run.traceId}`)}
                          >
                            <div className="flex items-center justify-between mb-1.5">
                              <span className={cn('inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-xs font-medium', sc.class)}>
                                {run.status === 'success' && <CheckCircle2 className="w-3 h-3" />}
                                {run.status === 'error' && <XCircle className="w-3 h-3" />}
                                {run.status === 'running' && <Clock className="w-3 h-3" />}
                                {sc.label}
                              </span>
                              <div className="flex items-center gap-2 text-xs text-gray-400">
                                <span className="tabular-nums">{formatDuration(run.durationMs)}</span>
                                <span>{formatTime(run.startTime)}</span>
                              </div>
                            </div>
                            <p className="text-sm text-gray-700 truncate">
                              {run.queryText || <span className="text-gray-400">(无查询文本)</span>}
                            </p>
                            {run.errorCode && (
                              <p className="text-xs text-red-400 mt-0.5">[{run.errorCode}]</p>
                            )}
                            <p className="text-[10px] text-gray-300 mt-1 font-mono">#{i + 1} · {run.traceId.slice(0, 8)}…</p>
                          </div>
                        </div>
                      )
                    })}
                  </div>
                </div>
              </div>
            )}
          </div>
        </>
      )}

      <ConfirmDialog
        open={confirmDelete.open}
        title={confirmDelete.isBatch ? `删除 ${selected.size} 条链路记录` : '删除链路记录'}
        description={
          confirmDelete.isBatch
            ? `确定要删除选中的 ${selected.size} 条链路记录吗？此操作无法撤销。`
            : '确定要删除该链路记录吗？此操作无法撤销。'
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
