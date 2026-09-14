import type { ComponentProps, ReactNode } from 'react'
import { Loader2 } from 'lucide-react'
import { cn } from '@/utils'

type Variant = 'primary' | 'secondary' | 'danger' | 'ghost' | 'icon'
export type ButtonProps = ComponentProps<'button'> & {
  size?: 'standard' | 'compact'
  loading?: boolean
  icon?: ReactNode
} & ({ variant: 'icon'; 'aria-label': string } | { variant?: Exclude<Variant, 'icon'> })

const variants: Record<Variant, string> = {
  primary: 'border-transparent bg-primary-500 text-white hover:bg-primary-600',
  secondary: 'border-gray-300 bg-white text-gray-700 hover:bg-gray-50',
  danger: 'border-transparent bg-danger-600 text-white hover:bg-danger-700',
  ghost: 'border-transparent bg-transparent text-gray-700 hover:bg-gray-100',
  icon: 'border-transparent bg-transparent text-gray-700 hover:bg-gray-100',
}

export default function Button({ variant = 'secondary', size = 'standard', loading = false, disabled, icon, children, className, type = 'button', title, ...props }: ButtonProps) {
  return <button {...props} type={type} disabled={disabled || loading} aria-busy={loading || undefined}
    title={title ?? props['aria-label']}
    className={cn('relative inline-flex shrink-0 items-center justify-center gap-2 rounded-lg border text-sm font-medium leading-[22px] transition-colors duration-[120ms] ease-out focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 focus-visible:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-50 motion-reduce:transition-none motion-reduce:duration-0',
      variants[variant], size === 'standard' ? 'h-9' : 'h-8', variant === 'icon' ? (size === 'standard' ? 'w-9 p-0' : 'w-8 p-0') : 'px-3', className)}>
    <span className={cn('inline-flex min-w-0 items-center justify-center gap-2', loading && 'opacity-0')}>
      {icon && <span aria-hidden="true" className="flex h-4 w-4 shrink-0 items-center justify-center [&>svg]:h-4 [&>svg]:w-4">{icon}</span>}
      {children}
    </span>
    {loading && <Loader2 aria-hidden="true" className="absolute h-4 w-4 animate-spin motion-reduce:animate-none" />}
  </button>
}
