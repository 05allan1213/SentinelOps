const IN_APP_BASE_URL = new URL('https://sentinelops.invalid/')

export const MARKDOWN_LANGUAGES = [
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

export type MarkdownLanguage = (typeof MARKDOWN_LANGUAGES)[number]

const LANGUAGE_ALIASES: Readonly<Record<string, MarkdownLanguage>> = {
  typescript: 'typescript',
  ts: 'typescript',
  tsx: 'typescript',
  javascript: 'javascript',
  js: 'javascript',
  jsx: 'javascript',
  mjs: 'javascript',
  cjs: 'javascript',
  json: 'json',
  jsonc: 'json',
  go: 'go',
  golang: 'go',
  python: 'python',
  py: 'python',
  sql: 'sql',
  postgres: 'sql',
  postgresql: 'sql',
  mysql: 'sql',
  sqlite: 'sql',
  shell: 'shell',
  sh: 'shell',
  zsh: 'shell',
  'shell-session': 'shell',
  console: 'shell',
  bash: 'bash',
  yaml: 'yaml',
  yml: 'yaml',
  markdown: 'markdown',
  md: 'markdown',
  mdown: 'markdown',
  mkdown: 'markdown',
  css: 'css',
  html: 'html',
  htm: 'html',
  xml: 'xml',
  svg: 'xml',
}

function hasInvalidUrlText(value: string): boolean {
  if (value.length === 0 || value !== value.trim() || value.includes('\\')) return true

  for (const character of value) {
    const codePoint = character.codePointAt(0) ?? 0
    if (codePoint <= 0x20 || codePoint === 0x7f || character.trim() === '') return true
  }

  return false
}

function parseAllowedLink(href: string): URL | undefined {
  if (hasInvalidUrlText(href)) return undefined

  try {
    const parsed = new URL(href, IN_APP_BASE_URL)
    return parsed.protocol === 'http:' || parsed.protocol === 'https:' ? parsed : undefined
  } catch {
    return undefined
  }
}

export function isAllowedLink(href: string): boolean {
  return parseAllowedLink(href) !== undefined
}

export function isExternalLink(href: string): boolean {
  if (parseAllowedLink(href) === undefined) return false
  if (href.startsWith('//')) return true

  try {
    new URL(href)
    return true
  } catch {
    return false
  }
}

export function isAllowedImageSource(src: string): boolean {
  if (
    hasInvalidUrlText(src)
    || src.startsWith('//')
  ) {
    return false
  }

  try {
    return new URL(src, IN_APP_BASE_URL).protocol === 'https:'
  } catch {
    return false
  }
}

export function normalizeLanguage(language: string | undefined): MarkdownLanguage | undefined {
  if (language === undefined) return undefined

  const normalized = language.trim().toLowerCase().replace(/^language-/, '')
  return LANGUAGE_ALIASES[normalized]
}

interface OpenFence {
  marker: '`' | '~'
  length: number
}

function openingFence(line: string): OpenFence | undefined {
  const match = /^(`{3,}|~{3,})(.*)$/u.exec(line)
  if (match === null) return undefined

  const markerRun = match[1]
  const marker = markerRun[0] as OpenFence['marker']
  const info = match[2]
  if (marker === '`' && info.includes('`')) return undefined

  return { marker, length: markerRun.length }
}

function closesFence(line: string, fence: OpenFence): boolean {
  let markerLength = 0
  while (line[markerLength] === fence.marker) markerLength += 1

  return markerLength >= fence.length && line.slice(markerLength).trim() === ''
}

export function isFenceClosed(markdown: string): boolean {
  let openFence: OpenFence | undefined

  for (const line of markdown.split(/\r\n?|\n/u)) {
    if (openFence === undefined) {
      openFence = openingFence(line)
      continue
    }

    if (closesFence(line, openFence)) openFence = undefined
  }

  return openFence === undefined
}

export interface StreamingMarkdownResult {
  content: string
  mode: 'markdown' | 'plain'
}

export function streamingMarkdown(markdown: string, complete: boolean): StreamingMarkdownResult {
  return {
    content: markdown,
    mode: complete || isFenceClosed(markdown) ? 'markdown' : 'plain',
  }
}

// Syntax highlighting stays bounded to 32 KiB and 1,000 lines. Larger blocks
// remain raw and copyable so generated output cannot monopolize the main thread.
export const MAX_HIGHLIGHT_BYTES = 32 * 1024
export const MAX_HIGHLIGHT_LINES = 1000

export function isCodeWithinHighlightLimits(code: string): boolean {
  let bytes = 0
  let lines = 1
  let previousCharacter = ''

  for (const character of code) {
    if (character === '\r' || (character === '\n' && previousCharacter !== '\r')) {
      lines += 1
      if (lines > MAX_HIGHLIGHT_LINES) return false
    }

    const codePoint = character.codePointAt(0) ?? 0
    if (codePoint <= 0x7f) bytes += 1
    else if (codePoint <= 0x7ff) bytes += 2
    else if (codePoint <= 0xffff) bytes += 3
    else bytes += 4

    if (bytes > MAX_HIGHLIGHT_BYTES) return false
    previousCharacter = character
  }

  return true
}

export type HighlightSkipReason = 'streaming' | 'unsupported-language' | 'size-limit'

export function highlightSkipReason(
  code: string,
  language: string | undefined,
  streaming = false,
): HighlightSkipReason | undefined {
  if (streaming) return 'streaming'
  if (normalizeLanguage(language) === undefined) return 'unsupported-language'
  if (!isCodeWithinHighlightLimits(code)) return 'size-limit'
  return undefined
}
