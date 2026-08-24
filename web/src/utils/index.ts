import { clsx, type ClassValue } from 'clsx'
import { twMerge } from 'tailwind-merge'
import type { ReportPayload } from '@/types'

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}

/**
 * 尝试将报告 content 字段解析为结构化 payload。
 * 旧格式（纯 Markdown）或解析失败时返回 null，调用方应降级使用原始字符串。
 */
export function parseReportPayload(content: string): ReportPayload | null {
  if (!content || content.trimStart()[0] !== '{') return null
  try {
    const parsed = JSON.parse(content)
    if (parsed?.format === 'sentinel-report-v1') return parsed as ReportPayload
    return null
  } catch {
    return null
  }
}

export function formatDate(date: string | Date, format = 'YYYY-MM-DD HH:mm') {
  const d = new Date(date)
  const year = d.getFullYear()
  const month = String(d.getMonth() + 1).padStart(2, '0')
  const day = String(d.getDate()).padStart(2, '0')
  const hours = String(d.getHours()).padStart(2, '0')
  const minutes = String(d.getMinutes()).padStart(2, '0')

  return format
    .replace('YYYY', String(year))
    .replace('MM', month)
    .replace('DD', day)
    .replace('HH', hours)
    .replace('mm', minutes)
}

export function formatRelativeTime(date: string | Date): string {
  const now = new Date()
  const d = new Date(date)
  const diff = now.getTime() - d.getTime()

  const minutes = Math.floor(diff / 60000)
  const hours = Math.floor(diff / 3600000)
  const days = Math.floor(diff / 86400000)

  if (minutes < 1) return '刚刚'
  if (minutes < 60) return `${minutes} 分钟前`
  if (hours < 24) return `${hours} 小时前`
  if (days < 7) return `${days} 天前`
  return formatDate(date, 'MM-DD HH:mm')
}

export function getSeverityColor(severity: string): string {
  const colors: Record<string, string> = {
    critical: 'text-red-400',
    high: 'text-orange-400',
    medium: 'text-yellow-400',
    low: 'text-blue-400',
    info: 'text-dark-400',
  }
  return colors[severity] || colors.info
}

export function getSeverityBadgeClass(severity: string): string {
  const classes: Record<string, string> = {
    critical: 'badge-critical',
    high: 'badge-high',
    medium: 'badge-medium',
    low: 'badge-low',
    info: 'badge-info',
  }
  return classes[severity] || classes.info
}

export function getSourceTypeLabel(type: string): string {
  const labels: Record<string, string> = {
    github_repo: 'GitHub',
    rss: 'RSS',
    nvd: 'NVD',
    custom: '自定义',
  }
  return labels[type] || type
}

export function getStatusLabel(status: string): string {
  const labels: Record<string, string> = {
    active: '运行中',
    paused: '已暂停',
    error: '异常',
  }
  return labels[status] || status
}

export function downloadBlob(blob: Blob, filename: string) {
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  document.body.appendChild(a)
  a.click()
  document.body.removeChild(a)
  URL.revokeObjectURL(url)
}

/**
 * 修复 LLM 输出中常见的 Markdown 格式问题，使 ReactMarkdown 能正确渲染。
 */
export function normalizeMarkdown(md: string): string {
  let result = md

  // 1. 将段落中间出现的 ###标题 提到独立行
  result = result.replace(/([^\n])\s*(#{1,6})([^\s#\n])/g, '$1\n\n$2 $3')
  result = result.replace(/([^\n])\s*(#{1,6})\s+/g, (_, pre, hashes) => `${pre}\n\n${hashes} `)

  // 2. 修复行首 ###标题（无空格）
  result = result.replace(/^(#{1,6})([^\s#\n])/gm, '$1 $2')

  // 3. 确保 ### 标题前后各有一个空行
  result = result.replace(/([^\n])\n(#{1,6} )/g, '$1\n\n$2')
  result = result.replace(/^(#{1,6} [^\n]+)\n([^\n#])/gm, '$1\n\n$2')

  // 4. 修复「### 标题- 内容」：LLM 常将副标题内联在同一行，如「###应急响应步骤- 立即需要采取的行动」
  //    拆为「### 应急响应步骤」+ 空行 + 「立即需要采取的行动」
  result = result.replace(
    /^(#{1,6}\s+)([\u4e00-\u9fa5\w]{2,20})-\s+(.+)$/gm,
    '$1$2\n\n$3',
  )

  // 5. 处理 LLM 常见内联列表格式
  result = result.split('\n').map(line => {
    if (/^#{1,6}\s/.test(line) || /^[-*]\s/.test(line)) return line

    // 模式 A：行首为纯中文短标题（2-12字）后紧跟「- 」
    const titleDashMatch = line.match(/^([\u4e00-\u9fa5]{2,12})-\s+(.+)$/)
    if (titleDashMatch) {
      const [, title, rest] = titleDashMatch
      const items = rest.split(/[。.]\s*-\s+|\s+-\s+/).map(s => s.trim()).filter(Boolean)
      if (items.length >= 1) {
        return `**${title}**\n\n` + items.map(p => `- ${p}`).join('\n')
      }
    }

    if (line.length < 60) return line

    // 模式 B：行内含 2+ 个「句号破折号」分隔符
    const byPeriodDash = line.split(/[。.]\s*-\s+/)
    if (byPeriodDash.length >= 3) {
      return byPeriodDash[0] + '\n' + byPeriodDash.slice(1).map(p => `- ${p.trim()}`).join('\n')
    }

    // 模式 C：行内含 3+ 个「空白破折号空白」分隔符
    const bySpaceDash = line.split(/\s+-\s+/)
    if (bySpaceDash.length >= 3) {
      return bySpaceDash[0] + '\n' + bySpaceDash.slice(1).map(p => `- ${p}`).join('\n')
    }

    return line
  }).join('\n')

  // 6. 清理连续超过两个的空行
  result = result.replace(/\n{3,}/g, '\n\n')

  return result
}

export function formatCronInterval(cronExpr: string): string {  if (!cronExpr) return '-'

  // 解析 cron 表达式: */15 * * * * (每15分钟)
  const parts = cronExpr.split(' ')
  if (parts.length < 5) return cronExpr

  const [minute, hour] = parts

  // 每 N 分钟: */N * * * *
  if (minute.startsWith('*/')) {
    const minutes = parseInt(minute.substring(2))
    return `${minutes} 分钟`
  }

  // 每 N 小时: 0 */N * * *
  if (hour.startsWith('*/')) {
    const hours = parseInt(hour.substring(2))
    return `${hours} 小时`
  }

  // 每天: 0 0 * * *
  if (minute === '0' && hour === '0') {
    return '24 小时'
  }

  return cronExpr
}
