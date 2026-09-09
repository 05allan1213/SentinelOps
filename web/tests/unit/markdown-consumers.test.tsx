import { readdirSync, readFileSync, statSync } from 'node:fs'
import { join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { render, screen } from '@testing-library/react'

import { MarkdownRenderer } from '@/components/markdown'
import { normalizeMarkdown } from '@/utils'

const consumerContracts = [
  {
    path: '../../src/pages/chat/index.tsx',
    calls: 4,
    variants: { chat: 2, thinking: 2 },
  },
  {
    path: '../../src/pages/events/components/EventDetailModal.tsx',
    calls: 1,
    variants: { event: 1 },
  },
  {
    path: '../../src/pages/event-analysis/components/ResultPanel.tsx',
    calls: 1,
    variants: { analysis: 1 },
  },
  {
    path: '../../src/pages/reports/components/ReportDetailModal.tsx',
    calls: 1,
    variants: { report: 1 },
  },
  {
    path: '../../src/components/report/ReportViewer.tsx',
    calls: 1,
    variants: { report: 1 },
  },
] as const

function source(relativePath: string): string {
  return readFileSync(fileURLToPath(new URL(relativePath, import.meta.url)), 'utf8')
}

function sourceFiles(directory: string): string[] {
  return readdirSync(directory).flatMap((entry) => {
    const path = join(directory, entry)
    return statSync(path).isDirectory() ? sourceFiles(path) : /\.[jt]sx?$/u.test(entry) ? [path] : []
  })
}

describe('normalizeMarkdown compatibility boundary', () => {
  it.each([
    '#合法标题\n\n正文',
    '正文内的 ### 标记保持原样',
    '- 列表一\n  - 嵌套列表\n\n1. 有序列表',
    '```markdown\n# code heading\n\n\n- code list\n```',
    '| A | B |\n| --- | --- |\n| 1 | 2 |',
    '# 标题\n\n\n\n保留合法的多个空行',
    '```ts\nconst unfinished = true',
    '<script>alert("unsafe")</script>\n\n<strong>raw html</strong>',
  ])('preserves valid, malformed or dangerous source text: %j', (markdown) => {
    expect(normalizeMarkdown(markdown)).toBe(markdown)
  })

  it('preserves mixed transport newlines and keeps empty input safe', () => {
    expect(normalizeMarkdown('第一行\r\n第二行\r第三行')).toBe('第一行\r\n第二行\r第三行')
    expect(normalizeMarkdown('')).toBe('')
  })

  it('leaves dangerous HTML for the shared renderer to escape', () => {
    const content = normalizeMarkdown('# Safe\n\n<script>alert("unsafe")</script>')
    const { container } = render(<MarkdownRenderer content={content} variant="event" />)

    expect(screen.getByRole('heading', { name: 'Safe' })).toBeInTheDocument()
    expect(container.querySelector('script')).toBeNull()
    expect(container.textContent).not.toContain('alert("unsafe")')
  })
})

describe('Markdown consumer migration contract', () => {
  it.each(consumerContracts)('$path imports and configures only the shared renderer', ({ path, calls, variants }) => {
    const text = source(path)

    expect(text).toMatch(/import \{ MarkdownRenderer \} from '@\/components\/markdown'/u)
    expect(text).not.toMatch(/from ['"]react-markdown['"]/u)
    expect(text).not.toMatch(/from ['"]remark-gfm['"]/u)
    expect(text.match(/<MarkdownRenderer\b/gu) ?? []).toHaveLength(calls)
    expect(text).not.toMatch(/className=["'][^"']*\bprose\b/u)

    for (const [variant, count] of Object.entries(variants)) {
      expect(text.match(new RegExp(`variant=["']${variant}["']`, 'gu')) ?? []).toHaveLength(count)
    }
  })

  it('keeps parser imports isolated to the shared renderer implementation', () => {
    const srcRootRelative = '../../src/'
    const implementationRelative = '../../src/components/markdown/MarkdownRenderer.tsx'
    const srcRoot = fileURLToPath(new URL(srcRootRelative, import.meta.url))
    const implementationPath = fileURLToPath(new URL(implementationRelative, import.meta.url))
    const implementation = readFileSync(implementationPath, 'utf8')
    expect(implementation).toMatch(/from 'react-markdown'/u)
    expect(implementation).toMatch(/from 'remark-gfm'/u)

    const parserOwners = sourceFiles(srcRoot).filter((path) => /from ['"](?:react-markdown|remark-gfm)['"]/u.test(readFileSync(path, 'utf8')))
    expect(parserOwners).toEqual([implementationPath])
  })

  it('marks both live chat and live thinking calls as streaming without owning throttling', () => {
    const chat = source('../../src/pages/chat/index.tsx')

    expect(chat).toMatch(/variant="chat"[\s\S]*?streaming[\s\S]*?complete=\{false\}/u)
    expect(chat).toMatch(/variant="thinking"[\s\S]*?streaming[\s\S]*?complete=\{false\}/u)
    expect(chat).not.toMatch(/requestAnimationFrame|last-render|lastRender/u)
  })
})
