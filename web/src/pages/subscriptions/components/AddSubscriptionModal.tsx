import { useState } from 'react'
import { X, Github, Rss, Loader2, Zap } from 'lucide-react'
import { subscriptionService } from '@/services/subscription'
import toast from 'react-hot-toast'
import CustomSelect, { type SelectOption } from '@/components/common/CustomSelect'

import type { SourceType, Subscription } from '@/types'

interface AddSubscriptionModalProps {
  isOpen: boolean
  onClose: () => void
  onSuccess?: () => void
  editMode?: boolean
  initialData?: Subscription
}

const sourceTypes = [
  {
    value: 'github_repo',
    label: 'GitHub 仓库',
    icon: Github,
    description: '监控 GitHub 仓库的安全公告（Security Advisory）、Release 发布及 CVE 漏洞披露，适合追踪开源组件风险',
    example: '示例：https://github.com/owner/repo',
    placeholder: 'https://github.com/owner/repo',
  },
  {
    value: 'rss',
    label: 'RSS / Atom 订阅',
    icon: Rss,
    description: '订阅安全厂商公告、漏洞数据库（NVD、CNVD）、CERT 通告等 RSS/Atom 信息源，系统定时抓取并自动解析为安全事件',
    example: '示例：https://nvd.nist.gov/feeds/xml/cve/misc/nvd-rss.xml',
    placeholder: 'https://example.com/feed.xml',
  },
]

// 将分钟转换为 cron 表达式
function minutesToCron(minutes: number): string {
  if (minutes < 60) {
    return `*/${minutes} * * * *`
  }
  const hours = Math.floor(minutes / 60)
  if (hours < 24) {
    return `0 */${hours} * * *`
  }
  return '0 0 * * *'
}

// 将 cron 表达式转换回分钟
function cronToMinutes(cron: string): number {
  if (!cron) return 60
  const parts = cron.split(' ')
  if (parts.length < 5) return 60

  // 匹配 */N * * * * 格式（每 N 分钟）
  if (parts[0].startsWith('*/')) {
    return parseInt(parts[0].substring(2)) || 60
  }

  // 匹配 0 */N * * * 格式（每 N 小时）
  if (parts[1].startsWith('*/')) {
    return (parseInt(parts[1].substring(2)) || 1) * 60
  }

  // 匹配 0 0 * * * 格式（每天）
  if (parts[0] === '0' && parts[1] === '0') {
    return 1440
  }

  return 60
}

export default function AddSubscriptionModal({
  isOpen,
  onClose,
  onSuccess,
  editMode = false,
  initialData,
}: AddSubscriptionModalProps) {
  const [step, setStep] = useState(editMode ? 2 : 1)
  const [sourceType, setSourceType] = useState<SourceType | ''>(
    editMode && initialData ? initialData.source_type : ''
  )
  const [formData, setFormData] = useState({
    name: editMode && initialData ? initialData.name : '',
    url: editMode && initialData ? initialData.source_url : '',
    fetch_interval: editMode && initialData ? cronToMinutes(initialData.cron_expr) : 60,
    fetch_immediately: false,
  })
  const [isSubmitting, setIsSubmitting] = useState(false)

  const selectedSource = sourceTypes.find((s) => s.value === sourceType)

  const handleSubmit = async () => {
    setIsSubmitting(true)
    try {
      if (editMode && initialData) {
        await subscriptionService.update(initialData.id, {
          name: formData.name,
          url: formData.url,
          type: sourceType as string,
          cron_expr: minutesToCron(formData.fetch_interval),
        })
        toast.success('订阅更新成功')

        // 立即抓取：更新后同步触发一次数据拉取
        if (formData.fetch_immediately) {
          const fetchToast = toast.loading('正在获取订阅消息...')
          try {
            const result = await subscriptionService.fetch(initialData.id)
            toast.dismiss(fetchToast)
            toast.success(`获取完成：新增 ${result.new_count} 条事件`)
          } catch {
            toast.dismiss(fetchToast)
            toast.error('立即获取失败，将在下次定时任务时自动抓取')
          }
        }
      } else {
        const { id } = await subscriptionService.create({
          name: formData.name,
          description: '',
          source_type: sourceType as SourceType,
          source_url: formData.url,
          cron_expr: minutesToCron(formData.fetch_interval),
          config: '',
        })
        toast.success('订阅创建成功')

        // 立即抓取：创建后同步触发一次数据拉取
        if (formData.fetch_immediately && id) {
          const fetchToast = toast.loading('正在获取订阅消息...')
          try {
            const result = await subscriptionService.fetch(id)
            toast.dismiss(fetchToast)
            toast.success(`获取完成：新增 ${result.new_count} 条事件`)
          } catch {
            toast.dismiss(fetchToast)
            toast.error('立即获取失败，将在下次定时任务时自动抓取')
          }
        }
      }
      onSuccess?.()
      handleClose()
    } catch (error) {
      toast.error(editMode ? '更新订阅失败' : '创建订阅失败')
      console.error(error)
    } finally {
      setIsSubmitting(false)
    }
  }

  const handleClose = () => {
    setStep(1)
    setSourceType('')
    setFormData({ name: '', url: '', fetch_interval: 60, fetch_immediately: false })
    onClose()
  }

  if (!isOpen) return null

  return (
    <>
      {/* Backdrop */}
      <div className="modal-overlay" onClick={handleClose}>
        {/* Modal */}
        <div className="modal w-full max-w-lg" onClick={(e) => e.stopPropagation()}>
          {/* Header */}
          <div className="modal-header">
            <div>
              <h2 className="modal-title">{editMode ? '编辑订阅' : '添加订阅'}</h2>
              <p className="text-sm text-gray-500 mt-0.5">
                {step === 1 ? '选择数据源类型' : '配置订阅信息'}
              </p>
            </div>
            <button
              onClick={handleClose}
              className="p-1.5 rounded text-gray-400 hover:text-gray-600 hover:bg-gray-100 transition-colors"
            >
              <X className="w-5 h-5" />
            </button>
          </div>

          <div className="modal-body max-h-[60vh]">
            {/* Step 1: Select Source Type */}
            {step === 1 && (
              <div className="flex flex-col gap-3">
                {sourceTypes.map((source) => (
                  <button
                    key={source.value}
                    onClick={() => {
                      setSourceType(source.value as SourceType)
                      setStep(2)
                    }}
                    className="flex items-start gap-4 p-4 rounded-lg border border-gray-200 hover:border-primary-400 hover:bg-primary-50 transition-colors text-left"
                  >
                    <div className="w-10 h-10 rounded bg-gray-100 flex items-center justify-center flex-shrink-0 mt-0.5">
                      <source.icon className="w-5 h-5 text-gray-600" />
                    </div>
                    <div className="min-w-0 flex-1">
                      <p className="text-sm font-semibold text-gray-900">{source.label}</p>
                      <p className="text-xs text-gray-600 mt-1 leading-relaxed">{source.description}</p>
                      <p className="text-xs text-gray-400 mt-1.5">{source.example}</p>
                    </div>
                  </button>
                ))}
              </div>
            )}

            {/* Step 2: Configure */}
            {step === 2 && selectedSource && (
              <div className="space-y-4">
                {/* Source Type Badge */}
                <div className="flex items-center gap-3 p-3 rounded-lg bg-gray-50 border border-gray-200">
                  <selectedSource.icon className="w-5 h-5 text-primary-500" />
                  <span className="text-sm font-medium text-gray-700">{selectedSource.label}</span>
                  {!editMode && (
                    <button
                      onClick={() => setStep(1)}
                      className="ml-auto text-xs text-primary-500 hover:text-primary-600"
                    >
                      更换类型
                    </button>
                  )}
                </div>

                {/* Name */}
                <div className="form-item">
                  <label className="label">订阅名称 <span className="text-danger-500">*</span></label>
                  <input
                    type="text"
                    value={formData.name}
                    onChange={(e) => setFormData({ ...formData, name: e.target.value })}
                    placeholder="输入订阅名称"
                    className="input"
                  />
                </div>

                {/* URL */}
                <div className="form-item">
                  <label className="label">
                    订阅地址 <span className="text-danger-500">*</span>
                  </label>
                  <input
                    type="text"
                    value={formData.url}
                    onChange={(e) => setFormData({ ...formData, url: e.target.value })}
                    placeholder={selectedSource.placeholder}
                    className="input"
                  />
                </div>

                {/* Fetch Interval */}
                <div className="form-item">
                  <label className="label">抓取间隔</label>
                  <CustomSelect
                    value={formData.fetch_interval}
                    onChange={v => setFormData({ ...formData, fetch_interval: Number(v) })}
                    options={[
                      { value: 15, label: '15 分钟' },
                      { value: 30, label: '30 分钟' },
                      { value: 60, label: '1 小时' },
                      { value: 180, label: '3 小时' },
                      { value: 360, label: '6 小时' },
                      { value: 720, label: '12 小时' },
                      { value: 1440, label: '24 小时' },
                    ] satisfies SelectOption[]}
                  />
                </div>

                {/* Fetch Immediately */}
                <button
                    type="button"
                    onClick={() => setFormData({ ...formData, fetch_immediately: !formData.fetch_immediately })}
                    className={`w-full flex items-center gap-3 px-3 py-2.5 rounded-lg border transition-colors text-left ${
                      formData.fetch_immediately
                        ? 'border-primary-400 bg-primary-50 text-primary-700'
                        : 'border-gray-200 text-gray-600 hover:border-gray-300 hover:bg-gray-50'
                    }`}
                  >
                    <Zap className={`w-4 h-4 flex-shrink-0 ${formData.fetch_immediately ? 'text-primary-500' : 'text-gray-400'}`} />
                    <div className="flex-1 min-w-0">
                      <p className="text-sm font-medium">{editMode ? '立即获取最新消息' : '创建后立即获取'}</p>
                      <p className="text-xs text-gray-500 mt-0.5">{editMode ? '保存更新后立即触发一次数据拉取' : '跳过首次等待，创建完成后立即拉取一次数据'}</p>
                    </div>
                    <div className={`w-4 h-4 rounded border flex-shrink-0 flex items-center justify-center transition-colors ${
                      formData.fetch_immediately ? 'border-primary-500 bg-primary-500' : 'border-gray-300'
                    }`}>
                      {formData.fetch_immediately && (
                        <svg className="w-2.5 h-2.5 text-white" fill="none" viewBox="0 0 10 10">
                          <path d="M1.5 5l2.5 2.5 4.5-4.5" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round"/>
                        </svg>
                      )}
                    </div>
                  </button>
              </div>
            )}
          </div>

          {/* Footer */}
          {step === 2 && (
            <div className="modal-footer">
              <button onClick={handleClose} className="btn-default">
                取消
              </button>
              <button
                onClick={handleSubmit}
                disabled={!formData.name || !formData.url || isSubmitting}
                className="btn-primary"
              >
                {isSubmitting ? (
                  <>
                    <Loader2 className="w-4 h-4 animate-spin" />
                    {editMode ? '更新中...' : '创建中...'}
                  </>
                ) : (
                  editMode ? '更新订阅' : '创建订阅'
                )}
              </button>
            </div>
          )}
        </div>
      </div>
    </>
  )
}
