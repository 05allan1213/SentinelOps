import { cn } from '@/utils'
import { runtimePhaseLabel, runtimeStatusMeta } from './runtimeStatus'

interface Props {
  status?: string
  phase?: string
  className?: string
}

export default function RuntimeStatusBadge({ status, phase, className }: Props) {
  const meta = runtimeStatusMeta(status)
  return (
    <span className={cn('inline-flex items-center gap-2 align-middle', className)}>
      <span
        data-testid="runtime-status-badge"
        data-status={status ?? 'unknown'}
        data-tone={meta.tone}
        className={cn('inline-flex items-center h-6 px-2 rounded-md border text-xs font-medium whitespace-nowrap', meta.className)}
      >
        {meta.label}
      </span>
      {phase !== undefined && (
        <span data-testid="runtime-phase" className="text-xs text-gray-500 whitespace-nowrap">
          {runtimePhaseLabel(phase)}
        </span>
      )}
    </span>
  )
}
