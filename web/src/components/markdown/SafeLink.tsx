import type { ReactNode } from 'react'

import { cn } from '@/utils'

import { isAllowedLink, isExternalLink } from './markdown'

export interface SafeLinkProps {
  href?: string
  children: ReactNode
  className?: string
}

const linkClassName = 'break-words text-primary-600 underline decoration-primary-300 underline-offset-2 hover:text-primary-700 [overflow-wrap:anywhere] dark:text-primary-400 dark:hover:text-primary-300'

export default function SafeLink({ href, children, className }: SafeLinkProps) {
  if (href === undefined || !isAllowedLink(href)) {
    return <span className={cn('break-words [overflow-wrap:anywhere]', className)}>{children}</span>
  }

  if (isExternalLink(href)) {
    return (
      <a
        href={href}
        target="_blank"
        rel="noopener noreferrer"
        className={cn(linkClassName, className)}
      >
        {children}
      </a>
    )
  }

  return <a href={href} className={cn(linkClassName, className)}>{children}</a>
}
