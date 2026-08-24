import type { LucideIcon } from 'lucide-react'
import type { ReactNode } from 'react'
import { cn } from '@/utils'

const toneMap = {
  blue:    { wrap: 'bg-blue-50 border-blue-100',    icon: 'bg-blue-100 text-blue-600',     val: 'text-blue-700' },
  emerald: { wrap: 'bg-emerald-50 border-emerald-100', icon: 'bg-emerald-100 text-emerald-600', val: 'text-emerald-700' },
  amber:   { wrap: 'bg-amber-50 border-amber-100',   icon: 'bg-amber-100 text-amber-600',   val: 'text-amber-700' },
  red:     { wrap: 'bg-red-50 border-red-100',       icon: 'bg-red-100 text-red-600',       val: 'text-red-700' },
  gray:    { wrap: 'bg-gray-50 border-gray-100',     icon: 'bg-gray-100 text-gray-500',     val: 'text-gray-700' },
  indigo:  { wrap: 'bg-indigo-50 border-indigo-100', icon: 'bg-indigo-100 text-indigo-600', val: 'text-indigo-700' },
}

interface StatCardProps {
  label: string
  value: number | string
  Icon: LucideIcon
  tone?: keyof typeof toneMap
  sub?: string | ReactNode
}

export default function StatCard({ label, value, Icon, tone = 'gray', sub }: StatCardProps) {
  const cls = toneMap[tone]
  return (
    <div className={cn('rounded-xl border p-4 flex items-center gap-3 transition-shadow hover:shadow-sm', cls.wrap)}>
      <div className={cn('w-10 h-10 rounded-xl flex items-center justify-center flex-shrink-0', cls.icon)}>
        <Icon className="w-5 h-5" />
      </div>
      <div className="min-w-0 flex-1">
        <p className={cn('text-2xl font-bold leading-none tabular-nums', cls.val)}>{value}</p>
        <p className="text-xs text-gray-500 mt-1 truncate">{label}</p>
        {sub && <div className="text-xs text-gray-400 mt-0.5">{sub}</div>}
      </div>
    </div>
  )
}
