import type { ReactNode } from 'react'
import { cn } from '@/utils'

export interface CardProps {
  children: ReactNode
  header?: ReactNode
  footer?: ReactNode
  className?: string
  testId?: string
}

export default function Card({ children, header, footer, className, testId }: CardProps) {
  return <div data-testid={testId} className={cn('min-w-0 rounded-xl border border-[#E5E7EB] bg-white shadow-[0_1px_3px_rgba(0,0,0,0.04)]', className)}>
    {header && <div className="border-b border-[#E5E7EB] px-5 py-4 text-lg font-semibold leading-6 text-gray-900">{header}</div>}
    <div className="min-w-0 p-5 [overflow-wrap:anywhere]">{children}</div>
    {footer && <div className="rounded-b-xl border-t border-[#E5E7EB] px-5 py-4">{footer}</div>}
  </div>
}
