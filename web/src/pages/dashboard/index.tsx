import { useState, useEffect, useMemo, type ReactNode, type CSSProperties } from 'react'
import {
  ShieldAlert,
  Rss,
  TrendingUp,
  TrendingDown,
  RefreshCw,
  Loader2,
  AlertCircle,
  Info,
  Lightbulb,
  Activity,
} from 'lucide-react'
import ReactECharts from 'echarts-for-react'
import { cn } from '@/utils'
import EventTrendChart from './components/EventTrendChart'
import SeverityDistribution from './components/SeverityDistribution'
import RecentEvents from './components/RecentEvents'
import SubscriptionStatus from './components/SubscriptionStatus'
import { eventService } from '@/services/event'
import { subscriptionService } from '@/services/subscription'
import { reportService } from '@/services/report'

// ============================================================================
// 类型定义
// ============================================================================

type HealthStatus = 'healthy' | 'attention' | 'critical'
type InsightType = 'anomaly' | 'trend' | 'recommendation'

interface DashboardStats {
  eventCount: number
  subscriptionCount: number
  reportCount: number
  todayCount: number
  criticalCount: number
  highCount: number
  mediumCount: number
  weekDelta: number | null
}

interface InsightItem {
  type: InsightType
  title: string
  metric: string
  change: string
  context: string
  action?: string
}

// ============================================================================
// 基础 UI 组件
// ============================================================================

function DashCard({
  children,
  className,
  style,
}: {
  children: ReactNode
  className?: string
  style?: CSSProperties
}) {
  return (
    <div
      className={cn('rounded-2xl bg-white p-5', className)}
      style={{ boxShadow: '0 1px 3px rgba(0,0,0,0.08)', ...style }}
    >
      {children}
    </div>
  )
}

function CardTitle({ children }: { children: ReactNode }) {
  return <h3 className="mb-4 text-sm font-semibold text-slate-700">{children}</h3>
}

function LoadingBlock({ className }: { className?: string }) {
  return <div className={cn('animate-pulse rounded-lg bg-slate-100', className)} />
}

// ============================================================================
// 核心指标 KPI
// ============================================================================

interface KPICardProps {
  label: string
  value: string
  delta?: number | null
  deltaLabel?: string
  /** 无环比/占比时底部展示的说明（避免误显示「--」） */
  caption?: string
  /** bad = 上升是坏事（事件数增加），good = 上升是好事 */
  deltaTone?: 'bad' | 'good' | 'neutral'
  icon: typeof ShieldAlert
  iconBg: string
  iconColor: string
}

function KPICard({
  label,
  value,
  delta,
  deltaLabel,
  caption,
  deltaTone = 'neutral',
  icon: Icon,
  iconBg,
  iconColor,
}: KPICardProps) {
  const showDelta = delta !== null && delta !== undefined
  const isUp = (delta ?? 0) > 0
  // bad：上升 = 红色；good：上升 = 绿色；neutral：上升 = 绿色
  const positive =
    deltaTone === 'bad'
      ? !isUp
      : isUp
  const deltaColor = showDelta
    ? positive
      ? 'text-emerald-600'
      : 'text-red-500'
    : 'text-slate-400'

  return (
    <div className="rounded-xl bg-slate-50 p-4">
      <div className="flex items-start justify-between">
        <div>
          <p className="text-2xl font-bold tracking-tight text-slate-900">{value}</p>
          <p className="mt-1 text-sm text-slate-500">{label}</p>
        </div>
        <div
          className="flex h-10 w-10 items-center justify-center rounded-xl flex-shrink-0"
          style={{ backgroundColor: iconBg, color: iconColor }}
        >
          <Icon className="h-5 w-5" />
        </div>
      </div>
      {(showDelta || caption) && (
        <div className="mt-3 flex items-center gap-1.5 text-sm min-h-[1.25rem]">
          {showDelta ? (
            <>
              {isUp ? (
                <TrendingUp className={cn('h-4 w-4', deltaColor)} />
              ) : (
                <TrendingDown className={cn('h-4 w-4', deltaColor)} />
              )}
              <span className={cn('font-medium', deltaColor)}>
                {(delta ?? 0) > 0 ? '+' : ''}
                {(delta ?? 0).toFixed(1)}%
              </span>
              <span className="text-slate-400">{deltaLabel ?? '较上周期'}</span>
            </>
          ) : (
            <span className="text-slate-400 leading-snug">{caption}</span>
          )}
        </div>
      )}
    </div>
  )
}

function KPISection({ stats, loading }: { stats: DashboardStats; loading: boolean }) {
  if (loading) {
    return (
      <DashCard>
        <CardTitle>核心指标</CardTitle>
        <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
          {[1, 2, 3, 4].map(i => (
            <LoadingBlock key={i} className="h-28" />
          ))}
        </div>
      </DashCard>
    )
  }

  const total = Math.max(stats.eventCount, 1)
  const criticalPct = (stats.criticalCount / total) * 100
  const weekDeltaPct =
    stats.weekDelta !== null
      ? (stats.weekDelta / Math.max(stats.eventCount - stats.weekDelta, 1)) * 100
      : null

  const items: KPICardProps[] = [
    {
      label: '安全事件',
      value: stats.eventCount.toLocaleString(),
      delta: weekDeltaPct,
      deltaTone: 'bad',
      icon: ShieldAlert,
      iconBg: '#FEE2E2',
      iconColor: '#DC2626',
    },
    {
      label: '严重事件',
      value: stats.criticalCount.toLocaleString(),
      delta: criticalPct > 0 ? criticalPct : null,
      deltaLabel: '占总事件比',
      deltaTone: 'bad',
      icon: Activity,
      iconBg: '#FEF3C7',
      iconColor: '#D97706',
    },
    {
      label: '今日新增',
      value: stats.todayCount.toLocaleString(),
      delta:
        stats.weekDelta !== null
          ? stats.weekDelta > 0
            ? stats.weekDelta
            : stats.weekDelta
          : null,
      deltaLabel: '较上周同期',
      deltaTone: 'bad',
      icon: TrendingUp,
      iconBg: '#DBEAFE',
      iconColor: '#2563EB',
    },
    {
      label: '活跃订阅',
      value: stats.subscriptionCount.toString(),
      caption: '当前启用中的情报订阅源',
      icon: Rss,
      iconBg: '#DCFCE7',
      iconColor: '#16A34A',
    },
  ]

  return (
    <DashCard>
      <CardTitle>核心指标</CardTitle>
      <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
        {items.map(item => (
          <KPICard key={item.label} {...item} />
        ))}
      </div>
    </DashCard>
  )
}

// ============================================================================
// 事件流量概览（面积图）
// ============================================================================

type FlowTimeWindow = '7d' | '30d'

const FLOW_TIME_OPTIONS: Array<{ value: FlowTimeWindow; label: string }> = [
  { value: '7d', label: '7天' },
  { value: '30d', label: '30天' },
]

function EventFlowSection({ loading }: { loading?: boolean }) {
  const [timeWindow, setTimeWindow] = useState<FlowTimeWindow>('7d')
  const [flowData, setFlowData] = useState<{ date: string; total: number }[]>([])
  const [flowLoading, setFlowLoading] = useState(true)

  useEffect(() => {
    const days = timeWindow === '30d' ? 30 : 7
    setFlowLoading(true)
    eventService
      .getTrend(days)
      .then(items => {
        // 生成完整日期序列，缺失天填 0，确保折线有足够数据点
        const dataMap = new Map(
          items.map(i => [i.date, i.critical + i.high + i.medium + i.low]),
        )
        const filled = Array.from({ length: days }, (_, idx) => {
          const d = new Date()
          d.setDate(d.getDate() - (days - 1 - idx))
          const dateStr = d.toISOString().slice(0, 10)
          return { date: dateStr, total: dataMap.get(dateStr) ?? 0 }
        })
        setFlowData(filled)
      })
      .catch(console.error)
      .finally(() => setFlowLoading(false))
  }, [timeWindow])

  const option = useMemo(
    () => ({
      backgroundColor: 'transparent',
      tooltip: {
        trigger: 'axis',
        axisPointer: {
          type: 'line',
          lineStyle: { color: '#e2e8f0', type: 'dashed' },
        },
        backgroundColor: '#1e293b',
        borderColor: '#334155',
        borderWidth: 1,
        textStyle: { color: '#f1f5f9', fontSize: 12 },
        extraCssText:
          'box-shadow: 0 4px 16px rgba(0,0,0,0.3); border-radius: 8px;',
        // eslint-disable-next-line @typescript-eslint/no-explicit-any
        formatter: (params: any[]) => {
          if (!params.length) return ''
          const p = params[0]
          return `<div style="font-weight:600;margin-bottom:4px;color:#94a3b8">${p.name}</div><div style="display:flex;align-items:center;gap:8px">${p.marker}<span>事件数</span><strong>${p.value}</strong></div>`
        },
      },
      grid: { left: 12, right: 12, bottom: 24, top: 8, containLabel: true },
      xAxis: {
        type: 'category',
        boundaryGap: false,
        data: flowData.map(d => d.date.slice(5).replace('-', '/')),
        axisLine: { show: false },
        axisTick: { show: false },
        axisLabel: {
          color: '#94a3b8',
          fontSize: 11,
          alignMinLabel: 'left',
          alignMaxLabel: 'right',
        },
        splitLine: { show: false },
      },
      yAxis: {
        type: 'value',
        minInterval: 1,
        axisLine: { show: false },
        axisTick: { show: false },
        axisLabel: { color: '#94a3b8', fontSize: 11 },
        splitLine: { lineStyle: { color: '#f1f5f9', type: 'dashed' } },
      },
      series: [
        {
          type: 'line',
          data: flowData.map(d => d.total),
          smooth: 0.3,
          smoothMonotone: 'x',
          clip: true,
          symbol: 'none',
          lineStyle: { color: '#3B82F6', width: 2.5 },
          areaStyle: {
            color: {
              type: 'linear',
              x: 0,
              y: 0,
              x2: 0,
              y2: 1,
              colorStops: [
                { offset: 0, color: 'rgba(59,130,246,0.22)' },
                { offset: 1, color: 'rgba(59,130,246,0.02)' },
              ],
            },
          },
          emphasis: { focus: 'series' },
        },
      ],
      animation: true,
      animationDuration: 600,
    }),
    [flowData],
  )

  return (
    <DashCard className="flex flex-col">
      <div className="mb-3 flex items-start justify-between gap-3">
        <div className="min-w-0">
          <p className="text-sm font-semibold text-slate-700">事件流量概览</p>
          <p className="mt-0.5 text-xs text-slate-400">
            {timeWindow === '7d' ? '近 7 天' : '近 30 天'} 事件总量趋势
          </p>
        </div>
        <div className="flex shrink-0 items-center gap-0.5">
          {FLOW_TIME_OPTIONS.map(opt => (
            <button
              key={opt.value}
              type="button"
              onClick={() => setTimeWindow(opt.value)}
              disabled={loading}
              className={cn(
                'rounded-md px-2.5 py-1 text-xs font-medium transition-colors disabled:opacity-50',
                timeWindow === opt.value
                  ? 'bg-primary-500 text-white'
                  : 'text-gray-500 hover:text-gray-700',
              )}
            >
              {opt.label}
            </button>
          ))}
        </div>
      </div>
      <div style={{ height: 220 }}>
        {flowLoading || loading ? (
          <div className="flex h-full items-center justify-center">
            <Loader2 className="h-5 w-5 animate-spin text-slate-300" />
          </div>
        ) : flowData.length === 0 ? (
          <div className="flex h-full items-center justify-center text-sm text-slate-400">
            暂无流量数据
          </div>
        ) : (
          <div className="h-full w-full min-w-0 overflow-hidden">
            <ReactECharts
              option={option}
              style={{ height: '100%', width: '100%' }}
              opts={{ renderer: 'svg' }}
            />
          </div>
        )}
      </div>
    </DashCard>
  )
}

// ============================================================================
// 安全健康度（右侧侧栏 - 环形图 + 质量柱 + 运营概览）
// ============================================================================

const HEALTH_CONFIG: Record<
  HealthStatus,
  { bg: string; text: string; label: string; ringColor: string }
> = {
  healthy: {
    bg: 'bg-emerald-100',
    text: 'text-emerald-700',
    label: '运行正常',
    ringColor: '#10B981',
  },
  attention: {
    bg: 'bg-amber-100',
    text: 'text-amber-700',
    label: '需要关注',
    ringColor: '#F59E0B',
  },
  critical: {
    bg: 'bg-red-100',
    text: 'text-red-700',
    label: '风险偏高',
    ringColor: '#EF4444',
  },
}

function SecurityHealthCard({
  stats,
  loading,
}: {
  stats: DashboardStats
  loading: boolean
}) {
  const total = Math.max(stats.eventCount, 1)
  const criticalPct = (stats.criticalCount / total) * 100
  const highPct = (stats.highCount / total) * 100
  const mediumPct = (stats.mediumCount / total) * 100
  const todayPct = Math.min((stats.todayCount / total) * 100, 100)

  // 安全评分：从 100 按严重度扣分
  const securityScore = Math.max(
    0,
    Math.min(100, 100 - criticalPct * 0.5 - highPct * 0.2 - mediumPct * 0.05),
  )
  const health: HealthStatus =
    securityScore >= 80 ? 'healthy' : securityScore >= 60 ? 'attention' : 'critical'
  const cfg = HEALTH_CONFIG[health]

  const radius = 50
  const circumference = 2 * Math.PI * radius
  const dashOffset = circumference - (securityScore / 100) * circumference

  const qualityItems = [
    {
      label: '严重占比',
      value: criticalPct,
      color: 'bg-red-500',
      textColor: 'text-red-600',
      target: '阈值 ≤5%',
    },
    {
      label: '高危占比',
      value: highPct,
      color: 'bg-orange-500',
      textColor: 'text-orange-600',
      target: '阈值 ≤20%',
    },
    {
      label: '今日增量',
      value: todayPct,
      color: 'bg-sky-500',
      textColor: 'text-sky-600',
      target: '',
    },
  ]

  const clamp = (v: number) => Math.max(0, Math.min(100, v))

  const overviewMetrics = [
    { label: '活跃订阅', value: `${stats.subscriptionCount} 个` },
    { label: '今日事件', value: `${stats.todayCount} 条` },
    { label: '分析报告', value: `${stats.reportCount} 份` },
  ]

  if (loading) {
    return (
      <DashCard>
        <LoadingBlock className="h-72" />
      </DashCard>
    )
  }

  return (
    <DashCard className="space-y-4">
      {/* 标题 + 状态徽章 */}
      <div className="flex items-center justify-between">
        <h3 className="text-sm font-semibold text-slate-800">安全健康度</h3>
        <span
          className={cn(
            'inline-flex items-center gap-1 rounded-full px-2.5 py-0.5 text-xs font-medium',
            cfg.bg,
            cfg.text,
          )}
        >
          <span className="h-1.5 w-1.5 rounded-full" style={{ backgroundColor: cfg.ringColor }} />
          {cfg.label}
        </span>
      </div>

      {/* 环形进度图 */}
      <div className="flex justify-center">
        <div className="relative">
          <svg className="-rotate-90" viewBox="0 0 120 120" width="116" height="116">
            <circle cx="60" cy="60" r={radius} fill="none" stroke="#F1F5F9" strokeWidth={9} />
            <circle
              cx="60" cy="60" r={radius} fill="none"
              stroke={cfg.ringColor} strokeWidth={9}
              strokeLinecap="round"
              strokeDasharray={circumference}
              strokeDashoffset={dashOffset}
              className="transition-all duration-700 ease-out"
            />
          </svg>
          <div className="absolute inset-0 flex flex-col items-center justify-center">
            <span className="text-[22px] font-bold leading-none tabular-nums" style={{ color: cfg.ringColor }}>
              {securityScore.toFixed(1)}%
            </span>
            <span className="mt-1 text-[11px] text-slate-400 tracking-wide">安全评分</span>
          </div>
        </div>
      </div>

      {/* 威胁分布柱状快照 */}
      <div className="rounded-xl border border-slate-100 bg-slate-50 p-3">
        <p className="mb-2.5 text-xs font-medium text-slate-600">威胁分布（柱状）</p>
        <div className="grid grid-cols-3 gap-2">
          {qualityItems.map(item => (
            <div key={item.label} className="space-y-1">
              <div className="flex h-20 items-end rounded-md border border-slate-200 bg-white p-1.5">
                <div
                  className={cn('w-full rounded-sm transition-[height] duration-500', item.color)}
                  style={{ height: `${Math.max(clamp(item.value), item.value > 0 ? 4 : 0)}%` }}
                />
              </div>
              <div className={cn('text-center text-xs font-semibold tabular-nums', item.textColor)}>
                {item.value.toFixed(1)}%
              </div>
              <div className="text-center text-[10px] text-slate-500 leading-tight">{item.label}</div>
              {item.target && (
                <div className="text-center text-[9px] text-slate-400">{item.target}</div>
              )}
            </div>
          ))}
        </div>
      </div>

      {/* 运营概览 */}
      <div className="rounded-xl border border-slate-100 bg-slate-50 p-3">
        <p className="mb-1.5 text-xs font-medium text-slate-600">运营概览</p>
        <div className="divide-y divide-slate-100">
          {overviewMetrics.map(m => (
            <div key={m.label} className="flex items-center justify-between py-2">
              <span className="text-xs text-slate-500">{m.label}</span>
              <span className="text-sm font-semibold tabular-nums text-slate-700">{m.value}</span>
            </div>
          ))}
        </div>
      </div>
    </DashCard>
  )
}

// ============================================================================
// 安全洞察（右侧侧栏 - 自动生成）
// ============================================================================

const INSIGHT_ICON: Record<InsightType, typeof Info> = {
  anomaly: AlertCircle,
  trend: Info,
  recommendation: Lightbulb,
}

const INSIGHT_STYLE: Record<InsightType, string> = {
  anomaly: 'bg-red-50 text-red-600',
  trend: 'bg-blue-50 text-blue-600',
  recommendation: 'bg-amber-50 text-amber-600',
}

const INSIGHT_LABEL: Record<InsightType, string> = {
  anomaly: '异常',
  trend: '趋势',
  recommendation: '建议',
}

function buildInsights(stats: DashboardStats): InsightItem[] {
  const items: InsightItem[] = []
  const total = Math.max(stats.eventCount, 1)

  // 严重事件洞察
  if (stats.criticalCount > 0) {
    items.push({
      type: 'anomaly',
      title: '检测到严重威胁事件',
      metric: '严重事件',
      change: `${stats.criticalCount} 条 (${((stats.criticalCount / total) * 100).toFixed(1)}%)`,
      context: '存在高优先级安全事件，需立即评估',
      action: '建议进行 Agent 深度分析',
    })
  } else {
    items.push({
      type: 'trend',
      title: '近期无严重威胁',
      metric: '严重事件',
      change: '0 条',
      context: '当前周期内未检测到严重级别事件',
    })
  }

  // 周环比洞察
  if (stats.weekDelta !== null) {
    const isIncrease = stats.weekDelta > 0
    const abs = Math.abs(stats.weekDelta)
    items.push({
      type: isIncrease ? 'anomaly' : 'trend',
      title: isIncrease ? '事件量环比上升' : '事件量环比下降',
      metric: '周环比变化',
      change: `${isIncrease ? '+' : '-'}${abs} 条 vs 上周`,
      context: isIncrease
        ? '事件增长可能反映新型威胁活动'
        : '威胁态势趋于平稳',
      action: isIncrease ? '建议增加订阅监控覆盖范围' : undefined,
    })
  }

  // 订阅覆盖洞察
  if (stats.subscriptionCount === 0) {
    items.push({
      type: 'recommendation',
      title: '无活跃订阅源',
      metric: '数据覆盖',
      change: '0 个订阅',
      context: '缺少威胁情报来源将降低检测覆盖率',
      action: '建议添加 RSS 或 GitHub 安全公告订阅',
    })
  } else {
    items.push({
      type: 'recommendation',
      title: '情报来源运行正常',
      metric: '活跃订阅',
      change: `${stats.subscriptionCount} 个订阅源`,
      context: '当前威胁情报采集管道运行稳定',
    })
  }

  return items.slice(0, 3)
}

function InsightPanel({
  stats,
  loading,
}: {
  stats: DashboardStats
  loading: boolean
}) {
  const insights = useMemo(() => buildInsights(stats), [stats])

  return (
    <DashCard className="flex flex-col">
      <CardTitle>安全洞察</CardTitle>
      {loading ? (
        <div className="space-y-3">
          {[1, 2, 3].map(i => (
            <LoadingBlock key={i} className="h-24" />
          ))}
        </div>
      ) : (
        <div className="space-y-3">
          {insights.map((item, i) => {
            const Icon = INSIGHT_ICON[item.type]
            return (
              <div key={i} className="rounded-xl bg-slate-50 p-3.5">
                <div className="mb-2 flex items-center">
                  <span
                    className={cn(
                      'inline-flex items-center gap-1 rounded-md px-2 py-0.5 text-xs font-medium',
                      INSIGHT_STYLE[item.type],
                    )}
                  >
                    <Icon className="h-3.5 w-3.5" />
                    {INSIGHT_LABEL[item.type]}
                  </span>
                </div>
                <p className="text-sm font-semibold text-slate-800">{item.title}</p>
                <p className="mt-1 text-xs text-slate-500">
                  {item.metric}：{item.change}
                </p>
                <p className="mt-0.5 text-xs text-slate-400">归因：{item.context}</p>
                {item.action && (
                  <p className="mt-1 text-xs font-medium text-slate-600">
                    建议：{item.action}
                  </p>
                )}
              </div>
            )
          })}
        </div>
      )}
    </DashCard>
  )
}

// ============================================================================
// 主仪表盘
// ============================================================================

export default function Dashboard() {
  const [loading, setLoading] = useState(true)
  const [refreshing, setRefreshing] = useState(false)
  const [lastUpdated, setLastUpdated] = useState<Date | null>(null)
  const [refreshKey, setRefreshKey] = useState(0)
  const [stats, setStats] = useState<DashboardStats>({
    eventCount: 0,
    subscriptionCount: 0,
    reportCount: 0,
    todayCount: 0,
    criticalCount: 0,
    highCount: 0,
    mediumCount: 0,
    weekDelta: null,
  })

  const fetchStats = async () => {
    try {
      const [subscriptionsRes, reportsRes, eventStats, trendItems] =
        await Promise.all([
          subscriptionService.list(1, 1),
          reportService.list(1, 1),
          eventService.getStats(),
          eventService.getTrend(14),
        ])

      // 计算近7天 vs 前7天 事件总量 delta
      let weekDelta: number | null = null
      if (trendItems && trendItems.length >= 2) {
        const sorted = [...trendItems].sort((a, b) => a.date.localeCompare(b.date))
        const recent7 = sorted.slice(-7)
        const prev7 = sorted.slice(0, Math.min(7, sorted.length - recent7.length))
        const recentTotal = recent7.reduce(
          (s, i) => s + i.critical + i.high + i.medium + i.low,
          0,
        )
        const prevTotal = prev7.reduce(
          (s, i) => s + i.critical + i.high + i.medium + i.low,
          0,
        )
        weekDelta = recentTotal - prevTotal
      }

      const bySeverity = eventStats.by_severity || {}
      setStats({
        eventCount: eventStats.total || 0,
        subscriptionCount: subscriptionsRes.total || 0,
        reportCount: reportsRes.total || 0,
        todayCount: eventStats.today_count || 0,
        // criticalCount 仅取 critical 级别，避免与 highCount 重复计算安全评分
        criticalCount: bySeverity['critical'] || 0,
        highCount: bySeverity['high'] || 0,
        mediumCount: bySeverity['medium'] || 0,
        weekDelta,
      })
      setLastUpdated(new Date())
    } catch (error) {
      console.error('获取统计数据失败:', error)
    } finally {
      setLoading(false)
      setRefreshing(false)
    }
  }

  useEffect(() => {
    fetchStats()
  }, [])

  const handleRefresh = () => {
    setRefreshing(true)
    setRefreshKey(k => k + 1)
    fetchStats()
  }

  const formatLastUpdated = () => {
    if (!lastUpdated) return '-'
    return lastUpdated.toLocaleString('zh-CN', {
      month: '2-digit',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit',
      hour12: false,
    })
  }

  return (
    <div className="space-y-5">
      {/* 页面 Header */}
      <header className="flex items-center justify-between">
        <div>
          <h1 className="text-4xl font-bold tracking-tight text-slate-900">安全态势总览与实时监控</h1>
        </div>
        <div className="flex items-center gap-3">
          {/* 最后更新时间 */}
          {lastUpdated && (
            <div className="flex items-center gap-2 text-sm text-slate-400">
              <span className="h-1.5 w-1.5 rounded-full bg-emerald-500" />
              <span>{formatLastUpdated()}</span>
            </div>
          )}

          {/* 刷新按钮 */}
          <button
            onClick={handleRefresh}
            disabled={refreshing}
            className="h-9 w-9 flex items-center justify-center rounded-lg border border-slate-200 bg-white text-slate-500 hover:text-slate-700 transition-colors disabled:opacity-50"
          >
            <RefreshCw className={cn('h-4 w-4', refreshing && 'animate-spin')} />
          </button>
        </div>
      </header>

      {/* 主体双列布局 */}
      <div className="flex items-start gap-5">
        {/* 左侧主区 */}
        <div className="flex-1 min-w-0 space-y-5">
          {/* 核心指标 */}
          <KPISection stats={stats} loading={loading} />

          {/* 事件流量概览 */}
          <EventFlowSection loading={loading} />

          {/* 趋势分析 + 级别分布 */}
          <div className="grid grid-cols-1 lg:grid-cols-3 gap-5">
            <DashCard className="min-w-0 lg:col-span-2">
              <div className="mb-3 text-sm font-semibold text-slate-700">事件趋势</div>
              <EventTrendChart refreshKey={refreshKey} />
            </DashCard>
            <DashCard>
              <div className="mb-3 text-sm font-semibold text-slate-700">
                严重级别分布
              </div>
              <SeverityDistribution refreshKey={refreshKey} />
            </DashCard>
          </div>

          {/* 底部：最新事件 + 订阅状态 */}
          <div className="grid grid-cols-1 lg:grid-cols-2 gap-5 items-stretch">
            <DashCard className="flex flex-col h-full">
              <div className="flex items-center justify-between mb-0">
                <div className="text-sm font-semibold text-slate-700">最新事件</div>
                <a
                  href="/events"
                  className="text-xs text-slate-400 hover:text-slate-700 transition-colors"
                >
                  查看全部 →
                </a>
              </div>
              <div className="-mx-5 -mb-5 mt-3 overflow-hidden rounded-b-2xl flex-1">
                <RecentEvents refreshKey={refreshKey} />
              </div>
            </DashCard>
            <DashCard className="flex flex-col h-full">
              <div className="flex items-center justify-between mb-0">
                <div className="text-sm font-semibold text-slate-700">订阅状态</div>
                <a
                  href="/subscriptions"
                  className="text-xs text-slate-400 hover:text-slate-700 transition-colors"
                >
                  管理订阅 →
                </a>
              </div>
              <div className="-mx-5 -mb-5 mt-3 overflow-hidden rounded-b-2xl flex-1">
                <SubscriptionStatus refreshKey={refreshKey} />
              </div>
            </DashCard>
          </div>
        </div>

        {/* 右侧侧栏：aside 本身 sticky + alignSelf:start，避免 grid stretch 留白 */}
        <aside
          className="hidden xl:block space-y-5"
          style={{ position: 'sticky', top: '16px', alignSelf: 'start' }}
        >
          <SecurityHealthCard stats={stats} loading={loading} />
          <InsightPanel stats={stats} loading={loading} />
        </aside>
      </div>
    </div>
  )
}
