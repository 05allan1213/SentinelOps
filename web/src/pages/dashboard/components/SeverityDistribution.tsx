import { useState, useEffect } from 'react'
import ReactECharts from 'echarts-for-react'
import { Loader2 } from 'lucide-react'
import { eventService } from '@/services/event'

interface SeverityCount {
  critical: number
  high: number
  medium: number
  low: number
  info: number
}

export default function SeverityDistribution({ refreshKey }: { refreshKey?: number }) {
  const [loading, setLoading] = useState(true)
  const [counts, setCounts] = useState<SeverityCount>({
    critical: 0,
    high: 0,
    medium: 0,
    low: 0,
    info: 0,
  })

  useEffect(() => {
    const fetchData = async () => {
      setLoading(true)
      try {
        const stats = await eventService.getStats()
        const bySeverity = stats.by_severity
        setCounts({
          critical: bySeverity['critical'] || 0,
          high: bySeverity['high'] || 0,
          medium: bySeverity['medium'] || 0,
          low: bySeverity['low'] || 0,
          info: bySeverity['info'] || 0,
        })
      } catch (error) {
        console.error('获取严重级别分布失败:', error)
      } finally {
        setLoading(false)
      }
    }
    fetchData()
  }, [refreshKey])

  if (loading) {
    return (
      <div className="flex items-center justify-center py-8">
        <Loader2 className="w-6 h-6 animate-spin text-primary-500" />
      </div>
    )
  }

  const total = counts.critical + counts.high + counts.medium + counts.low

  const chartData = [
    { value: counts.critical, name: '严重', itemStyle: { color: '#ef4444' } },
    { value: counts.high, name: '高危', itemStyle: { color: '#f97316' } },
    { value: counts.medium, name: '中危', itemStyle: { color: '#eab308' } },
    { value: counts.low, name: '低危', itemStyle: { color: '#3b82f6' } },
  ]

  const option = {
    backgroundColor: 'transparent',
    tooltip: {
      trigger: 'item',
      formatter: '{b}: {c} ({d}%)',
      backgroundColor: '#1e293b',
      borderColor: '#334155',
      borderWidth: 1,
      textStyle: { color: '#f1f5f9', fontSize: 13 },
      extraCssText: 'box-shadow: 0 4px 12px rgba(0,0,0,0.3);',
    },
    graphic: [
      {
        type: 'text',
        left: 'center',
        top: '32%',
        style: {
          text: total.toLocaleString(),
          fontSize: 26,
          fontWeight: 'bold',
          fill: '#111827',
          textAlign: 'center',
        },
      },
      {
        type: 'text',
        left: 'center',
        top: '50%',
        style: {
          text: '事件总数',
          fontSize: 11,
          fill: '#9ca3af',
          textAlign: 'center',
        },
      },
    ],
    series: [
      {
        type: 'pie',
        radius: ['52%', '76%'],
        center: ['50%', '44%'],
        avoidLabelOverlap: false,
        itemStyle: {
          borderRadius: 6,
          borderColor: '#fff',
          borderWidth: 2,
        },
        label: { show: false },
        emphasis: {
          label: { show: true, fontSize: 13, fontWeight: 'bold', color: '#111827', formatter: '{b}\n{c}' },
          itemStyle: { shadowBlur: 16, shadowColor: 'rgba(0,0,0,0.15)' },
          scale: true,
          scaleSize: 6,
        },
        labelLine: { show: false },
        data: chartData,
        animationType: 'scale',
        animationEasing: 'elasticOut',
      },
    ],
  }

  const legendData = [
    { label: '严重', value: counts.critical, color: 'bg-red-500' },
    { label: '高危', value: counts.high, color: 'bg-orange-500' },
    { label: '中危', value: counts.medium, color: 'bg-yellow-400' },
    { label: '低危', value: counts.low, color: 'bg-blue-500' },
  ]

  return (
    <div>
      <ReactECharts option={option} style={{ height: '220px' }} opts={{ renderer: 'svg' }} />
      <div className="grid grid-cols-2 gap-x-4 gap-y-2 mt-3 px-2">
        {legendData.map((item) => (
          <div key={item.label} className="flex items-center gap-2">
            <div className={`w-2.5 h-2.5 rounded-full flex-shrink-0 ${item.color}`} />
            <span className="text-sm text-gray-600">{item.label}</span>
            <span className="text-sm font-semibold text-gray-900 ml-auto">{item.value}</span>
          </div>
        ))}
      </div>
    </div>
  )
}

