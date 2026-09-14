import { Dialog, DialogContent, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import Button from '@/components/common/Button'
import { useRef, useState } from 'react'
import { X, Loader2 } from 'lucide-react'
import { cn } from '@/utils'
import { knowledgeService, type KnowledgeBase } from '@/services/knowledge'
import toast from 'react-hot-toast'

interface Props {
  onClose: () => void
  onSuccess: (base: KnowledgeBase) => void
}

export default function BaseCreateModal({ onClose, onSuccess }: Props) {
  const nameInput = useRef<HTMLInputElement>(null)
  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [loading, setLoading] = useState(false)

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!name.trim()) { toast.error('请输入知识库名称'); return }
    try {
      setLoading(true)
      const base = await knowledgeService.createBase(name.trim(), description.trim())
      toast.success('知识库创建成功')
      onSuccess(base)
    } catch {
      toast.error('创建失败，请重试')
    } finally {
      setLoading(false)
    }
  }

  return (
    <Dialog open onOpenChange={open => { if (!open) onClose() }}>
      <DialogContent onOpenAutoFocus={event => { event.preventDefault(); nameInput.current?.focus() }} aria-describedby={undefined} className="max-w-md overflow-hidden p-4 sm:p-6">
        <DialogHeader className="flex items-start justify-between gap-3 space-y-0">
          <DialogTitle>新建知识库</DialogTitle>
          <Button variant="icon" aria-label="关闭" onClick={onClose}>
            <X className="w-4 h-4 text-gray-500" />
          </Button>
        </DialogHeader>
        <form onSubmit={handleSubmit} className="flex min-h-0 flex-1 flex-col overflow-y-auto space-y-4">
          <div>
            <label className="block text-sm font-medium text-gray-700 mb-1.5">
              名称 <span className="text-red-500">*</span>
            </label>
            <input
              type="text"
              value={name}
              onChange={e => setName(e.target.value)}
              placeholder="如：CVE 漏洞处置手册"
              className="control w-full text-sm"
              ref={nameInput}
            />
          </div>
          <div>
            <label className="block text-sm font-medium text-gray-700 mb-1.5">描述</label>
            <textarea
              value={description}
              onChange={e => setDescription(e.target.value)}
              placeholder="知识库用途说明（可选）"
              rows={3}
              className="control h-auto py-2 w-full text-sm resize-none"
            />
          </div>
          <div className="flex justify-end gap-3 pt-1">
            <button
              type="button"
              onClick={onClose}
              className="px-4 py-2 text-sm font-medium text-gray-600 bg-gray-100 rounded-lg hover:bg-gray-200 transition-colors"
            >
              取消
            </button>
            <button
              type="submit"
              disabled={loading}
              className={cn(
                'flex items-center gap-2 px-4 py-2 text-sm font-medium text-white rounded-lg transition-colors',
                loading ? 'bg-indigo-400 cursor-not-allowed' : 'bg-indigo-600 hover:bg-indigo-700',
              )}
            >
              {loading && <Loader2 className="w-3.5 h-3.5 animate-spin" />}
              创建
            </button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  )
}
