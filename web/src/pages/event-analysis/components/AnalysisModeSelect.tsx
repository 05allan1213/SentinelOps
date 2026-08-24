import { useState, useRef, useEffect } from 'react'
import { Clock, Crosshair, ChevronDown } from 'lucide-react'
import { cn } from '@/utils'

export type AnalysisMode = 'latest' | 'specific'

interface Props {
  value: AnalysisMode
  onChange: (mode: AnalysisMode) => void
  disabled?: boolean
  selectedCount?: number
}

const options = [
  { value: 'latest' as const, label: '最近10条', icon: Clock, desc: '按时间倒序' },
  { value: 'specific' as const, label: '指定事件', icon: Crosshair, desc: '手动选择事件' },
]

export default function AnalysisModeSelect({ value, onChange, disabled, selectedCount }: Props) {
  const [open, setOpen] = useState(false)
  const ref = useRef<HTMLDivElement>(null)

  useEffect(() => {
    const handler = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false)
    }
    document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [])

  const current = options.find(o => o.value === value) || options[0]
  const Icon = current.icon

  return (
    <div ref={ref} className="relative">
      <button
        onClick={() => !disabled && setOpen(!open)}
        disabled={disabled}
        className={cn(
          'flex items-center gap-2 px-4 py-2.5 rounded-lg text-sm font-medium transition-all',
          'border border-gray-200 dark:border-[#30363D] bg-white dark:bg-[#161B22] text-gray-700 dark:text-[#C9D1D9] hover:border-gray-300 dark:hover:border-[#8B949E]',
          disabled && 'opacity-50 cursor-not-allowed',
        )}
      >
        <Icon className="w-4 h-4 text-[#00F0E0]" />
        <span>{current.label}</span>
        {value === 'specific' && selectedCount !== undefined && selectedCount > 0 && (
          <span className="px-1.5 py-0.5 rounded bg-[#00F0E0]/15 text-[#00F0E0] text-xs font-mono">
            {selectedCount}
          </span>
        )}
        <ChevronDown className={cn('w-3.5 h-3.5 text-gray-500 dark:text-[#8B949E] transition-transform', open && 'rotate-180')} />
      </button>

      {open && (
        <div className="absolute top-full left-0 mt-1 w-52 rounded-xl border border-gray-200 dark:border-[#30363D] bg-white dark:bg-[#161B22] shadow-xl z-50 overflow-hidden">
          {options.map(opt => {
            const OptIcon = opt.icon
            const active = opt.value === value
            return (
              <button
                key={opt.value}
                onClick={() => { onChange(opt.value); setOpen(false) }}
                className={cn(
                  'w-full flex items-center gap-2.5 px-4 py-3 text-left transition-colors',
                  active ? 'bg-[#00F0E0]/10 text-[#00F0E0]' : 'text-gray-700 dark:text-[#C9D1D9] hover:bg-gray-100 dark:hover:bg-[#21262D]',
                )}
              >
                <OptIcon className="w-4 h-4 shrink-0" />
                <div className="flex flex-col">
                  <span className="text-sm font-medium">{opt.label}</span>
                  <span className="text-xs text-gray-500 dark:text-[#8B949E]">{opt.desc}</span>
                </div>
              </button>
            )
          })}
        </div>
      )}
    </div>
  )
}
