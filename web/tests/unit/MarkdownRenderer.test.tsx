import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { createElement } from 'react'

import MarkdownRenderer from '@/components/markdown/MarkdownRenderer'

const faults = vi.hoisted(() => ({ parse: false, highlight: false, highlightCalls: 0 }))
vi.mock('remark-gfm', async (importOriginal) => {
  const original = await importOriginal<typeof import('remark-gfm')>()
  return { default: function (...args: Parameters<typeof original.default>) {
    if (faults.parse) throw new Error('parser failure')
    return original.default.apply(this, args)
  } }
})
vi.mock('rehype-highlight', async (importOriginal) => {
  const original = await importOriginal<typeof import('rehype-highlight')>()
  return { default: (...args: Parameters<typeof original.default>) => {
    const transform = original.default(...args)
    return (...transformArgs: Parameters<typeof transform>) => {
      faults.highlightCalls += 1
      if (faults.highlight) throw new Error('highlighter failure')
      return transform(...transformArgs)
    }
  } }
})

beforeEach(() => { faults.parse = false; faults.highlight = false; faults.highlightCalls = 0 })

const gfm = '# Incident\n\n- item\n- [x] checked\n\n1. step\n\n> evidence\n\n| Field | Value |\n| --- | --- |\n| state | unknown |\n\n~~obsolete~~\n\n<script>alert(1)</script>\n\n[unsafe](javascript:alert)\n\n![tracker](http://example.com/a.png)'

describe('MarkdownRenderer', () => {
  it.each(['chat', 'thinking', 'event', 'analysis', 'report', 'runtime'] as const)('shares GFM and security for %s', (variant) => {
    const { container } = render(createElement(MarkdownRenderer, { content: gfm, variant }))
    expect(screen.getByRole('heading', { name: 'Incident', level: 1 })).toBeInTheDocument()
    expect(container.querySelector('ul')).toBeInTheDocument()
    expect(container.querySelector('ol')).toBeInTheDocument()
    expect(screen.getByRole('checkbox')).toBeChecked()
    expect(screen.getByRole('checkbox')).toBeDisabled()
    expect(container.querySelector('blockquote')).toHaveTextContent('evidence')
    expect(screen.getByRole('table')).toHaveTextContent('unknown')
    expect(container.querySelector('del')).toHaveTextContent('obsolete')
    expect(container.querySelector('script')).toBeNull()
    expect(screen.getByText('unsafe').closest('a')).toBeNull()
    expect(container.querySelector('img')).toBeNull()
    expect(screen.getByText('图片不可用：tracker')).toBeInTheDocument()
  })

  it('highlights a fenced alias, keeps inline code toolbar-free, and copies exact raw text', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText } })
    const raw = 'const tag = "<b>"\n\n'
    const { container } = render(createElement(MarkdownRenderer, { content: `Use \`inline()\`.\n\n\`\`\`ts\n${raw}\`\`\`` }))
    expect(screen.getByText('inline()').tagName).toBe('CODE')
    expect(screen.getAllByRole('button')).toHaveLength(1)
    expect(screen.getByText('typescript')).toBeInTheDocument()
    expect(container.querySelector('pre .hljs-keyword')).toHaveTextContent('const')
    expect(container.querySelector('pre code')?.textContent).toBe(raw)
    fireEvent.click(screen.getByRole('button', { name: '复制代码' }))
    await waitFor(() => expect(writeText).toHaveBeenCalledWith(raw))
    expect(screen.getByRole('button', { name: '代码已复制' })).toBeInTheDocument()
  })

  it.each([
    ['closed CRLF', '```ts\r\nconst a = 1\r\n\r\n```', 'const a = 1\r\n\r\n'],
    ['unterminated fence', '```ts\nconst a = 1', 'const a = 1'],
  ])('preserves source newlines for %s when copying', async (_, content, raw) => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText } })
    render(createElement(MarkdownRenderer, { content }))
    fireEvent.click(screen.getByRole('button', { name: '复制代码' }))
    await waitFor(() => expect(writeText).toHaveBeenCalledWith(raw))
  })

  it.each([
    ['rust', 'fn main() {}', false, 'unsupported-language'],
    ['js', 'const active = true', true, 'streaming'],
    ['js', 'a'.repeat(32769), false, 'size-limit'],
    ['js', 'x\n'.repeat(1001), false, 'size-limit'],
  ])('skips highlighter work for case %# (%s)', (language, code, streaming, reason) => {
    const { container } = render(createElement(MarkdownRenderer, { content: `\`\`\`${language}\n${code}\n\`\`\``, streaming: Boolean(streaming) }))
    expect(faults.highlightCalls).toBe(0)
    expect(screen.getByTestId('code-block')).toHaveAttribute('data-highlight-skipped', reason)
    expect(container.querySelector('pre code')?.textContent).toBe(`${code}\n`)
    expect(container.querySelector('[class^="hljs-"]')).toBeNull()
  })

  it.each([
    ['typescript', 'const a: number = 1'], ['javascript', 'const a = true'],
    ['json', '{"a": true}'], ['go', 'package main'], ['python', 'def main(): pass'],
    ['sql', 'SELECT * FROM events'], ['shell', '$ echo safe'], ['bash', 'echo "$USER"'],
    ['yaml', 'active: true'], ['markdown', '# Heading'], ['css', 'a { color: red; }'],
    ['html', '<p>safe</p>'], ['xml', '<tag>safe</tag>'],
  ])('highlights allowlisted %s with the restricted registry', (language, code) => {
    const { container } = render(createElement(MarkdownRenderer, { content: `\`\`\`${language}\n${code}\n\`\`\`` }))
    expect(container.querySelector('pre [class^="hljs-"]')).not.toBeNull()
    expect(container.querySelector('pre code')?.textContent).toBe(`${code}\n`)
    expect(faults.highlightCalls).toBe(1)
  })

  it('keeps unlabeled fenced and indented blocks raw and copyable', () => {
    const { container } = render(createElement(MarkdownRenderer, { content: '```\nraw()\n```\n\n    indented()' }))
    expect(screen.getAllByRole('button', { name: '复制代码' })).toHaveLength(2)
    expect(container.querySelectorAll('[data-highlight-skipped="unsupported-language"]')).toHaveLength(2)
    expect(faults.highlightCalls).toBe(0)
  })

  it('uses raw code on highlight exceptions without losing surrounding Markdown', () => {
    faults.highlight = true
    const { container } = render(createElement(MarkdownRenderer, { content: '# Kept\n\n```js\nconst safe = true\n```' }))
    expect(screen.getByRole('heading', { name: 'Kept' })).toBeInTheDocument()
    expect(container.querySelector('pre code')?.textContent).toBe('const safe = true\n')
    expect(screen.getByRole('button', { name: '复制代码' })).toBeInTheDocument()
  })

  it('keeps external protections and in-app navigation while rejecting dangerous links', () => {
    render(createElement(MarkdownRenderer, { content: '[external](https://example.com) [internal](/runs/1) [data](data:text/html,evil) [mail](mailto:a@b.com)' }))
    expect(screen.getByRole('link', { name: 'external' })).toHaveAttribute('rel', 'noopener noreferrer')
    expect(screen.getByRole('link', { name: 'external' })).toHaveAttribute('target', '_blank')
    expect(screen.getByRole('link', { name: 'internal' })).not.toHaveAttribute('target')
    expect(screen.getByText('data').closest('a')).toBeNull()
    expect(screen.getByText('mail').closest('a')).toBeNull()
  })

  it('renders safe lazy images and recovers failed loads locally', () => {
    render(createElement(MarkdownRenderer, { content: '![Evidence](https://example.com/evidence.png)' }))
    const img = screen.getByRole('img', { name: 'Evidence' })
    expect(img).toHaveAttribute('loading', 'lazy')
    expect(img).toHaveAttribute('referrerpolicy', 'no-referrer')
    fireEvent.error(img)
    expect(screen.getByText('图片不可用：Evidence')).toBeInTheDocument()
  })

  it('keeps an unfinished stream plain and renders Markdown when completed', () => {
    const content = '# Partial\n\n```ts\nconst a ='
    const { container, rerender } = render(createElement(MarkdownRenderer, { content, streaming: true, complete: false }))
    expect(container.querySelector('h1')).toBeNull()
    expect(container.textContent).toContain(content)
    rerender(createElement(MarkdownRenderer, { content, streaming: false, complete: true }))
    expect(screen.getByRole('heading', { name: 'Partial' })).toBeInTheDocument()
  })

  it('shows escaped readable text on parser failure and recovers for new content', () => {
    faults.parse = true
    const error = vi.spyOn(console, 'error').mockImplementation(() => {})
    const content = '# broken\n<script>alert(1)</script>'
    const { container, rerender } = render(createElement(MarkdownRenderer, { content, className: 'custom' }))
    expect(screen.getByRole('status')).toHaveTextContent('Markdown 暂时无法解析')
    expect(container.textContent).toContain(content)
    expect(container.querySelector('script')).toBeNull()
    expect(container.firstChild).toHaveClass('min-w-0', 'max-w-full', 'custom')
    faults.parse = false
    rerender(createElement(MarkdownRenderer, { content: '# Recovered' }))
    expect(screen.getByRole('heading', { name: 'Recovered' })).toBeInTheDocument()
    error.mockRestore()
  })
})
