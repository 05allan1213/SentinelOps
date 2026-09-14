import type { ReactNode } from 'react'
import { Ban, CircleDashed, Clock, Info, Loader2, TriangleAlert } from 'lucide-react'

export interface StatePanelProps {
  kind: 'loading' | 'empty' | 'error' | 'unavailable' | 'not-run' | 'partial'
  title: ReactNode
  description?: ReactNode
  action?: ReactNode
  technicalDetail?: ReactNode
}

const presentation = {
  loading: { Icon: Loader2, color: 'text-gray-500' },
  empty: { Icon: CircleDashed, color: 'text-gray-500' },
  error: { Icon: TriangleAlert, color: 'text-danger-600' },
  unavailable: { Icon: Ban, color: 'text-warning-700' },
  'not-run': { Icon: Clock, color: 'text-gray-500' },
  partial: { Icon: Info, color: 'text-warning-700' },
}

export default function StatePanel({ kind, title, description, action, technicalDetail }: StatePanelProps) {
  const { Icon, color } = presentation[kind]
  return <section data-state-kind={kind} className="flex min-w-0 items-start gap-3 py-5">
    <Icon aria-hidden="true" className={`mt-0.5 h-5 w-5 shrink-0 ${color} ${kind === 'loading' ? 'animate-spin motion-reduce:animate-none' : ''}`} />
    <div className="min-w-0 flex-1 [overflow-wrap:anywhere]">
      <div role={kind === 'error' ? 'alert' : 'status'}>
        <h2 className="text-lg font-semibold leading-6 text-gray-900">{title}</h2>
        {description && <div className="mt-1 text-sm leading-[22px] text-gray-600">{description}</div>}
      </div>
      {technicalDetail && <details className="mt-2 text-sm text-gray-600">
        <summary className="w-fit cursor-pointer rounded focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 focus-visible:ring-offset-2">技术详情</summary>
        <div className="mt-2 whitespace-pre-wrap font-mono text-xs leading-5">{technicalDetail}</div>
      </details>}
      {action && <div className="mt-3 flex flex-wrap gap-2">{action}</div>}
    </div>
  </section>
}
