import type { ComponentProps } from 'react'
import { cn } from '@/utils'

export default function PaginationBar({ className, children, ...props }: ComponentProps<'div'>) {
  return <div {...props} className={cn('min-w-0 shrink-0 rounded-b-xl border-t border-[#E5E7EB] bg-white px-4 py-3', className)}>{children}</div>
}
