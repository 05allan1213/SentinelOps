import { useState, useEffect, useCallback, useRef } from 'react'
import {
  TrendingUp, TrendingDown, Zap, RefreshCw,
  Loader2, BarChart2, Cpu, Tag, CalendarDays, DollarSign,
} from 'lucide-react'
import ReactECharts from 'echarts-for-react'
import { cn } from '@/utils'
import StatCard from '@/components/common/StatCard'
import {
  traceService,
  type CostOverview,
  type TokenTrendPoint,
} from '@/services/trace'

// ── 格式化工具 ─────────────────────────────────────────────────────────────────

function fmtCost(usd: number): string {
  if (usd === 0) return '0.000'
  if (usd < 0.001) return `${(usd * 1000).toFixed(3)}m`
  if (usd < 1) return `${usd.toFixed(4)}`
  return `${usd.toFixed(3)}`
}

function fmtTokens(n: number): string {
  if (!n) return '0'
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(2)}M`
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}k`
  return String(n)
}

function fmtPct(v: number): string {
  return `${v >= 0 ? '+' : ''}${v.toFixed(1)}%`
}

// ── 时间范围选择器 ─────────────────────────────────────────────────────────────
type Range = '24h' | '7d' | '30d' | '90d'
const RANGES: { value: Range; label: string; days: number; hours?: number }[] = [
  { value: '24h', label: '今日', days: 1, hours: 24 },
  { value: '7d',  label: '7 天', days: 7 },
  { value: '30d', label: '30 天', days: 30 },
  { value: '90d', label: '90 天', days: 90 },
]

// ── 日期格式化（ISO → MM-DD）─────────────────────────────────────────────────────
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
          const val = p.seriesName === '成本(CNY)' ? `¥${p.value}` : fmtTokens(p.value)
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
      textStyle: { color: '#6b7280', fontSize: 11 },
      itemWidth: 14,
      itemHeight: 10,
      itemGap: 16,
      data: ['成本(CNY)', '输入 Token', '输出 Token'],
    },
    grid: { top: 30, left: 65, right: 65, bottom: 50 },
    xAxis: {
      type: 'category',
      data: dates,
      boundaryGap: true,
      axisLine: { lineStyle: { color: '#e5e7eb' } },
      axisLabel: { color: '#6b7280', fontSize: 11, margin: 10, interval: 0 },
      axisTick: { show: true, lineStyle: { color: '#e5e7eb' } },
      splitLine: { show: false },
    },
    yAxis: [
      {
        type: 'value',
        name: '¥',
        nameTextStyle: { color: '#6b7280', fontSize: 10 },
        axisLabel: { color: '#6b7280', fontSize: 10, formatter: (v: number) => `¥${v}` },
        splitLine: { lineStyle: { color: '#f3f4f6', type: 'dashed', width: 1 } },
        position: 'left',
      },
      {
        type: 'value',
        name: 'Tokens',
        nameTextStyle: { color: '#6b7280', fontSize: 10 },
        axisLabel: { color: '#6b7280', fontSize: 10, formatter: (v: number) => fmtTokens(v) },
        splitLine: { show: false },
        position: 'right',
      },
    ],
    series: [
      {
        name: '成本(CNY)',
        type: 'line',
        yAxisIndex: 0,
        data: costs,
        smooth: true,
        symbol: 'circle',
        symbolSize: 6,
        lineStyle: { width: 2.5, color: '#10b981', type: 'solid' },
        itemStyle: { color: '#10b981', borderWidth: 2, borderColor: '#fff' },
        areaStyle: {
          color: {
            type: 'linear',
            x: 0, y: 0, x2: 0, y2: 1,
            colorStops: [
              { offset: 0, color: 'rgba(16,185,129,0.25)' },
              { offset: 1, color: 'rgba(16,185,129,0.02)' },
            ],
          },
        },
        emphasis: { focus: 'series', itemStyle: { borderWidth: 3, shadowBlur: 8, shadowColor: 'rgba(16,185,129,0.4)' } },
      },
      {
        name: '输入 Token',
        type: 'bar',
        yAxisIndex: 1,
        data: inputs,
        barMaxWidth: 28,
        barGap: '30%',
        itemStyle: { color: '#3b82f6', borderRadius: [4, 4, 0, 0] },
        stack: 'tokens',
        emphasis: { focus: 'series', itemStyle: { color: '#2563eb' } },
      },
      {
        name: '输出 Token',
        type: 'bar',
        yAxisIndex: 1,
        data: outputs,
        barMaxWidth: 28,
        itemStyle: { color: '#8b5cf6', borderRadius: [4, 4, 0, 0] },
        stack: 'tokens',
        emphasis: { focus: 'series', itemStyle: { color: '#7c3aed' } },
      },
    ],
  }
}

function buildTokenTrendOption(points: TokenTrendPoint[]) {
  const hours   = points.map(p => p.hour.slice(11)) // "HH"
  const inputs  = points.map(p => p.inputTokens)
  const outputs = points.map(p => p.outputTokens)
  const cached  = points.map(p => p.inputTokens)
  const reqs    = points.map(p => p.requestCount)

  return {
    backgroundColor: 'transparent',
    tooltip: {
      trigger: 'axis',
      axisPointer: { type: 'shadow' },
    },
    legend: {
      bottom: 0, textStyle: { color: '#6b7280', fontSize: 11 },
      data: ['输入', '输出', '缓存', '请求数'],
    },
    grid: { top: 20, left: 55, right: 55, bottom: 50 },
    xAxis: {
      type: 'category', data: hours,
      axisLabel: { color: '#9ca3af', fontSize: 10, formatter: (v: string) => `${v}:00` },
      axisLine: { lineStyle: { color: '#e5e7eb' } },
      splitLine: { show: false },
    },
    yAxis: [
      {
        type: 'value', name: 'Tokens',
        axisLabel: { color: '#9ca3af', fontSize: 10, formatter: (v: number) => fmtTokens(v) },
        splitLine: { lineStyle: { color: '#f3f4f6', type: 'dashed' } },
      },
      {
        type: 'value', name: '请求数', min: 0,
        axisLabel: { color: '#9ca3af', fontSize: 10 },
        splitLine: { show: false },
        position: 'right',
      },
    ],
    series: [
      {
        name: '输入', type: 'bar', stack: 'tokens', data: inputs,
        itemStyle: { color: 'rgba(59,130,246,0.75)', borderRadius: [0,0,0,0] },
      },
      {
        name: '输出', type: 'bar', stack: 'tokens', data: outputs,
        itemStyle: { color: 'rgba(139,92,246,0.75)', borderRadius: [0,0,0,0] },
      },
      {
        name: '缓存', type: 'bar', stack: 'tokens', data: cached,
        itemStyle: { color: 'rgba(16,185,129,0.75)', borderRadius: [2,2,0,0] },
      },
      {
        name: '请求数', type: 'line', yAxisIndex: 1, data: reqs,
        symbol: 'circle', symbolSize: 4,
        lineStyle: { color: '#f59e0b', width: 1.5 },
        itemStyle: { color: '#f59e0b' },
      },
    ],
  }
}

function buildModelPieOption(items: CostOverview['modelBreakdown']) {
  const data = items.map(m => ({
    name: m.modelName || '未知模型',
    value: +m.totalCostCny.toFixed(5),
  }))
  return {
    backgroundColor: 'transparent',
    tooltip: {
      trigger: 'item',
      formatter: (p: any) => `${p.name}<br/>$${p.value.toFixed(5)} (${p.percent}%)`,
    },
    legend: {
      type: 'scroll', orient: 'vertical', right: 10, top: 'center',
      textStyle: { color: '#6b7280', fontSize: 11 },
      formatter: (name: string) => name.length > 20 ? name.slice(0, 20) + '…' : name,
    },
    series: [{
      type: 'pie', radius: ['45%', '70%'], center: ['40%', '50%'],
      data,
      label: { show: false },
      emphasis: {
        itemStyle: { shadowBlur: 10, shadowOffsetX: 0, shadowColor: 'rgba(0,0,0,0.1)' },
        label: { show: true, fontSize: 12, fontWeight: 'bold', formatter: '{b}\n{d}%' },
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
      axisPointer: { type: 'shadow' },
      formatter(params: any[]) {
        const name = params[0]?.axisValue || ''
        const cost = params[0]?.value ?? 0
        const req  = params[1]?.value ?? 0
        return `<div style="font-size:12px"><b>${name}</b><br/>成本: ¥${cost}<br/>请求数: ${req}</div>`
      },
    },
    grid: { top: 15, left: 120, right: 55, bottom: 20 },
    xAxis: [
      {
        type: 'value',
        axisLabel: { color: '#9ca3af', fontSize: 10, formatter: (v: number) => `¥${v}` },
        splitLine: { lineStyle: { color: '#f3f4f6', type: 'dashed' } },
      },
      {
        type: 'value', position: 'top',
        axisLabel: { color: '#9ca3af', fontSize: 10 },
        splitLine: { show: false },
      },
    ],
    yAxis: {
      type: 'category', data: names.slice().reverse(),
      axisLabel: { color: '#6b7280', fontSize: 11, width: 110, overflow: 'truncate' },
      axisLine: { lineStyle: { color: '#e5e7eb' } },
    },
    series: [
      {
        name: '成本(USD)', type: 'bar', xAxisIndex: 0, data: costs.slice().reverse(),
        barMaxWidth: 18,
        itemStyle: {
          color: (p: any) => {
            const colors = ['#6366f1','#8b5cf6','#3b82f6','#10b981','#f59e0b']
            return colors[p.dataIndex % colors.length]
          },
          borderRadius: [0, 3, 3, 0],
        },
        label: { show: true, position: 'right', formatter: (p: any) => `$${p.value}`, color: '#6b7280', fontSize: 10 },
      },
      {
        name: '请求数', type: 'bar', xAxisIndex: 1, data: reqs.slice().reverse(),
        barMaxWidth: 8,
        itemStyle: { color: 'rgba(251,191,36,0.4)', borderRadius: [0, 3, 3, 0] },
      },
    ],
  }
}

// ── 主组件 ─────────────────────────────────────────────────────────────────────

export default function CostMonitor() {
  const [range, setRange] = useState<Range>('7d')
  const [overview, setOverview] = useState<CostOverview | null>(null)
  const [trendPoints, setTrendPoints] = useState<TokenTrendPoint[]>([])
  const [loading, setLoading] = useState(true)
  const [trendLoading, setTrendLoading] = useState(true)
  const timerRef = useRef<ReturnType<typeof setInterval> | null>(null)

  const loadData = useCallback(async (r: Range) => {
    const cfg = RANGES.find(x => x.value === r)!
    setLoading(true)
    try {
      const data = await traceService.costOverview({ days: cfg.days })
      setOverview(data)
    } catch {
      /* silent */
    } finally {
      setLoading(false)
    }
  }, [])

  const loadTrend = useCallback(async (r: Range) => {
    const cfg = RANGES.find(x => x.value === r)!
    const hours = cfg.hours ?? cfg.days * 24
    setTrendLoading(true)
    try {
      const data = await traceService.tokenTrend(Math.min(hours, 72))
      setTrendPoints(data.points || [])
    } catch {
      /* silent */
    } finally {
      setTrendLoading(false)
    }
  }, [])

  useEffect(() => {
    loadData(range)
    loadTrend(range)
  }, [range, loadData, loadTrend])

  // 24h 时每 5min 自动刷新 Token 趋势
  useEffect(() => {
    if (range !== '24h') return
    timerRef.current = setInterval(() => loadTrend('24h'), 5 * 60 * 1000)
    return () => { if (timerRef.current) clearInterval(timerRef.current) }
  }, [range, loadTrend])

  const rv = overview

  return (
    <div className="flex flex-col gap-5 h-full overflow-auto">

      {/* 标题栏 */}
      <div className="flex items-center justify-between flex-shrink-0">
        <div>
          <h1 className="text-2xl font-semibold text-gray-900 tracking-tight">成本监控</h1>
          <p className="text-sm text-gray-500 mt-1">Token 消耗实时统计与 AI 调用成本分析</p>
        </div>
        <div className="flex items-center gap-2">
          {/* 时间范围 */}
          <div className="flex items-center bg-gray-100 rounded-lg p-1 gap-0.5">
            {RANGES.map(r => (
              <button
                key={r.value}
                onClick={() => setRange(r.value)}
                className={cn(
                  'px-3 py-1.5 rounded-md text-xs font-medium transition-all',
                  range === r.value ? 'bg-white shadow text-gray-900' : 'text-gray-500 hover:text-gray-700',
                )}
              >
                {r.label}
              </button>
            ))}
          </div>
          <button
            onClick={() => { loadData(range); loadTrend(range) }}
            disabled={loading}
            className="btn-default"
          >
            <RefreshCw className={cn('w-4 h-4', loading && 'animate-spin')} />
            刷新
          </button>
        </div>
      </div>

      {/* KPI 卡片行 */}
      {loading ? (
        <div className="flex justify-center py-8">
          <Loader2 className="w-6 h-6 animate-spin text-gray-400" />
        </div>
      ) : (
        <>
          <div className="grid grid-cols-2 sm:grid-cols-4 gap-4 flex-shrink-0">
            <StatCard
              label="总成本"
              value={rv ? fmtCost(rv.totalCostCny) : '-'}
              Icon={DollarSign}
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
              value={rv ? fmtTokens((rv.totalInputTokens ?? 0) + (rv.totalOutputTokens ?? 0)) : '-'}
              Icon={Zap}
              tone="blue"
              sub={rv ? `↑${fmtTokens(rv.totalInputTokens)} ↓${fmtTokens(rv.totalOutputTokens)}` : undefined}
            />
            <StatCard
              label="请求次数"
              value={rv?.totalRequests ?? 0}
              Icon={BarChart2}
              tone="emerald"
              sub={rv ? `均 ${fmtCost(rv.avgCostPerReq)}/次` : undefined}
            />
            <StatCard
              label="缓存节省"
              value={rv ? fmtTokens(rv.totalInputTokens ?? 0) : '-'}
              Icon={TrendingDown}
              tone="emerald"
              sub={rv && (rv.totalInputTokens + rv.totalInputTokens) > 0
                ? `节省率 ${((rv.totalInputTokens / (rv.totalInputTokens + rv.totalInputTokens)) * 100).toFixed(1)}%`
                : undefined
              }
            />
          </div>

          {/* 每日成本 + Token 趋势图 */}
          <div className="grid grid-cols-1 lg:grid-cols-2 gap-4 flex-shrink-0">
            {/* 每日趋势 */}
            <div className="card card-body">
              <div className="flex items-center gap-2 mb-3">
                <CalendarDays className="w-4 h-4 text-indigo-500" />
                <h2 className="text-sm font-semibold text-gray-800">每日成本趋势</h2>
              </div>
              {(rv?.dailyTrend?.length ?? 0) > 0 ? (
                <ReactECharts
                  option={buildDailyTrendOption(rv!.dailyTrend)}
                  style={{ height: 220 }}
                  notMerge
                />
              ) : (
                <div className="flex items-center justify-center h-[220px] text-sm text-gray-400">暂无数据</div>
              )}
            </div>

            {/* 实时 Token 趋势（小时粒度） */}
            <div className="card card-body">
              <div className="flex items-center gap-2 mb-3">
                <TrendingUp className="w-4 h-4 text-violet-500" />
                <h2 className="text-sm font-semibold text-gray-800">
                  实时 Token 趋势
                  <span className="text-xs text-gray-400 font-normal ml-2">（最近 {range === '24h' ? '24h' : '72h'}，小时粒度）</span>
                </h2>
                {range === '24h' && (
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
                <ReactECharts
                  option={buildTokenTrendOption(trendPoints)}
                  style={{ height: 220 }}
                  notMerge
                />
              ) : (
                <div className="flex items-center justify-center h-[220px] text-sm text-gray-400">暂无数据</div>
              )}
            </div>
          </div>

          {/* 模型成本分布 + 意图分布 */}
          <div className="grid grid-cols-1 lg:grid-cols-2 gap-4 flex-shrink-0">
            {/* 模型饼图 */}
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
                  <ReactECharts
                    option={buildModelPieOption(rv!.modelBreakdown)}
                    style={{ height: 200 }}
                    notMerge
                  />
                  )}
                  {/* 模型明细表 */}
                  <div className="mt-3 space-y-1.5">
                    {rv!.modelBreakdown.slice(0, 5).map((m, i) => (
                      <div key={i} className="flex items-center gap-2 text-xs">
                        <span className="flex-1 text-gray-700 truncate" title={m.modelName}>{m.modelName || '未知'}</span>
                        <span className="text-gray-400 tabular-nums">↑{fmtTokens(m.inputTokens)} ↓{fmtTokens(m.outputTokens)}</span>
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

            {/* 意图/链路成本横向柱状图 */}
            <div className="card card-body">
              <div className="flex items-center gap-2 mb-3">
                <Tag className="w-4 h-4 text-emerald-500" />
                <h2 className="text-sm font-semibold text-gray-800">意图成本分布 <span className="text-xs text-gray-400 font-normal">TOP 10</span></h2>
              </div>
              {(rv?.intentBreakdown?.length ?? 0) > 0 ? (
                <ReactECharts
                  option={buildIntentBarOption(rv!.intentBreakdown)}
                  style={{ height: 280 }}
                  notMerge
                />
              ) : (
                <div className="flex items-center justify-center h-[280px] text-sm text-gray-400">暂无数据</div>
              )}
            </div>
          </div>

          {/* 成本明细表 */}
          {(rv?.intentBreakdown?.length ?? 0) > 0 && (
            <div className="card flex-shrink-0">
              <div className="flex items-center gap-2 px-5 py-3 border-b border-gray-100">
                <DollarSign className="w-4 h-4 text-gray-500" />
                <h2 className="text-sm font-semibold text-gray-800">意图成本明细</h2>
              </div>
              <div className="overflow-x-auto">
                <table className="w-full text-sm">
                  <thead>
                    <tr className="border-b border-gray-50">
                      {['链路类型', '请求数', '总成本', '均次成本', '输入 Token', '输出 Token', '占比'].map(h => (
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
                        <td className="px-4 py-2.5 text-blue-600 tabular-nums">—</td>
                        <td className="px-4 py-2.5 text-violet-600 tabular-nums">—</td>
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
    </div>
  )
}
