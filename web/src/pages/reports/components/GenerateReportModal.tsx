import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogBody, DialogFooter } from '@/components/ui/dialog'
import Button from '@/components/common/Button'
import { useState } from 'react'
import { X, Sparkles, Loader2, CheckCircle } from 'lucide-react'
import { reportService } from '@/services/report'
import { chatService } from '@/services/chat'
import { runtimeService } from '@/services/runtime'
import toast from 'react-hot-toast'

interface GenerateReportModalProps {
  isOpen: boolean
  onClose: () => void
  onSuccess?: () => void
}

const reportTypes = [
  { value: 'custom', label: '决策简报', description: '一页纸管理层汇报，含一句话风险概括' },
  { value: 'daily', label: '日报', description: '今日安全事件汇总' },
  { value: 'weekly', label: '周报', description: '本周安全态势分析' },
  { value: 'monthly', label: '月报', description: '本月安全趋势报告' },
  { value: 'vuln_alert', label: '漏洞告警', description: '针对特定漏洞的深度分析' },
  { value: 'threat_brief', label: '威胁简报', description: '威胁情报汇总分析' },
]

// 获取时间范围
function getTimeRange(type: string): { start: string; end: string } {
  const now = new Date()
  const end = now.toISOString().split('T')[0]
  let start = end

  switch (type) {
    case 'daily':
      start = end
      break
    case 'weekly':
      const weekAgo = new Date(now.getTime() - 7 * 24 * 60 * 60 * 1000)
      start = weekAgo.toISOString().split('T')[0]
      break
    case 'monthly':
      const monthAgo = new Date(now.getTime() - 30 * 24 * 60 * 60 * 1000)
      start = monthAgo.toISOString().split('T')[0]
      break
    default:
      const defaultStart = new Date(now.getTime() - 7 * 24 * 60 * 60 * 1000)
      start = defaultStart.toISOString().split('T')[0]
  }

  return { start: start + ' 00:00:00', end: end + ' 23:59:59' }
}

function buildReportQuery(type: string, title: string, start: string, end: string, eventIds: number[]): string {
  const label = reportTypes.find((item) => item.value === type)?.label ?? type
  const lines = [
    `请生成一份${label}，报告标题为「${title || `${label} ${start.slice(0, 10)}`}」。`,
    `统计周期：${start} 至 ${end}。`,
    '要求：',
    '1. 输出完整 Markdown 报告正文，至少包含：概况、关键指标、重点事件、风险研判、处置建议。',
    '2. 只输出报告正文，不要调用 create_report、保存或通知类工具。',
  ]
  if (eventIds.length > 0) lines.push(`重点关注事件 ID：${eventIds.join(', ')}。`)
  return lines.join('\n')
}

function createReportSessionId(): string {
  return `report-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`
}

export default function GenerateReportModal({ isOpen, onClose, onSuccess }: GenerateReportModalProps) {
  const [step, setStep] = useState(1)
  const [reportType, setReportType] = useState('')
  const [selectedEvents, setSelectedEvents] = useState<number[]>([])
  const [title, setTitle] = useState('')
  const [isGenerating, setIsGenerating] = useState(false)
  const [progress, setProgress] = useState(0)

  const handleGenerate = async () => {
    setIsGenerating(true)
    setProgress(5)

    try {
      const { start, end } = getTimeRange(reportType)
      // 复用 durable Runtime：由现有 Report Agent 生成正文，再用现有报告写接口落库。
      const sessionId = createReportSessionId()
      const run = await chatService.createDurableRun({
        sessionId,
        query: buildReportQuery(reportType, title, start, end, selectedEvents),
      })

      let content = ''
      let sawApproval = false
      let streamError = ''
      let abortForApproval: (() => void) | null = null
      const stopAtApproval = () => {
        sawApproval = true
        abortForApproval?.()
      }
      const control = chatService.tailDurableRun({
        runId: run.run_id,
        onEvent: (type, raw) => {
          setProgress((prev) => Math.min(90, prev + 3))
          if (type === 'approval.requested') {
            // 聊天页需要保持长连等待审批结果；报告弹窗提交完提案即可收尾，
            // 否则 SSE 会一直处于 reconnecting，弹窗进度卡在 90%。
            stopAtApproval()
            return
          }
          if (type !== 'agent.plan') return
          try {
            const summary = (JSON.parse(raw) as { summary?: string }).summary?.trim()
            if (!summary) return
            try {
              const plan = JSON.parse(summary) as { response?: string }
              if (plan.response?.trim()) content = plan.response.trim()
            } catch {
              content = summary
            }
          } catch { /* 忽略格式异常的事件负载 */ }
        },
        onState: (state) => {
          if (state.kind !== 'run') return
          if (['failed', 'retryable_failed', 'canceled'].includes(state.status)) {
            streamError = state.error || '报告生成失败'
          }
          if (['waiting_approval', 'parked'].includes(state.status)) {
            stopAtApproval()
          }
        },
        onError: (error) => { streamError = error.message || '报告生成失败' },
      })
      abortForApproval = () => control.abort()
      await control.finished

      // SSE 断开不等于 Run 失败，终态必须回查服务端权威状态。
      const detail = await runtimeService.getRun(run.run_id)
      const status = detail.item?.summary?.status
      if (status === 'waiting_approval' || status === 'parked' || sawApproval) {
        setProgress(100)
        toast.success('报告提案已提交审批，请在控制台待办中处理，通过后自动入库')
        setTimeout(() => { onSuccess?.(); handleClose() }, 500)
        return
      }
      if (status !== 'succeeded') {
        throw new Error(streamError || `报告生成未完成（${status ?? 'unknown'}）`)
      }

      const effects = await runtimeService.getEffects(run.run_id)
      const createdByEffect = (effects.items ?? []).some(
        (effect) => effect.tool_name === 'create_report' && effect.status === 'succeeded',
      )
      if (!createdByEffect) {
        if (!content.trim()) throw new Error('模型未返回报告正文')
        await reportService.save(title || '安全报告', content, reportType)
      }
      setProgress(100)
      toast.success('报告生成成功')

      setTimeout(() => {
        onSuccess?.()
        handleClose()
      }, 500)
    } catch (error) {
      toast.error(error instanceof Error ? error.message : '报告生成失败')
      console.error(error)
      setIsGenerating(false)
    }
  }

  const handleClose = () => {
    setStep(1)
    setReportType('')
    setSelectedEvents([])
    setTitle('')
    setIsGenerating(false)
    setProgress(0)
    onClose()
  }

  if (!isOpen) return null

  return (
    <Dialog open onOpenChange={open => { if (!open) handleClose() }}>
      <DialogContent aria-describedby={undefined} className="max-w-lg overflow-hidden p-4 sm:p-6">
        {/* 弹窗标题 */}
        <DialogHeader className="flex items-start justify-between gap-3 space-y-0">
          <div className="flex items-center gap-3">
            <div className="w-9 h-9 rounded-lg bg-primary-500/20 flex items-center justify-center">
              <Sparkles className="w-4 h-4 text-primary-400" />
            </div>
            <div>
              <DialogTitle>AI 生成报告</DialogTitle>
              <p className="text-xs text-gray-500 mt-0.5">
                {step === 1 && '选择报告类型'}
                {step === 2 && '配置报告信息'}
                {step === 3 && '确认并生成'}
              </p>
            </div>
          </div>
          <Button variant="icon" aria-label="关闭" onClick={handleClose}>
            <X className="w-5 h-5" />
          </Button>
        </DialogHeader>

        <DialogBody className="">
          {/* 第一步：选择报告类型 */}
          {step === 1 && (
            <div className="grid grid-cols-2 gap-3">
              {reportTypes.map((type) => (
                <button
                  key={type.value}
                  onClick={() => {
                    setReportType(type.value)
                    setStep(2)
                  }}
                  className="flex flex-col items-start gap-1 p-3 rounded-lg border border-gray-200 hover:border-primary-500 hover:bg-gray-50 transition-colors text-left"
                >
                  <p className="text-sm font-medium text-gray-900">{type.label}</p>
                  <p className="text-xs text-gray-500">{type.description}</p>
                </button>
              ))}
            </div>
          )}

          {/* 第二步：填写生成配置 */}
          {step === 2 && (
            <div className="space-y-4">
              <div className="form-item">
                <label className="label">报告标题 <span className="text-danger-500">*</span></label>
                <input
                  type="text"
                  value={title}
                  onChange={(e) => setTitle(e.target.value)}
                  placeholder="输入报告标题"
                  className="control w-full"
                />
              </div>

              <div className="form-item">
                <label className="label">报告类型</label>
                <p className="text-sm text-gray-700">
                  {reportTypes.find((t) => t.value === reportType)?.label} - {reportTypes.find((t) => t.value === reportType)?.description}
                </p>
              </div>
            </div>
          )}

          {/* 第三步：生成报告 */}
          {step === 3 && (
            <div className="space-y-4">
              {!isGenerating ? (
                <div className="space-y-3">
                  <div className="p-3 rounded-lg bg-gray-50 space-y-2">
                    <div className="flex justify-between text-sm">
                      <span className="text-gray-500">报告类型</span>
                      <span className="text-gray-900">
                        {reportTypes.find((t) => t.value === reportType)?.label}
                      </span>
                    </div>
                    <div className="flex justify-between text-sm">
                      <span className="text-gray-500">报告标题</span>
                      <span className="text-gray-900">{title}</span>
                    </div>
                    <div className="flex justify-between text-sm">
                      <span className="text-gray-500">时间范围</span>
                      <span className="text-gray-900">
                        {(() => {
                          const { start, end } = getTimeRange(reportType)
                          return `${start} ~ ${end}`
                        })()}
                      </span>
                    </div>
                  </div>
                  <div className="rounded-lg border border-amber-500/30 bg-amber-500/10 p-3 text-xs text-amber-800">
                    报告入库属于 L1 变更：Agent 生成内容后会提交审批，审批通过后自动写入报告库。
                  </div>
                </div>
              ) : (
                <div className="py-6 text-center">
                  <div className="w-12 h-12 rounded-full bg-primary-500/20 flex items-center justify-center mx-auto mb-3">
                    {progress >= 100 ? (
                      <CheckCircle className="w-6 h-6 text-success-500" />
                    ) : (
                      <Loader2 className="w-6 h-6 text-primary-400 animate-spin" />
                    )}
                  </div>
                  <p className="text-sm text-gray-900 font-medium mb-3">
                    {progress >= 100 ? '报告生成完成！' : 'AI 正在分析并生成报告...'}
                  </p>
                  <div className="w-full h-1.5 bg-gray-200 rounded-full overflow-hidden">
                    <div
                      className="h-full bg-primary-500 transition-all duration-300"
                      style={{ width: `${Math.min(progress, 100)}%` }}
                    />
                  </div>
                  <p className="text-xs text-gray-500 mt-2">
                    {Math.round(Math.min(progress, 100))}%
                  </p>
                </div>
              )}
            </div>
          )}
        </DialogBody>

        {/* 底部操作区 */}
        {step > 1 && !isGenerating && (
          <DialogFooter>
            <Button onClick={() => setStep(step - 1)} variant="secondary">
              上一步
            </Button>
            {step === 2 ? (
              <Button
                onClick={() => setStep(3)}
                disabled={!title}
                variant="primary"
              >
                下一步
              </Button>
            ) : (
              <Button onClick={handleGenerate} variant="primary">
                <Sparkles className="w-4 h-4" />
                开始生成
              </Button>
            )}
          </DialogFooter>
        )}
      </DialogContent>
    </Dialog>
  )
}
