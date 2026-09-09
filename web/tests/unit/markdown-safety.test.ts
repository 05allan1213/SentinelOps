import { fireEvent, render, screen, within } from '@testing-library/react'
import { createElement } from 'react'

import CodeBlock from '@/components/markdown/CodeBlock'
import SafeImage from '@/components/markdown/SafeImage'
import SafeLink from '@/components/markdown/SafeLink'
import {
  MAX_HIGHLIGHT_BYTES,
  MAX_HIGHLIGHT_LINES,
  isAllowedImageSource,
  isAllowedLink,
  isCodeWithinHighlightLimits,
  isFenceClosed,
  normalizeLanguage,
  streamingMarkdown,
} from '@/components/markdown/markdown'

describe('Markdown URL safety', () => {
  it.each([
    'javascript:alert(1)',
    'java\nscript:alert(1)',
    'data:text/html,<script>alert(1)</script>',
    'vbscript:msgbox(1)',
    'file:///etc/passwd',
    'mailto:security@example.com',
    'https://',
    'http://[::1',
    'not a url',
    '',
  ])('rejects dangerous or malformed link %j', (href) => {
    expect(isAllowedLink(href)).toBe(false)
  })

  it.each([
    'https://example.com/advisory',
    'http://example.com/advisory',
    '/runtime/runs/run-1',
    './run-1',
    '../runs',
    'runs/run-1',
    '#events',
    '?tab=events',
  ])('allows HTTP(S) or an in-app link %j', (href) => {
    expect(isAllowedLink(href)).toBe(true)
  })

  it.each([
    'javascript:alert(1)',
    'data:text/html,unsafe',
    'vbscript:msgbox(1)',
    'file:///tmp/unsafe',
    'https://',
    'not a url',
  ])('renders rejected link %j as non-clickable text', (href) => {
    const { container } = render(createElement(SafeLink, { href }, 'unsafe destination'))

    expect(screen.getByText('unsafe destination')).toBeInTheDocument()
    expect(container.querySelector('a')).not.toBeInTheDocument()
  })

  it('adds exact new-window protections to external HTTP(S) links', () => {
    render(createElement(SafeLink, { href: 'https://example.com/advisory' }, 'advisory'))

    const link = screen.getByRole('link', { name: 'advisory' })
    expect(link).toHaveAttribute('href', 'https://example.com/advisory')
    expect(link).toHaveAttribute('target', '_blank')
    expect(link).toHaveAttribute('rel', 'noopener noreferrer')
  })

  it('treats protocol-relative HTTP(S) destinations as external', () => {
    render(createElement(SafeLink, { href: '//example.com/advisory' }, 'protocol relative'))

    const link = screen.getByRole('link', { name: 'protocol relative' })
    expect(link).toHaveAttribute('target', '_blank')
    expect(link).toHaveAttribute('rel', 'noopener noreferrer')
  })

  it('keeps in-app links in the current window', () => {
    render(createElement(SafeLink, { href: '/runtime/runs/run-1' }, 'run'))

    const link = screen.getByRole('link', { name: 'run' })
    expect(link).toHaveAttribute('href', '/runtime/runs/run-1')
    expect(link).not.toHaveAttribute('target')
    expect(link).not.toHaveAttribute('rel')
  })
})

describe('Markdown image safety', () => {
  it.each([
    'https://images.example.com/evidence.png',
    '/assets/evidence.png',
    './evidence.png',
    '../evidence.png',
  ])('allows HTTPS remote or in-app image %j', (src) => {
    expect(isAllowedImageSource(src)).toBe(true)
  })

  it.each([
    'http://images.example.com/evidence.png',
    '//images.example.com/evidence.png',
    'data:image/svg+xml,<svg/>',
    'javascript:alert(1)',
    'file:///tmp/evidence.png',
    'not an image url',
    '',
  ])('rejects non-HTTPS remote, dangerous or malformed image %j', (src) => {
    expect(isAllowedImageSource(src)).toBe(false)
  })

  it('renders a bounded, lazy and no-referrer image', () => {
    render(createElement(SafeImage, { src: 'https://images.example.com/evidence.png', alt: 'Evidence' }))

    const image = screen.getByRole('img', { name: 'Evidence' })
    const wrapper = screen.getByTestId('safe-image-wrapper')
    expect(image).toHaveAttribute('src', 'https://images.example.com/evidence.png')
    expect(image).toHaveAttribute('loading', 'lazy')
    expect(image).toHaveAttribute('referrerpolicy', 'no-referrer')
    expect(image).toHaveAttribute('width', '640')
    expect(image).toHaveAttribute('height', '360')
    expect(image).toHaveClass('max-w-full', 'h-auto')
    expect(wrapper).toHaveClass('max-w-full', 'overflow-x-auto')
  })

  it('replaces a failed image with a local unavailable label', () => {
    render(createElement(SafeImage, {
      src: 'https://images.example.com/missing.png',
      alt: 'Missing evidence',
    }))

    fireEvent.error(screen.getByRole('img', { name: 'Missing evidence' }))

    expect(screen.queryByRole('img')).not.toBeInTheDocument()
    expect(screen.getByText('图片不可用：Missing evidence')).toBeInTheDocument()
  })

  it('uses the local fallback without creating an image for a rejected source', () => {
    render(createElement(SafeImage, {
      src: 'http://images.example.com/tracker.png',
      alt: 'Tracker',
    }))

    expect(screen.queryByRole('img')).not.toBeInTheDocument()
    expect(screen.getByText('图片不可用：Tracker')).toBeInTheDocument()
  })
})

describe('Markdown fenced language safety', () => {
  const canonicalLanguages = [
    'typescript',
    'javascript',
    'json',
    'go',
    'python',
    'sql',
    'shell',
    'bash',
    'yaml',
    'markdown',
    'css',
    'html',
    'xml',
  ] as const

  it.each(canonicalLanguages)('retains canonical language %s', (language) => {
    expect(normalizeLanguage(language)).toBe(language)
  })

  it.each([
    ['ts', 'typescript'],
    ['tsx', 'typescript'],
    ['js', 'javascript'],
    ['jsx', 'javascript'],
    ['mjs', 'javascript'],
    ['cjs', 'javascript'],
    ['jsonc', 'json'],
    ['golang', 'go'],
    ['py', 'python'],
    ['postgres', 'sql'],
    ['postgresql', 'sql'],
    ['mysql', 'sql'],
    ['sqlite', 'sql'],
    ['sh', 'shell'],
    ['zsh', 'shell'],
    ['shell-session', 'shell'],
    ['console', 'shell'],
    ['yml', 'yaml'],
    ['md', 'markdown'],
    ['mdown', 'markdown'],
    ['mkdown', 'markdown'],
    ['htm', 'html'],
    ['svg', 'xml'],
    [' language-TS ', 'typescript'],
  ])('normalizes alias %s to %s', (alias, canonical) => {
    expect(normalizeLanguage(alias)).toBe(canonical)
  })

  it.each([undefined, '', 'rust', 'java', 'brainfuck', 'language-ruby'])('rejects unknown language %j', (language) => {
    expect(normalizeLanguage(language)).toBeUndefined()
  })
})

describe('Markdown streaming fence safety', () => {
  it.each([
    ['plain text', true],
    ['```ts\nconst safe = true\n```', true],
    ['~~~sh\necho safe\n~~~', true],
    ['````markdown\n``` remains content\n````', true],
    ['\\``` escaped marker', true],
    ['    ``` indented marker', true],
    ['prefix ``` marker in prose', true],
    ['```ts\nconst open = true', false],
    ['~~~sh\necho open\n```', false],
    ['````ts\nconst open = true\n```', false],
    ['```ts\n\\``` escaped close', false],
    ['```ts\n``` trailing text', false],
    ['```ts\n~~~\n```', true],
  ])('reports fence closure for %j', (markdown, closed) => {
    expect(isFenceClosed(markdown)).toBe(closed)
  })

  it('returns stable plain mode only for an incomplete open fence', () => {
    const markdown = '```ts\nconst partial = true'

    expect(streamingMarkdown(markdown, false)).toEqual({ content: markdown, mode: 'plain' })
    expect(streamingMarkdown(markdown, true)).toEqual({ content: markdown, mode: 'markdown' })
    expect(streamingMarkdown(`${markdown}\n\`\`\``, false)).toEqual({
      content: `${markdown}\n\`\`\``,
      mode: 'markdown',
    })
  })
})

describe('Markdown code block safety', () => {
  it('keeps inline code toolbar-free while a fenced block has a copy toolbar', () => {
    render(createElement(
      'div',
      null,
      createElement('code', { 'data-testid': 'inline-code' }, 'inline()'),
      createElement(CodeBlock, { code: 'block()', language: 'js' }),
    ))

    expect(within(screen.getByTestId('inline-code')).queryByRole('button')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: '复制代码' })).toBeInTheDocument()
    expect(screen.getByText('javascript')).toBeInTheDocument()
  })

  it('copies the exact original raw code', () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.defineProperty(navigator, 'clipboard', {
      configurable: true,
      value: { writeText },
    })
    const code = 'const raw = "<tag>"\n\n'
    render(createElement(CodeBlock, { code, language: 'typescript' }))

    fireEvent.click(screen.getByRole('button', { name: '复制代码' }))

    expect(writeText).toHaveBeenCalledWith(code)
    expect(screen.getByTestId('code-block')).toHaveTextContent('const raw = "<tag>"')
  })

  it('renders unknown languages as raw code without a language label', () => {
    render(createElement(CodeBlock, { code: 'fn main() {}', language: 'rust' }))

    expect(screen.queryByText('rust')).not.toBeInTheDocument()
    expect(screen.getByTestId('code-block')).toHaveAttribute('data-highlight-skipped', 'unsupported-language')
    expect(screen.getByText('fn main() {}')).toBeInTheDocument()
  })

  it('documents and enforces byte and line highlighting limits', () => {
    expect(MAX_HIGHLIGHT_BYTES).toBe(32 * 1024)
    expect(MAX_HIGHLIGHT_LINES).toBe(1000)
    expect(isCodeWithinHighlightLimits('a'.repeat(MAX_HIGHLIGHT_BYTES))).toBe(true)
    expect(isCodeWithinHighlightLimits('a'.repeat(MAX_HIGHLIGHT_BYTES + 1))).toBe(false)
    expect(isCodeWithinHighlightLimits('中'.repeat(Math.floor(MAX_HIGHLIGHT_BYTES / 3)))).toBe(true)
    expect(isCodeWithinHighlightLimits('中'.repeat(Math.floor(MAX_HIGHLIGHT_BYTES / 3) + 1))).toBe(false)
    expect(isCodeWithinHighlightLimits(Array(MAX_HIGHLIGHT_LINES).fill('line').join('\n'))).toBe(true)
    expect(isCodeWithinHighlightLimits(Array(MAX_HIGHLIGHT_LINES + 1).fill('line').join('\n'))).toBe(false)
    expect(isCodeWithinHighlightLimits(Array(MAX_HIGHLIGHT_LINES).fill('line').join('\r\n'))).toBe(true)
    expect(isCodeWithinHighlightLimits(Array(MAX_HIGHLIGHT_LINES + 1).fill('line').join('\r'))).toBe(false)

    render(createElement(CodeBlock, {
      code: 'a'.repeat(MAX_HIGHLIGHT_BYTES + 1),
      language: 'js',
    }))
    expect(screen.getByTestId('code-block')).toHaveAttribute('data-highlight-skipped', 'size-limit')
  })

  it('marks streaming code ineligible for highlighting', () => {
    render(createElement(CodeBlock, {
      code: 'const partial =',
      language: 'ts',
      streaming: true,
    }))

    expect(screen.getByTestId('code-block')).toHaveAttribute('data-highlight-skipped', 'streaming')
  })
})
