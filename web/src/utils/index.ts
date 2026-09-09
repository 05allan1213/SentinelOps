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

/** 保留原始 Markdown（包括换行）；解析、安全策略与降级均由共享 Renderer 负责。 */
export function normalizeMarkdown(md: string): string {
  return md
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
