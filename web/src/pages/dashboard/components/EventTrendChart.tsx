import { useState, useEffect } from 'react'
import ReactECharts from 'echarts-for-react'
import { Loader2 } from 'lucide-react'
import { eventService } from '@/services/event'
import { cn } from '@/utils'

// 时间窗口选项
const TIME_WINDOWS = [
  { label: '7天', value: 7 },
  { label: '30天', value: 30 },
  { label: '90天', value: 90 },
] as const

type WindowValue = 7 | 30 | 90

export default function EventTrendChart({ refreshKey }: { refreshKey?: number }) {
  const [loading, setLoading] = useState(true)
  const [timeWindow, setTimeWindow] = useState<WindowValue>(30)
  const [dates, setDates] = useState<string[]>([])
  const [series, setSeries] = useState<{ critical: number[]; high: number[]; medium: number[]; low: number[] }>({
    critical: [],
    high: [],
    medium: [],
    low: [],
  })

  useEffect(() => {
    const fetchData = async () => {
      setLoading(true)
      try {
        const items = await eventService.getTrend(timeWindow)
        // 生成完整日期序列，缺失天填 0，保证折线点数充足
        const dataMap = new Map(items.map(i => [i.date, i]))
        const filled = Array.from({ length: timeWindow }, (_, idx) => {
          const d = new Date()
          d.setDate(d.getDate() - (timeWindow - 1 - idx))
          const dateStr = d.toISOString().slice(0, 10)
          return dataMap.get(dateStr) ?? { date: dateStr, critical: 0, high: 0, medium: 0, low: 0 }
        })
        setDates(filled.map(i => i.date))
        setSeries({
          critical: filled.map(i => i.critical),
          high: filled.map(i => i.high),
          medium: filled.map(i => i.medium),
          low: filled.map(i => i.low),
        })
      } catch (error) {
        console.error('获取事件趋势失败:', error)
      } finally {
        setLoading(false)
      }
    }
    fetchData()
  }, [timeWindow, refreshKey])

  // 时间窗口 Tab 条，始终渲染（含 loading 态）
  const tabBar = (
    <div className="flex items-center gap-0.5 mb-3">
      {TIME_WINDOWS.map(tw => (
        <button
          key={tw.value}
          onClick={() => setTimeWindow(tw.value as WindowValue)}
          className={cn(
            'px-2.5 py-1 text-xs rounded-md font-medium transition-colors',
            timeWindow === tw.value
              ? 'bg-primary-500 text-white'
              : 'text-gray-500 hover:text-gray-700'
          )}
        >
          {tw.label}
        </button>
      ))}
    </div>
  )

  if (loading) {
    return (
      <div>
        {tabBar}
        <div className="flex items-center justify-center" style={{ height: '272px' }}>
          <Loader2 className="w-6 h-6 animate-spin text-primary-500" />
        </div>
      </div>
    )
  }

  if (dates.length === 0) {
    return (
      <div>
        {tabBar}
        <div className="flex items-center justify-center text-gray-400 text-sm" style={{ height: '272px' }}>
          暂无事件趋势数据
        </div>
      </div>
    )
  }

  const mkArea = (color: string, opacity0 = 0.25, opacity1 = 0.02) => ({
    color: {
      type: 'linear', x: 0, y: 0, x2: 0, y2: 1,
      colorStops: [
        { offset: 0, color: color.replace(')', `,${opacity0})`).replace('rgb', 'rgba') },
        { offset: 1, color: color.replace(')', `,${opacity1})`).replace('rgb', 'rgba') },
      ],
    },
  })

  const option = {
    backgroundColor: 'transparent',
    tooltip: {
      trigger: 'axis',
      axisPointer: { type: 'cross', crossStyle: { color: '#94a3b8' }, lineStyle: { color: '#e2e8f0', type: 'dashed' } },
      backgroundColor: '#1e293b',
      borderColor: '#334155',
      borderWidth: 1,
      textStyle: { color: '#f1f5f9', fontSize: 13 },
      extraCssText: 'box-shadow: 0 4px 16px rgba(0,0,0,0.4); border-radius: 8px;',
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      formatter: (params: any[]) => {
        if (!params.length) return ''
        let html = `<div style="font-weight:600;margin-bottom:6px;color:#94a3b8">${String(params[0].name).substring(0, 10)}</div>`
        params.forEach((p: any) => {
          html += `<div style="display:flex;align-items:center;gap:8px;margin:3px 0">${p.marker}<span style="flex:1">${p.seriesName}</span><strong>${p.value}</strong></div>`
        })
        return html
      },
    },
    legend: {
      data: ['严重', '高危', '中危', '低危'],
      textStyle: { color: '#64748b', fontSize: 12 },
      top: 4,
      right: 8,
      icon: 'circle',
      itemWidth: 8,
      itemHeight: 8,
      itemGap: 16,
    },
    graphic: dates.length > 0 ? [{
      type: 'text',
      left: 12,
      bottom: 4,
      style: {
        text: dates[0]?.slice(0, 4) === dates[dates.length - 1]?.slice(0, 4)
          ? dates[0]?.slice(0, 4) + ' 年'
          : `${dates[0]?.slice(0, 4)} — ${dates[dates.length - 1]?.slice(0, 4)}`,
        fill: '#94a3b8',
        fontSize: 11,
      },
    }] : [],
    grid: { left: 12, right: 12, bottom: 28, top: 36, containLabel: true },
    xAxis: {
      type: 'category',
      // 贴满左右，避免两端大块留白；smoothMonotone + clip 抑制平滑曲线外凸
      boundaryGap: false,
      data: dates,
      axisLine: { show: false },
      axisTick: { show: false },
      axisLabel: {
        color: '#94a3b8',
        fontSize: 11,
        rotate: 0,
        margin: 8,
        alignMinLabel: 'left',
        alignMaxLabel: 'right',
        interval: dates.length > 20 ? Math.floor(dates.length / 10) : dates.length > 10 ? 1 : 0,
        formatter: (val: string) => val.slice(5).replace('-', '/'), // show MM/DD
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
        name: '严重',
        type: 'line',
        smooth: 0.3,
        smoothMonotone: 'x',
        clip: true,
        symbol: 'circle',
        symbolSize: 5,
        showSymbol: false,
        data: series.critical,
        itemStyle: { color: '#ef4444' },
        lineStyle: { color: '#ef4444', width: 2.5 },
        areaStyle: mkArea('rgb(239,68,68)', 0.2, 0),
        emphasis: { focus: 'series', itemStyle: { borderWidth: 2 } },
      },
      {
        name: '高危',
        type: 'line',
        smooth: 0.3,
        smoothMonotone: 'x',
        clip: true,
        symbol: 'circle',
        symbolSize: 5,
        showSymbol: false,
        data: series.high,
        itemStyle: { color: '#f97316' },
        lineStyle: { color: '#f97316', width: 2.5 },
        areaStyle: mkArea('rgb(249,115,22)', 0.18, 0),
        emphasis: { focus: 'series', itemStyle: { borderWidth: 2 } },
      },
      {
        name: '中危',
        type: 'line',
        smooth: 0.3,
        smoothMonotone: 'x',
        clip: true,
        symbol: 'circle',
        symbolSize: 5,
        showSymbol: false,
        data: series.medium,
        itemStyle: { color: '#eab308' },
        lineStyle: { color: '#eab308', width: 2.5 },
        areaStyle: mkArea('rgb(234,179,8)', 0.15, 0),
        emphasis: { focus: 'series', itemStyle: { borderWidth: 2 } },
      },
      {
        name: '低危',
        type: 'line',
        smooth: 0.3,
        smoothMonotone: 'x',
        clip: true,
        symbol: 'circle',
        symbolSize: 5,
        showSymbol: false,
        data: series.low,
        itemStyle: { color: '#3b82f6' },
        lineStyle: { color: '#3b82f6', width: 2.5 },
        areaStyle: mkArea('rgb(59,130,246)', 0.15, 0),
        emphasis: { focus: 'series', itemStyle: { borderWidth: 2 } },
      },
    ],
    animation: true,
    animationDuration: 800,
    animationEasing: 'cubicOut',
  }

  return (
    <div>
      {tabBar}
      <div className="w-full min-w-0 overflow-hidden" style={{ height: 272 }}>
        <ReactECharts
          option={option}
          style={{ height: '100%', width: '100%' }}
          opts={{ renderer: 'svg' }}
        />
      </div>
    </div>
  )
}

