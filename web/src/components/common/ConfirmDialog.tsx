import { useId } from 'react'
import { Dialog, DialogContent, DialogTitle, DialogDescription } from '@/components/ui/dialog'
import { AlertTriangle, X } from 'lucide-react'
import { cn } from '@/utils'

interface ConfirmDialogProps {
  open: boolean
  title: string
  description: string
  confirmLabel?: string
  onConfirm: () => void
  onClose: () => void
  danger?: boolean
  reason?: string
  onReasonChange?: (value: string) => void
  reasonRequired?: boolean
  reasonLabel?: string
  reasonPlaceholder?: string
}

export default function ConfirmDialog({
  open,
  title,
  description,
  confirmLabel = '删除',
  onConfirm,
  onClose,
  danger = true,
  reason = '',
  onReasonChange,
  reasonRequired = false,
  reasonLabel = '理由',
  reasonPlaceholder = '请输入理由',
}: ConfirmDialogProps) {
  const reasonId = useId()

  const canConfirm = !reasonRequired || reason.trim().length > 0

  return (
    <Dialog open={open} onOpenChange={value => { if (!value) onClose() }}>
      <DialogContent className="max-w-sm">
        <div className="flex items-start gap-3 mb-5">
          {danger && (
            <div className="w-10 h-10 rounded-full bg-red-50 flex items-center justify-center flex-shrink-0 mt-0.5">
              <AlertTriangle className="w-5 h-5 text-red-500" />
            </div>
          )}
          <div className="flex-1 min-w-0">
            <DialogTitle className="text-base font-semibold text-gray-900">{title}</DialogTitle>
            <DialogDescription className="text-sm text-gray-500 mt-1 leading-relaxed">{description}</DialogDescription>
            {onReasonChange && (
              <div className="mt-4">
                <label htmlFor={reasonId} className="block text-xs font-medium text-gray-600 mb-1">
                  {reasonLabel}{reasonRequired ? '（必填）' : ''}
                </label>
                <textarea
                  id={reasonId}
                  required={reasonRequired}
                  value={reason}
                  onChange={event => onReasonChange(event.target.value)}
                  placeholder={reasonPlaceholder}
                  rows={3}
                  className="w-full rounded-lg border border-slate-200 px-3 py-2 text-sm text-slate-700 outline-none focus:border-indigo-400 focus:ring-2 focus:ring-indigo-100"
                />
              </div>
            )}
          </div>
          <button type="button" aria-label="关闭" onClick={onClose} className="text-gray-400 hover:text-gray-600 flex-shrink-0 -mt-0.5">
            <X className="w-4 h-4" />
          </button>
        </div>
        <div className="flex justify-end gap-3">
          <button onClick={onClose} className="btn-default">
            取消
          </button>
          <button
            onClick={() => { if (canConfirm) { onConfirm(); onClose() } }}
            disabled={!canConfirm}
            className={cn('btn', danger ? 'bg-red-600 text-white hover:bg-red-700' : 'btn-primary')}
          >
            {confirmLabel}
          </button>
        </div>
      </DialogContent>
    </Dialog>
  )
}
