import { useState, useRef, useEffect } from 'react'
import { ChevronDown, Check } from 'lucide-react'
import { cn } from '@/utils'

export interface SelectOption {
  value: string | number
  label: string
}

interface CustomSelectProps {
  value: string | number
  onChange: (value: string) => void
  options: SelectOption[]
  className?: string
  placeholder?: string
  prefix?: React.ReactNode
}

export default function CustomSelect({
  value,
  onChange,
  options,
  className,
  prefix,
}: CustomSelectProps) {
  const [open, setOpen] = useState(false)
  const ref = useRef<HTMLDivElement>(null)

  const selected = options.find(o => String(o.value) === String(value))

  // 点击外部关闭
  useEffect(() => {
    const handler = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) {
        setOpen(false)
      }
    }
    document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [])

  return (
    <div ref={ref} className={cn('relative select-none', className)}>
      {/* 触发器 */}
      <button
        type="button"
        onClick={() => setOpen(v => !v)}
        className={cn(
          'flex items-center gap-1.5 h-9 rounded-lg border border-gray-200 bg-white',
          'text-sm text-gray-700 transition-all duration-150',
          'focus:outline-none focus:ring-2 focus:ring-indigo-500/30 focus:border-indigo-400',
          'hover:border-gray-300 hover:bg-gray-50',
          prefix ? 'pl-8 pr-2.5' : 'px-3',
          open && 'border-indigo-400 ring-2 ring-indigo-500/30',
        )}
      >
        {prefix && (
          <span className="absolute left-2.5 top-1/2 -translate-y-1/2 pointer-events-none text-gray-400">
            {prefix}
          </span>
        )}
        <span className="flex-1 text-left truncate">{selected?.label ?? '请选择'}</span>
        <ChevronDown
          className={cn(
            'w-3.5 h-3.5 text-gray-400 flex-shrink-0 transition-transform duration-200',
            open && 'rotate-180',
          )}
        />
      </button>

      {/* 下拉列表 */}
      {open && (
        <div className="absolute z-50 mt-1 w-full min-w-[120px] bg-white border border-gray-200 rounded-xl shadow-lg shadow-gray-200/60 overflow-hidden animate-in fade-in slide-in-from-top-1 duration-100">
          {options.map(opt => {
            const isActive = String(opt.value) === String(value)
            return (
              <button
                key={opt.value}
                type="button"
                onClick={() => { onChange(String(opt.value)); setOpen(false) }}
                className={cn(
                  'w-full flex items-center justify-between px-3 py-2 text-sm text-left transition-colors',
                  isActive
                    ? 'bg-indigo-50 text-indigo-700 font-medium'
                    : 'text-gray-700 hover:bg-gray-50',
                )}
              >
                <span>{opt.label}</span>
                {isActive && <Check className="w-3.5 h-3.5 flex-shrink-0 text-indigo-500" />}
              </button>
            )
          })}
        </div>
      )}
    </div>
  )
}
