import { useState } from 'react'
import { createPortal } from 'react-dom'
import { X, RotateCcw, Loader2 } from 'lucide-react'
import { cn } from '@/utils'
import { knowledgeService } from '@/services/knowledge'
import toast from 'react-hot-toast'

interface Props {
  /** 单文档重建时传 docId，批量时传 docIds */
  docId?: string
  docIds?: string[]
  /** 当前策略（单文档时用于预选和标注） */
  currentStrategy?: string
  onClose: () => void
  onSuccess: () => void
}

const STRATEGIES = [
  {
    value: 'hierarchical',
    label: '层级分块（推荐）',
    desc: '父块 1024 字符 + 子块 256 字符，检索精度与上下文完整性兼顾',
  },
  {
    value: 'sliding_window',
    label: '滑动窗口',
    desc: '滑动窗口 512 字符 + 128 重叠，通用场景，计算开销最低',
  },
  {
    value: 'code',
    label: '代码切分',
    desc: '按函数 / 类定义边界切分，适合 Go、Python、Java 代码文件',
  },
]

const STRATEGY_LABEL: Record<string, string> = {
  sliding_window:      '滑动窗口',
  hierarchical:    '层级分块',
  code:            '代码切分',
}

export default function RebuildModal({ docId, docIds, currentStrategy, onClose, onSuccess }: Props) {
  // 默认预选当前策略；批量时默认选 hierarchical
  const [strategy, setStrategy] = useState(currentStrategy ?? 'hierarchical')
  const [loading, setLoading] = useState(false)

  const isBatch = !!docIds && docIds.length > 0
  const count = isBatch ? docIds.length : 1

  const handleSubmit = async () => {
    try {
      setLoading(true)
      if (isBatch) {
        const res = await knowledgeService.batchRebuildDocs(docIds!, strategy)
        toast.success(`已提交 ${res.submitted} 个文档重建${res.failed > 0 ? `，${res.failed} 个失败` : ''}`)
      } else {
        await knowledgeService.rebuildDoc(docId!, strategy)
        toast.success('已提交重建任务')
      }
      onSuccess()
    } catch {
      toast.error('提交失败，请重试')
    } finally {
      setLoading(false)
    }
  }

  return createPortal(
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4">
      <div className="bg-white rounded-xl shadow-xl w-full max-w-md">
        {/* 标题 */}
        <div className="flex items-center justify-between px-6 py-4 border-b border-gray-100">
          <div>
            <h3 className="text-base font-semibold text-gray-900 flex items-center gap-2">
              <RotateCcw className="w-4 h-4 text-indigo-500" />
              重建索引{isBatch ? `（${count} 个文档）` : ''}
            </h3>
            {!isBatch && currentStrategy && (
              <p className="text-xs text-gray-500 mt-0.5">
                当前策略：{STRATEGY_LABEL[currentStrategy] ?? currentStrategy}
              </p>
            )}
          </div>
          <button onClick={onClose} className="p-1 rounded-lg hover:bg-gray-100 transition-colors">
            <X className="w-4 h-4 text-gray-500" />
          </button>
        </div>

        {/* 策略选择 */}
        <div className="px-6 py-4 space-y-2">
          <p className="text-sm text-gray-600 mb-3">
            选择分块策略后，旧向量将被清除并按新策略重新索引。
          </p>
          {STRATEGIES.map(s => {
            const isCurrent = !isBatch && s.value === currentStrategy
            return (
              <label
                key={s.value}
                className={cn(
                  'flex items-start gap-3 p-3 rounded-lg border cursor-pointer transition-colors',
                  strategy === s.value
                    ? 'border-indigo-400 bg-indigo-50'
                    : 'border-gray-200 hover:border-gray-300 hover:bg-gray-50',
                )}
              >
                <input
                  type="radio"
                  name="strategy"
                  value={s.value}
                  checked={strategy === s.value}
                  onChange={() => setStrategy(s.value)}
                  className="mt-0.5 accent-indigo-600"
                />
                <div className="flex-1 min-w-0">
                  <p className="text-sm font-medium text-gray-800 flex items-center gap-2">
                    {s.label}
                    {isCurrent && (
                      <span className="inline-flex items-center px-1.5 py-0.5 rounded text-[10px] font-semibold bg-indigo-100 text-indigo-600">
                        当前
                      </span>
                    )}
                  </p>
                  <p className="text-xs text-gray-500 mt-0.5">{s.desc}</p>
                </div>
              </label>
            )
          })}
        </div>

        {/* 操作按钮 */}
        <div className="flex justify-end gap-3 px-6 py-4 border-t border-gray-100">
          <button
            onClick={onClose}
            className="px-4 py-2 text-sm font-medium text-gray-600 bg-gray-100 rounded-lg hover:bg-gray-200 transition-colors"
          >
            取消
          </button>
          <button
            onClick={handleSubmit}
            disabled={loading}
            className={cn(
              'flex items-center gap-2 px-4 py-2 text-sm font-medium text-white rounded-lg transition-colors',
              loading ? 'bg-indigo-400 cursor-default' : 'bg-indigo-600 hover:bg-indigo-700',
            )}
          >
            {loading && <Loader2 className="w-3.5 h-3.5 animate-spin" />}
            确认重建
          </button>
        </div>
      </div>
    </div>,
    document.body,
  )
}
