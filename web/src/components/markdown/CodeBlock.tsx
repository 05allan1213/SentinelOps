import { Check, Copy } from 'lucide-react'
import { useState } from 'react'

import { cn } from '@/utils'

import { highlightSkipReason, normalizeLanguage } from './markdown'

export interface CodeBlockProps {
  code: string
  language?: string
  streaming?: boolean
}

export default function CodeBlock({ code, language, streaming = false }: CodeBlockProps) {
  const [copied, setCopied] = useState(false)
  const normalizedLanguage = normalizeLanguage(language)
  const skipReason = highlightSkipReason(code, language, streaming)

  const copyCode = async () => {
    try {
      await navigator.clipboard.writeText(code)
      setCopied(true)
      window.setTimeout(() => setCopied(false), 1500)
    } catch {
      setCopied(false)
    }
  }

  return (
    <div
      data-testid="code-block"
      data-language={normalizedLanguage}
      data-highlight-skipped={skipReason}
      className="my-3 min-w-0 max-w-full overflow-hidden rounded-md border border-gray-200 bg-gray-950 text-gray-100 dark:border-gray-700"
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
          <span>{copied ? '已复制' : '复制'}</span>
        </button>
      </div>
      <pre className="max-w-full overflow-x-auto p-4 text-sm leading-6">
        <code className="font-mono">{code}</code>
      </pre>
    </div>
  )
}
