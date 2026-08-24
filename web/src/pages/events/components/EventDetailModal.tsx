import { useState, useEffect } from 'react'
import ReactMarkdown from 'react-markdown'
import {
  X,
  ExternalLink,
  Clock,
  Shield,
  AlertTriangle,
} from 'lucide-react'
import { cn, formatDate } from '@/utils'
import type { SecurityEvent } from '@/types'
import { eventService } from '@/services/event'
import toast from 'react-hot-toast'
import CustomSelect, { type SelectOption } from '@/components/common/CustomSelect'

interface EventDetailModalProps {
  event: SecurityEvent | null
  onClose: () => void
  onUpdate?: (event: SecurityEvent) => void
}

const severityConfig: Record<string, { label: string; class: string; desc: string }> = {
  critical: { label: '严重', class: 'severity-critical', desc: '需要立即处理，可能导致严重安全事故' },
  high: { label: '高危', class: 'severity-high', desc: '建议尽快处理，存在较高安全风险' },
  medium: { label: '中危', class: 'severity-medium', desc: '建议在合适时间处理，存在一定风险' },
  low: { label: '低危', class: 'severity-low', desc: '可以计划处理，风险较低' },
  info: { label: '信息', class: 'severity-info', desc: '仅供参考，无需特别处理' },
}

export default function EventDetailModal({ event, onClose, onUpdate }: EventDetailModalProps) {
  const [currentStatus, setCurrentStatus] = useState<SecurityEvent['status']>(event?.status || 'new')
  const [updatingStatus, setUpdatingStatus] = useState(false)

  useEffect(() => {
    setCurrentStatus(event?.status || 'new')
  }, [event?.id, event?.status])

  if (!event) return null

  const severity = severityConfig[event.severity] || severityConfig.info

  const handleUpdateStatus = async (newStatus: string) => {
    setCurrentStatus(newStatus as SecurityEvent['status'])
    setUpdatingStatus(true)
    try {
      await eventService.updateStatus(event.id, newStatus)
      toast.success('状态已更新')
      onUpdate?.({ ...event, status: newStatus as SecurityEvent['status'] })
    } catch {
      toast.error('状态更新失败')
      setCurrentStatus(event.status || 'new')
    } finally {
      setUpdatingStatus(false)
    }
  }

  return (
    <div className="modal-overlay" onClick={onClose}>
      <div
        className="modal w-full max-w-2xl max-h-[90vh] flex flex-col"
        onClick={(e) => e.stopPropagation()}
      >
        {/* Header */}
        <div className="modal-header">
          <div className="flex-1 pr-4">
            <div className="flex items-center gap-2 mb-2">
              <span className={severity.class}>{severity.label}</span>
              {event.cve_id && (
                <span className="text-sm font-mono text-blue-600">{event.cve_id}</span>
              )}
            </div>
            <h2 className="text-lg font-medium text-gray-900 leading-tight">
              {event.title}
            </h2>
          </div>
          <button
            onClick={onClose}
            className="p-1.5 rounded text-gray-400 hover:text-gray-600 hover:bg-gray-100 transition-colors"
          >
            <X className="w-5 h-5" />
          </button>
        </div>

        {/* Content */}
        <div className="modal-body flex-1 overflow-y-auto space-y-4">
          {/* Meta Info */}
          <div className="grid grid-cols-2 gap-3">
            <div className="p-3 rounded-lg bg-gray-50 border border-gray-100">
              <div className="flex items-center gap-2 text-gray-500 text-xs mb-1">
                <Clock className="w-3.5 h-3.5" />
                发现时间
              </div>
              <p className="text-sm text-gray-700">{formatDate(event.event_time)}</p>
            </div>
            <div className="p-3 rounded-lg bg-gray-50 border border-gray-100">
              <div className="flex items-center gap-2 text-gray-500 text-xs mb-1">
                <Shield className="w-3.5 h-3.5" />
                数据来源
              </div>
              <p className="text-sm text-gray-700">{event.source === 'web_search' ? '联网搜索' : event.source}</p>
            </div>
            {event.cvss_score != null && event.cvss_score > 0 && (
              <div className="p-3 rounded-lg bg-gray-50 border border-gray-100">
                <div className="flex items-center gap-2 text-gray-500 text-xs mb-1">
                  <AlertTriangle className="w-3.5 h-3.5" />
                  CVSS 评分
                </div>
                <p className="text-sm text-gray-700 font-mono">
                  {event.cvss_score}
                  <span className="text-gray-400 ml-1">/ 10.0</span>
                </p>
              </div>
            )}
            {event.affected_vendor && (
              <div className="p-3 rounded-lg bg-gray-50 border border-gray-100">
                <div className="text-gray-500 text-xs mb-1">影响厂商</div>
                <p className="text-sm text-gray-700">{event.affected_vendor}</p>
              </div>
            )}
          </div>

          {/* Severity Alert */}
          <div className={cn(
            'alert',
            event.severity === 'critical' && 'alert-danger',
            event.severity === 'high' && 'alert-warning',
            event.severity === 'medium' && 'alert-warning',
            event.severity === 'low' && 'alert-info',
            event.severity === 'info' && 'bg-gray-50 border border-gray-200 text-gray-500'
          )}>
            <AlertTriangle className="w-4 h-4 flex-shrink-0" />
            <p className="text-sm">{severity.desc}</p>
          </div>

          {/* Source Link */}
          {event.source_url && (
            <div>
              <h3 className="text-sm font-medium text-gray-500 mb-2">原始链接</h3>
              <a
                href={event.source_url}
                target="_blank"
                rel="noopener noreferrer"
                className="flex items-center gap-2 p-3 rounded-lg bg-gray-50 border border-gray-100 text-sm text-blue-600 hover:text-blue-700 hover:bg-blue-50 transition-colors"
              >
                <ExternalLink className="w-4 h-4 flex-shrink-0" />
                <span className="truncate">{event.source_url}</span>
              </a>
            </div>
          )}

          {/* Risk Score */}
          {event.risk_score != null && event.risk_score > 0 && (
            <div>
              <h3 className="text-sm font-medium text-gray-500 mb-2">风险评分</h3>
              <div className="p-3 rounded-lg bg-gray-50 border border-gray-100 flex items-center gap-3">
                <span className={cn(
                  'text-2xl font-bold',
                  event.risk_score >= 80 ? 'text-red-500' : event.risk_score >= 60 ? 'text-orange-500' : event.risk_score >= 40 ? 'text-yellow-500' : 'text-green-500'
                )}>{event.risk_score}</span>
                <span className="text-gray-400 text-sm">/ 100</span>
                <div className="flex-1 bg-gray-200 rounded-full h-2 ml-2">
                  <div
                    className={cn('h-2 rounded-full', event.risk_score >= 80 ? 'bg-red-500' : event.risk_score >= 60 ? 'bg-orange-500' : event.risk_score >= 40 ? 'bg-yellow-500' : 'bg-green-500')}
                    style={{ width: `${event.risk_score}%` }}
                  />
                </div>
              </div>
            </div>
          )}

          {/* Recommendation */}
          {event.recommendation && (
            <div>
              <h3 className="text-sm font-medium text-gray-500 mb-2">处置建议</h3>
              <div className="p-3 rounded-lg bg-gray-50 border border-gray-100 text-sm text-gray-700 leading-relaxed">
                <div className="prose prose-sm max-w-none">
                  <ReactMarkdown>{event.recommendation}</ReactMarkdown>
                </div>
              </div>
            </div>
          )}
        </div>

        {/* Footer Actions */}
        <div className={cn('modal-footer', updatingStatus && 'opacity-60 pointer-events-none')}>
          <CustomSelect
            value={currentStatus}
            onChange={v => handleUpdateStatus(v)}
            className="w-32"
            options={[
              { value: 'new', label: '新建' },
              { value: 'processing', label: '处理中' },
              { value: 'resolved', label: '已解决' },
              { value: 'ignored', label: '已忽略' },
            ] satisfies SelectOption[]}
          />
          <div className="flex-1" />
          <button onClick={onClose} className="btn-default">
            关闭
          </button>
        </div>
      </div>
    </div>
  )
}
