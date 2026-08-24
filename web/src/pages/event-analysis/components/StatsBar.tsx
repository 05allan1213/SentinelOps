import { useEffect, useState } from 'react'
import { Shield, AlertTriangle, Activity, TrendingUp } from 'lucide-react'
import { cn } from '@/utils'

interface Props {
  data: {
    maxCVSS: number
    count: number
    avgRisk: number
    critical?: number
    highRisk?: number
  } | null
  isProcessing: boolean
}

function AnimatedNumber({ value, decimals = 0, color }: { value: number; decimals?: number; color: string }) {
  const [display, setDisplay] = useState(0)

  useEffect(() => {
    if (value === 0) { setDisplay(0); return }
    const duration = 800
    const start = performance.now()
    const from = 0

    const tick = (now: number) => {
      const elapsed = now - start
      const progress = Math.min(elapsed / duration, 1)
      const eased = 1 - Math.pow(1 - progress, 3)
      setDisplay(from + (value - from) * eased)
      if (progress < 1) requestAnimationFrame(tick)
    }
    requestAnimationFrame(tick)
  }, [value])

  return (
    <span className="tabular-nums font-mono" style={{ color }}>
      {decimals > 0 ? display.toFixed(decimals) : Math.round(display)}
    </span>
  )
}

export default function StatsBar({ data, isProcessing }: Props) {
  const stats = [
    {
      label: '最高CVSS',
      value: data?.maxCVSS ?? 0,
      decimals: 1,
      icon: AlertTriangle,
      color: '#F43F5E',
      glow: data != null && data.maxCVSS >= 9,
    },
    {
      label: '严重事件',
      value: data?.critical ?? 0,
      icon: Shield,
      color: '#F43F5E',
    },
    {
      label: '高危事件',
      value: data?.highRisk ?? 0,
      icon: Activity,
      color: '#F97316',
    },
    {
      label: '分析总数',
      value: data?.count ?? 0,
      icon: TrendingUp,
      color: '#00F0E0',
    },
  ]

  return (
    <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
      {stats.map((stat, idx) => {
        const Icon = stat.icon
        const hasValue = data != null
        return (
          <div
            key={idx}
            className={cn(
              'relative p-4 rounded-2xl border transition-all duration-300 overflow-hidden',
              'bg-white dark:from-[#0D1117] dark:via-[#0D1117] dark:to-[#161B22]',
              'border-gray-200 dark:border-[#30363D]/60 shadow-sm hover:shadow-md',
              stat.glow && 'border-red-200 dark:border-[#EF4444]/40 shadow-red-100 dark:shadow-[#EF4444]/20'
            )}
          >
            {/* 背景渐变装饰 */}
            <div
              className="absolute inset-0 opacity-[0.03]"
              style={{
                background: `radial-gradient(circle at 0% 0%, ${stat.color}, transparent 70%)`,
              }}
            />

            {/* 严重时的脉冲背景 */}
            {stat.glow && (
              <div className="absolute inset-0 rounded-2xl bg-[#EF4444]/5 animate-pulse" />
            )}

            <div className="relative flex items-center gap-4">
              <div
                className="w-10 h-10 rounded-xl flex items-center justify-center shrink-0 relative overflow-hidden"
                style={{
                  background: `linear-gradient(135deg, ${stat.color}18, ${stat.color}0d)`,
                }}
              >
                <Icon className="w-5 h-5 relative z-10" style={{ color: stat.color }} />
              </div>
              <div>
                <div className="text-2xl font-bold leading-tight">
                  {hasValue ? (
                    <AnimatedNumber
                      value={stat.value}
                      decimals={stat.decimals || 0}
                      color={stat.color}
                    />
                  ) : (
                    <span className={cn(
                      'text-gray-300 dark:text-[#30363D]',
                      isProcessing && 'animate-pulse'
                    )}>--</span>
                  )}
                </div>
                <div className="text-xs text-gray-600 dark:text-[#8B949E] tracking-wide mt-1 font-medium">
                  {stat.label}
                </div>
              </div>
            </div>
          </div>
        )
      })}
    </div>
  )
}
