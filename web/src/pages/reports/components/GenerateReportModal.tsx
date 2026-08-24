import { useState } from 'react'
import { X, Sparkles, Loader2, CheckCircle } from 'lucide-react'
import { reportService } from '@/services/report'
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

export default function GenerateReportModal({ isOpen, onClose, onSuccess }: GenerateReportModalProps) {
  const [step, setStep] = useState(1)
  const [reportType, setReportType] = useState('')
  const [selectedEvents, setSelectedEvents] = useState<number[]>([])
  const [title, setTitle] = useState('')
  const [isGenerating, setIsGenerating] = useState(false)
  const [progress, setProgress] = useState(0)

  const handleGenerate = async () => {
    setIsGenerating(true)
    setProgress(0)

    // 模拟进度
    const interval = setInterval(() => {
      setProgress((prev) => {
        if (prev >= 90) {
          return prev
        }
        return prev + Math.random() * 15
      })
    }, 500)

    try {
      const { start, end } = getTimeRange(reportType)
      await reportService.generate({
        type: reportType as 'daily' | 'weekly' | 'monthly' | 'vuln_alert' | 'threat_brief' | 'custom',
        title,
        start_time: start,
        end_time: end,
        event_ids: selectedEvents,
      })

      clearInterval(interval)
      setProgress(100)
      toast.success('报告生成成功')

      setTimeout(() => {
        onSuccess?.()
        handleClose()
      }, 500)
    } catch (error) {
      clearInterval(interval)
      toast.error('报告生成失败')
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
    <div className="modal-overlay" onClick={handleClose}>
      <div className="modal w-full max-w-lg" onClick={(e) => e.stopPropagation()}>
        {/* Header */}
        <div className="modal-header">
          <div className="flex items-center gap-3">
            <div className="w-9 h-9 rounded-lg bg-primary-500/20 flex items-center justify-center">
              <Sparkles className="w-4 h-4 text-primary-400" />
            </div>
            <div>
              <h2 className="modal-title">AI 生成报告</h2>
              <p className="text-xs text-gray-500 mt-0.5">
                {step === 1 && '选择报告类型'}
                {step === 2 && '配置报告信息'}
                {step === 3 && '确认并生成'}
              </p>
            </div>
          </div>
          <button
            onClick={handleClose}
            className="p-1.5 rounded text-gray-400 hover:text-gray-200 hover:bg-gray-800 transition-colors"
          >
            <X className="w-5 h-5" />
          </button>
        </div>

        <div className="modal-body">
          {/* Step 1: Select Type */}
          {step === 1 && (
            <div className="grid grid-cols-2 gap-3">
              {reportTypes.map((type) => (
                <button
                  key={type.value}
                  onClick={() => {
                    setReportType(type.value)
                    setStep(2)
                  }}
                  className="flex flex-col items-start gap-1 p-3 rounded-lg border border-gray-700 hover:border-primary-500 hover:bg-gray-800/50 transition-colors text-left"
                >
                  <p className="text-sm font-medium text-gray-200">{type.label}</p>
                  <p className="text-xs text-gray-500">{type.description}</p>
                </button>
              ))}
            </div>
          )}

          {/* Step 2: Configure */}
          {step === 2 && (
            <div className="space-y-4">
              <div className="form-item">
                <label className="label">报告标题 <span className="text-danger-500">*</span></label>
                <input
                  type="text"
                  value={title}
                  onChange={(e) => setTitle(e.target.value)}
                  placeholder="输入报告标题"
                  className="input"
                />
              </div>

              <div className="form-item">
                <label className="label">报告类型</label>
                <p className="text-sm text-gray-300">
                  {reportTypes.find((t) => t.value === reportType)?.label} - {reportTypes.find((t) => t.value === reportType)?.description}
                </p>
              </div>
            </div>
          )}

          {/* Step 3: Generate */}
          {step === 3 && (
            <div className="space-y-4">
              {!isGenerating ? (
                <div className="space-y-3">
                  <div className="p-3 rounded-lg bg-gray-800/50 space-y-2">
                    <div className="flex justify-between text-sm">
                      <span className="text-gray-500">报告类型</span>
                      <span className="text-gray-200">
                        {reportTypes.find((t) => t.value === reportType)?.label}
                      </span>
                    </div>
                    <div className="flex justify-between text-sm">
                      <span className="text-gray-500">报告标题</span>
                      <span className="text-gray-200">{title}</span>
                    </div>
                    <div className="flex justify-between text-sm">
                      <span className="text-gray-500">时间范围</span>
                      <span className="text-gray-200">
                        {(() => {
                          const { start, end } = getTimeRange(reportType)
                          return `${start} ~ ${end}`
                        })()}
                      </span>
                    </div>
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
                  <p className="text-sm text-gray-200 font-medium mb-3">
                    {progress >= 100 ? '报告生成完成！' : 'AI 正在分析并生成报告...'}
                  </p>
                  <div className="w-full h-1.5 bg-gray-800 rounded-full overflow-hidden">
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
        </div>

        {/* Footer */}
        {step > 1 && !isGenerating && (
          <div className="modal-footer">
            <button onClick={() => setStep(step - 1)} className="btn-default">
              上一步
            </button>
            {step === 2 ? (
              <button
                onClick={() => setStep(3)}
                disabled={!title}
                className="btn-primary"
              >
                下一步
              </button>
            ) : (
              <button onClick={handleGenerate} className="btn-primary">
                <Sparkles className="w-4 h-4" />
                开始生成
              </button>
            )}
          </div>
        )}
      </div>
    </div>
  )
}
