import { cn } from '@/utils'

interface Props {
  confidence: number
  showLabel?: boolean
}

export default function ConfidenceBadge({ confidence, showLabel = true }: Props) {
  const isLow = confidence < 0.8

  return (
    <div className={cn(
      'inline-flex items-center gap-1.5 px-2 py-0.5 rounded text-xs font-mono',
      isLow ? 'bg-[#E3B341]/20 text-[#E3B341] animate-pulse' : 'bg-[#3FB950]/20 text-[#3FB950]'
    )}>
      <span>{(confidence * 100).toFixed(0)}%</span>
      {showLabel && isLow && <span className="text-[10px]">建议专家介入</span>}
    </div>
  )
}
