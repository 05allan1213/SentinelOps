import type { ReactNode } from 'react'
import { CircleCheck, Info, TriangleAlert } from 'lucide-react'
import { cn } from '@/utils'

export interface AlertProps {
  tone?: 'info' | 'success' | 'warning' | 'danger' | 'neutral'
  children: ReactNode
  action?: ReactNode
  className?: string
}

const tones = {
  info: 'border-primary-100 bg-primary-50 text-primary-800',
  success: 'border-success-100 bg-success-50 text-success-800',
  warning: 'border-warning-200 bg-warning-50 text-warning-800',
  danger: 'border-danger-100 bg-danger-50 text-danger-800',
  neutral: 'border-gray-200 bg-gray-50 text-gray-700',
}

export default function Alert({ tone = 'info', children, action, className }: AlertProps) {
  const Icon = tone === 'success' ? CircleCheck : tone === 'warning' || tone === 'danger' ? TriangleAlert : Info
  return <div role={tone === 'danger' ? 'alert' : 'status'} className={cn('flex min-w-0 flex-wrap items-start gap-3 rounded-lg border px-4 py-3 text-sm leading-[22px]', tones[tone], className)}>
    <Icon aria-hidden="true" className="mt-0.5 h-5 w-5 shrink-0" />
    <div className="min-w-0 flex-1 basis-40 [overflow-wrap:anywhere]">{children}</div>
    {action && <div className="flex max-w-full flex-wrap gap-2">{action}</div>}
  </div>
}
