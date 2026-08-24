import { Copy, Search } from 'lucide-react'
import { cn } from '@/utils'
import toast from 'react-hot-toast'

interface Props {
  label: string
  value: string
  status?: 'critical' | 'high' | 'medium' | 'low'
  onSearch?: (value: string) => void
}

const statusColors = {
  critical: 'border-t-[#F85149]',
  high: 'border-t-[#F0883E]',
  medium: 'border-t-[#E3B341]',
  low: 'border-t-[#3FB950]',
}

export default function DataBlock({ label, value, status, onSearch }: Props) {
  const copy = () => {
    navigator.clipboard.writeText(value)
    toast.success('已复制')
  }

  return (
    <div className={cn(
      'bg-[#0D1117] border border-[#30363D] rounded p-2 group',
      status && 'border-t-2',
      status && statusColors[status]
    )}>
      <div className="text-[10px] text-[#8B949E] mb-1">{label}</div>
      <div className="flex items-center justify-between">
        <span className="font-mono text-sm text-[#E6EDF3] truncate">{value}</span>
        <div className="flex gap-1 opacity-0 group-hover:opacity-100 transition-opacity">
          <button onClick={copy} className="p-1 hover:bg-[#30363D] rounded">
            <Copy className="w-3 h-3 text-[#8B949E]" />
          </button>
          {onSearch && (
            <button onClick={() => onSearch(value)} className="p-1 hover:bg-[#30363D] rounded">
              <Search className="w-3 h-3 text-[#8B949E]" />
            </button>
          )}
        </div>
      </div>
    </div>
  )
}
