import { cn } from '@/utils'

interface Props {
  availability?: string
  dataQuality?: string
  reasonCode?: string
  notRun?: boolean
  className?: string
}

const AVAILABILITY: Record<string, { label: string; className: string }> = {
  available: { label: '可用', className: 'bg-emerald-50 text-emerald-700 border-emerald-200' },
  partial: { label: '部分可用', className: 'bg-amber-50 text-amber-800 border-amber-200' },
  unavailable: { label: '不可用', className: 'bg-gray-100 text-gray-600 border-gray-300' },
}

const QUALITY: Record<string, { label: string; className: string }> = {
  complete: { label: '数据完整', className: 'bg-emerald-50 text-emerald-700 border-emerald-200' },
  reconstructed: { label: '数据重建', className: 'bg-blue-50 text-blue-700 border-blue-200' },
  partial: { label: '数据不完整', className: 'bg-amber-50 text-amber-800 border-amber-200' },
  unknown: { label: '质量未知', className: 'bg-gray-100 text-gray-600 border-gray-300' },
}

/** Explicit server metadata chips: availability, data quality, reason and independent not_run evidence. */
export default function RuntimeQualityState({ availability, dataQuality, reasonCode, notRun, className }: Props) {
  const availabilityMeta = availability ? AVAILABILITY[availability] : undefined
  const qualityMeta = dataQuality ? QUALITY[dataQuality] : undefined
  if (!availabilityMeta && !qualityMeta && !reasonCode && !notRun) return null

  return (
    <div data-testid="runtime-quality" className={cn('flex flex-wrap items-center gap-2 text-xs', className)}>
      {availabilityMeta && (
        <span data-testid="runtime-quality-availability" data-availability={availability} className={cn('inline-flex items-center h-6 px-2 rounded-md border', availabilityMeta.className)}>
          {availabilityMeta.label}
        </span>
      )}
      {qualityMeta && (
        <span data-testid="runtime-quality-data" data-quality={dataQuality} className={cn('inline-flex items-center h-6 px-2 rounded-md border', qualityMeta.className)}>
          {qualityMeta.label}
        </span>
      )}
      {reasonCode && (
        <span data-testid="runtime-quality-reason" className="inline-flex min-w-0 max-w-full items-center min-h-6 px-2 py-0.5 rounded-md border border-gray-200 bg-white font-mono text-gray-600 [overflow-wrap:anywhere]">
          reason: {reasonCode}
        </span>
      )}
      {notRun && (
        <span data-testid="runtime-quality-not-run" className="inline-flex items-center h-6 px-2 rounded-md border border-dashed border-gray-300 bg-white text-gray-500">
          未执行
        </span>
      )}
    </div>
  )
}
