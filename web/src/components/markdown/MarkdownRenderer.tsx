import { Children, Component, isValidElement, type ReactNode } from 'react'
import ReactMarkdown, { type Components } from 'react-markdown'
import remarkGfm from 'remark-gfm'
import rehypeHighlight from 'rehype-highlight'
import type { Element, Root } from 'hast'
import type { PluggableList } from 'unified'
import type { VFile } from 'vfile'
import typescript from 'highlight.js/lib/languages/typescript'
import javascript from 'highlight.js/lib/languages/javascript'
import json from 'highlight.js/lib/languages/json'
import go from 'highlight.js/lib/languages/go'
import python from 'highlight.js/lib/languages/python'
import sql from 'highlight.js/lib/languages/sql'
import shell from 'highlight.js/lib/languages/shell'
import bash from 'highlight.js/lib/languages/bash'
import yaml from 'highlight.js/lib/languages/yaml'
import markdown from 'highlight.js/lib/languages/markdown'
import css from 'highlight.js/lib/languages/css'
import xml from 'highlight.js/lib/languages/xml'

import { cn } from '@/utils'
import CodeBlock from './CodeBlock'
import SafeImage from './SafeImage'
import SafeLink from './SafeLink'
import { highlightSkipReason, normalizeLanguage, streamingMarkdown } from './markdown'

declare module 'hast' {
  interface ElementData {
    markdownCode?: { raw: string; language?: string; streaming: boolean }
  }
}

export interface MarkdownRendererProps {
  content: string
  variant?: 'chat' | 'thinking' | 'event' | 'analysis' | 'report' | 'runtime'
  streaming?: boolean
  complete?: boolean
  className?: string
}

// rehype-highlight constructs one restricted lowlight instance here. Neither
// auto-detection nor the package's common/all grammar registries are used.
const highlight = rehypeHighlight({
  languages: { typescript, javascript, json, go, python, sql, shell, bash, yaml, markdown, css, html: xml, xml },
  detect: false,
})

function originalCode(node: Element, rendered: string, file: VFile): string {
  const start = node.position?.start.offset
  const end = node.position?.end.offset
  if (start === undefined || end === undefined) return rendered
  const source = String(file.value).slice(start, end)
  const opening = /^(`{3,}|~{3,})[^\r\n]*(?:\r\n|\r|\n|$)/u.exec(source)
  if (opening === null) return rendered

  // mdast-to-hast appends one synthetic LF. Keep parsed indentation/content,
  // but recover the actual terminal separator for copying fenced source.
  const value = rendered.endsWith('\n') ? rendered.slice(0, -1) : rendered
  const lastLine = /(\r\n|\r|\n)([^\r\n]*)$/u.exec(source)
  if (lastLine === null || source.length === opening[0].length) return value
  if (lastLine[2] === '') return value + lastLine[1]

  // The existing parser omits a real closing line from code content, while
  // retaining fence-like literals (including their indentation/quote prefix).
  // Compare line counts, not a second approximation of CommonMark's closers.
  const sourceBreaks = node.position!.end.line - node.position!.start.line
  const contentBreaks = value.split(/\r\n|\r|\n/u).length - 1
  return sourceBreaks > contentBreaks + 1 ? value + lastLine[1] : value
}

function boundedHighlight(options: { streaming: boolean }) {
  return (tree: Root, file: VFile) => {
    const walk = (parent: Root | Element) => {
      for (const node of parent.children) {
        if (node.type !== 'element') continue
        if (parent.type === 'element' && parent.tagName === 'pre' && node.tagName === 'code') {
          const raw = node.children.map((child) => child.type === 'text' ? child.value : '').join('')
          const languageClass = (node.properties.className as string[] | undefined)?.find((name) => name.startsWith('language-'))
          const language = normalizeLanguage(languageClass)
          node.data = { ...node.data, markdownCode: { raw: originalCode(node, raw, file), language, streaming: options.streaming } }
          // Check eligibility before doing any highlighter work, not after render.
          if (highlightSkipReason(raw, language, options.streaming) !== undefined) continue
          const originalChildren = node.children
          const originalProperties = node.properties
          node.properties = { ...originalProperties, className: [`language-${language}`] }
          try {
            highlight({ type: 'root', children: [{ ...parent, children: [node] }] }, file)
          } catch {
            node.children = originalChildren
            node.properties = originalProperties
          }
        } else {
          walk(node)
        }
      }
    }
    walk(tree)
  }
}

const plugins = {
  remark: [remarkGfm] as PluggableList,
  rehype: [[boundedHighlight, { streaming: false }]] as PluggableList,
  streaming: [[boundedHighlight, { streaming: true }]] as PluggableList,
}

const components: Components = {
  a: ({ href, children }) => <SafeLink href={href}>{children}</SafeLink>,
  img: ({ src, alt }) => <SafeImage src={src} alt={alt} />,
  pre: ({ node, children }) => {
    const code = node?.children.find((child): child is Element => child.type === 'element' && child.tagName === 'code')
    const data = code?.data?.markdownCode
    const element = Children.toArray(children).find(isValidElement)
    return <CodeBlock code={typeof data?.raw === 'string' ? data.raw : ''} language={typeof data?.language === 'string' ? data.language : undefined} streaming={data?.streaming === true}>
      {isValidElement<{ children: ReactNode }>(element) ? element.props.children : undefined}
    </CodeBlock>
  },
  code: ({ node, children }) => <code className={node?.data?.markdownCode !== undefined ? undefined : 'rounded bg-gray-100 px-1 py-0.5 font-mono text-[0.9em] [overflow-wrap:anywhere] dark:bg-gray-800'}>{children}</code>,
  table: ({ children }) => <div className="my-3 min-w-0 max-w-full overflow-x-auto" data-testid="markdown-table-scroll"><table className="w-full border-collapse text-left text-sm">{children}</table></div>,
  thead: ({ children }) => <thead className="bg-gray-50 dark:bg-gray-800">{children}</thead>,
  tbody: ({ children }) => <tbody>{children}</tbody>,
  tr: ({ children }) => <tr className="border-b border-gray-200 dark:border-gray-700">{children}</tr>,
  th: ({ children, style }) => <th style={style} className="whitespace-nowrap px-3 py-2 font-semibold">{children}</th>,
  td: ({ children, style }) => <td style={style} className="px-3 py-2 align-top">{children}</td>,
  h1: ({ children }) => <h1 className="mb-3 mt-5 text-xl font-semibold">{children}</h1>,
  h2: ({ children }) => <h2 className="mb-2 mt-4 text-lg font-semibold">{children}</h2>,
  h3: ({ children }) => <h3 className="mb-2 mt-3 text-base font-semibold">{children}</h3>,
  h4: ({ children }) => <h4 className="mb-2 mt-3 font-semibold">{children}</h4>,
  h5: ({ children }) => <h5 className="mb-2 mt-3 font-semibold">{children}</h5>,
  h6: ({ children }) => <h6 className="mb-2 mt-3 font-semibold">{children}</h6>,
  p: ({ children }) => <p className="my-2">{children}</p>,
  blockquote: ({ children }) => <blockquote className="my-3 border-l-2 border-primary-300 pl-4 text-gray-500 dark:text-gray-400">{children}</blockquote>,
  ul: ({ children, className }) => <ul className={cn('my-2 list-disc space-y-1 pl-6', className)}>{children}</ul>,
  ol: ({ children, start }) => <ol start={start} className="my-2 list-decimal space-y-1 pl-6">{children}</ol>,
  li: ({ children, className }) => <li className={cn('pl-1', className, className?.includes('task-list-item') && 'list-none')}>{children}</li>,
  input: ({ checked }) => <input type="checkbox" checked={checked} disabled className="mr-2 accent-primary-600" />,
}

// Preserve the original URL for the primitives' stricter policy. ReactMarkdown's
// default transform turns rejected URLs into '', which could become a local link.
const preserveUrl = (url: string) => url
const variants = {
  chat: 'text-sm leading-7', thinking: 'text-xs leading-6', event: 'text-sm leading-6',
  analysis: 'text-sm leading-7', report: 'text-base leading-7', runtime: 'text-xs leading-6',
}
const highlightColors = '[&_.hljs-keyword]:text-primary-300 [&_.hljs-built_in]:text-primary-300 [&_.hljs-string]:text-success-300 [&_.hljs-number]:text-warning-300 [&_.hljs-comment]:text-gray-400 [&_.hljs-title]:text-primary-200 [&_.hljs-attr]:text-warning-200 [&_.hljs-tag]:text-primary-300'

class ParserBoundary extends Component<{ content: string; children: ReactNode }, { failed: boolean; content: string }> {
  state = { failed: false, content: this.props.content }
  static getDerivedStateFromError() { return { failed: true } }
  static getDerivedStateFromProps(props: { content: string }, state: { content: string }) {
    return props.content !== state.content ? { content: props.content, failed: false } : null
  }
  render() {
    return this.state.failed ? <>
      <span role="status" className="text-xs text-gray-500">Markdown 暂时无法解析，显示原文</span>
      <pre className="max-w-full whitespace-pre-wrap break-words [overflow-wrap:anywhere]">{this.props.content}</pre>
    </> : this.props.children
  }
}

export default function MarkdownRenderer({ content, variant = 'chat', streaming = false, complete = !streaming, className }: MarkdownRendererProps) {
  const display = streamingMarkdown(content, complete)
  return <div data-markdown-variant={variant} className={cn('min-w-0 max-w-full text-gray-700 [overflow-wrap:anywhere] dark:text-gray-200', variants[variant], highlightColors, className)}>
    <ParserBoundary content={content}>
      {display.mode === 'plain' ? <pre className="max-w-full whitespace-pre-wrap break-words [overflow-wrap:anywhere]">{content}</pre> : <ReactMarkdown
        remarkPlugins={plugins.remark}
        rehypePlugins={streaming ? plugins.streaming : plugins.rehype}
        components={components}
        urlTransform={preserveUrl}
        skipHtml
      >{content}</ReactMarkdown>}
    </ParserBoundary>
  </div>
}
