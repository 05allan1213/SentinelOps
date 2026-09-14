import { Check, Copy } from 'lucide-react'
import { useEffect, useRef, useState, type ReactNode } from 'react'

import { cn } from '@/utils'

import { highlightSkipReason, normalizeLanguage } from './markdown'

export interface CodeBlockProps {
  code: string
  language?: string
  streaming?: boolean
  children?: ReactNode
}

export default function CodeBlock({ code, language, streaming = false, children }: CodeBlockProps) {
  const [copied, setCopied] = useState(false)
  const [copyError, setCopyError] = useState(false)
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)
  useEffect(() => () => clearTimeout(timer.current), [])
  const normalizedLanguage = normalizeLanguage(language)
  const skipReason = highlightSkipReason(code, language, streaming)

  const copyCode = async () => {
    try {
      await navigator.clipboard.writeText(code)
      setCopyError(false)
      setCopied(true)
      clearTimeout(timer.current)
      timer.current = setTimeout(() => setCopied(false), 1500)
    } catch {
      setCopyError(true)
      setCopied(false)
    }
  }

  return (
    <div
      data-testid="code-block"
      data-language={normalizedLanguage}
      data-highlight-skipped={skipReason}
      className="my-3 min-w-0 max-w-full overflow-hidden rounded-md border border-gray-200 bg-gray-950 text-gray-100"
    >
      <div className="flex min-h-9 items-center justify-between gap-3 border-b border-gray-800 px-3 py-1.5 text-xs text-gray-400">
        <span>{normalizedLanguage}</span>
        <button
          type="button"
          onClick={copyCode}
          aria-label={copied ? '代码已复制' : '复制代码'}
          className={cn(
            'inline-flex items-center gap-1.5 rounded px-2 py-1 text-xs transition-colors',
            copied
              ? 'text-success-400'
              : 'text-gray-400 hover:bg-gray-800 hover:text-gray-100',
          )}
        >
          {copied ? <Check aria-hidden="true" className="h-3.5 w-3.5" /> : <Copy aria-hidden="true" className="h-3.5 w-3.5" />}
          <span aria-live="polite">{copied ? '已复制' : '复制'}</span>
        </button>
      </div>
      {copyError && <p role="status" className="px-3 py-1 text-xs text-amber-200">复制失败，请选择代码手动复制。</p>}
      <pre tabIndex={0} role="region" aria-label="代码内容" className="max-w-full overflow-x-auto p-4 text-sm leading-6">
        <code className="font-mono">{skipReason === undefined ? (children ?? code) : code}</code>
      </pre>
    </div>
  )
}
