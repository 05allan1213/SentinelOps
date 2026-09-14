import type { ReactNode } from 'react'
import type { LucideIcon } from 'lucide-react'
import { cn } from '@/utils'

export interface PageHeaderProps {
  title: ReactNode
  subtitle?: ReactNode
  icon?: LucideIcon
  actions?: ReactNode
  breadcrumb?: ReactNode
  className?: string
}

export default function PageHeader({ title, subtitle, icon: Icon, actions, breadcrumb, className }: PageHeaderProps) {
  return <header className={cn('min-w-0 space-y-3', className)}>
    {breadcrumb}
    <div className="flex min-w-0 flex-wrap items-start justify-between gap-4">
      <div className="flex min-w-0 flex-1 basis-64 items-start gap-3">
        {Icon && <Icon aria-hidden="true" className="mt-1 h-6 w-6 shrink-0 text-primary-600" />}
        <div className="min-w-0">
          <h1 className="text-2xl font-semibold leading-8 text-gray-900 [overflow-wrap:anywhere]">{title}</h1>
          {subtitle && <div className="mt-1 text-sm leading-[22px] text-gray-500 [overflow-wrap:anywhere]">{subtitle}</div>}
        </div>
      </div>
      {actions && <div className="flex max-w-full flex-wrap items-center gap-2">{actions}</div>}
    </div>
  </header>
}
