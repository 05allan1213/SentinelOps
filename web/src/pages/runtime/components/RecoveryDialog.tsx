import { Dialog, DialogContent, DialogTitle, DialogDescription } from '@/components/ui/dialog'
import { useRef, useState } from 'react'
import { AlertTriangle, X } from 'lucide-react'
import { runtimeService } from '@/services/runtime'
import { cn } from '@/utils'
import type { RecoveryAction, RunDetailDTO } from '@/types/runtime'

interface Props {
  run: RunDetailDTO
  action: RecoveryAction
  open: boolean
  onClose: () => void
  onAccepted: (operationId: string) => void
}

const ACTION_META: Record<RecoveryAction, { title: string; description: string; confirm: string }> = {
  resume: {
    title: 'Resume Run',
    description: '将唤醒 Worker 从最后一个已提交 Checkpoint 继续执行；不会重新执行已提交的 Effect。',
    confirm: '提交 Resume',
  },
  replay: {
    title: 'Replay Run',
    description: '在没有 Checkpoint、Approval 或 Effect 的前提下重放不可变输入；服务端会重新校验合法性。',
    confirm: '提交 Replay',
  },
  cancel: {
    title: 'Cancel Run',
    description: '请求取消该 Run；运行中的 Worker 将在安全点停止，已提交的 Effect 不会被回滚。',
    confirm: '提交 Cancel',
  },
  restore: {
    title: 'Runtime Restore',
    description: '仅对 parked + runtime_incompatible 的 Run 生效，且必须与当前兼容性完全一致；不是通用解锁。',
    confirm: '提交 Restore',
  },
}

export default function RecoveryDialog({ run, action, open, onClose, onAccepted }: Props) {
  const [reason, setReason] = useState('')
  const [generation, setGeneration] = useState(() => String(run.summary?.lease_generation ?? ''))
  const [compatibility, setCompatibility] = useState(() => run.summary?.runtime_compatibility_hash ?? '')
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<string | null>(null)
  // One idempotency key per mounted dialog (per user submission); retries of the same submission reuse it.
  const idempotencyKey = useRef<string>(crypto.randomUUID())
  const reasonRef = useRef<HTMLTextAreaElement>(null)
  const meta = ACTION_META[action]

  const generationValid = generation.trim() !== '' && Number.isSafeInteger(Number(generation)) && Number(generation) >= 0
  const compatibilityValid = action !== 'restore' || compatibility.trim() !== ''
  const canSubmit = reason.trim() !== '' && generationValid && compatibilityValid && !pending

  const submit = async () => {
    if (!canSubmit) return
    setPending(true)
    setError(null)
    try {
      const result = await runtimeService.recoverRun(run.summary.run_id, {
        action,
        idempotency_key: idempotencyKey.current,
        reason: reason.trim(),
        expected_generation: Number(generation),
        ...(action === 'restore' ? { expected_compatibility_hash: compatibility.trim() } : {}),
      })
      onAccepted(result.operation.operation_id)
    } catch (submitError) {
      setError(submitError instanceof Error ? submitError.message : '提交失败')
    } finally {
      setPending(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={value => { if (!value && !pending) onClose() }}>
      <DialogContent
        data-testid="runtime-recovery-dialog"
        data-action={action}
        className="max-w-lg"
        onOpenAutoFocus={event => { event.preventDefault(); reasonRef.current?.focus() }}
        onEscapeKeyDown={event => { if (pending) event.preventDefault() }}
        onPointerDownOutside={event => { if (pending) event.preventDefault() }}
      >
        <div className="flex items-start gap-3">
          <div className={cn('mt-0.5 flex h-10 w-10 flex-shrink-0 items-center justify-center rounded-full', action === 'cancel' || action === 'restore' ? 'bg-red-50' : 'bg-amber-50')}>
            <AlertTriangle className={cn('h-5 w-5', action === 'cancel' || action === 'restore' ? 'text-red-500' : 'text-amber-500')} />
          </div>
          <div className="flex-1 min-w-0">
            <DialogTitle className="text-base font-semibold text-gray-900">{meta.title}</DialogTitle>
            <DialogDescription className="mt-1 text-sm text-gray-600">{meta.description}</DialogDescription>
          </div>
          <button type="button" aria-label="关闭" onClick={onClose} disabled={pending} className="p-1 text-gray-400 hover:text-gray-600 transition-colors duration-150">
            <X className="h-4 w-4" />
          </button>
        </div>

        <div className="mt-4 space-y-3">
          <label className="block">
            <span className="text-xs font-medium text-gray-700">理由（必填）</span>
            <textarea
              ref={reasonRef}
              aria-label="恢复理由"
              required
              disabled={pending}
              value={reason}
              onChange={event => setReason(event.target.value)}
              rows={3}
              className="mt-1 w-full rounded-lg border border-gray-200 px-3 py-2 text-sm text-gray-900 focus:border-indigo-400 focus:outline-none focus:ring-2 focus:ring-indigo-500/20"
              placeholder="说明为什么需要该操作，将写入审计事件"
            />
          </label>
          <label className="block">
            <span className="text-xs font-medium text-gray-700">期望 Generation（必填）</span>
            <input
              aria-label="期望 Generation"
              required
              disabled={pending}
              value={generation}
              onChange={event => setGeneration(event.target.value)}
              inputMode="numeric"
              className="mt-1 h-9 w-full rounded-lg border border-gray-200 px-3 text-sm tabular-nums text-gray-900 focus:border-indigo-400 focus:outline-none focus:ring-2 focus:ring-indigo-500/20"
            />
          </label>
          {action === 'restore' && (
            <label className="block">
              <span className="text-xs font-medium text-gray-700">期望兼容性哈希（Restore 必填）</span>
              <input
                aria-label="期望兼容性哈希"
                required
                disabled={pending}
                value={compatibility}
                onChange={event => setCompatibility(event.target.value)}
                className="mt-1 h-9 w-full rounded-lg border border-gray-200 px-3 font-mono text-xs text-gray-900 focus:border-indigo-400 focus:outline-none focus:ring-2 focus:ring-indigo-500/20"
              />
            </label>
          )}
          {error && <p role="alert" data-testid="runtime-recovery-error" className="rounded-lg border border-red-200 bg-red-50 px-3 py-2 text-xs text-red-700">{error}</p>}
        </div>

        <div className="mt-5 flex items-center justify-end gap-2">
          <button
            type="button"
            onClick={onClose}
            disabled={pending}
            className="h-9 rounded-lg border border-gray-200 bg-white px-3 text-sm text-gray-600 hover:bg-gray-50 transition-colors duration-150"
          >
            取消
          </button>
          <button
            type="button"
            onClick={() => void submit()}
            disabled={!canSubmit}
            className={cn(
              'h-9 rounded-lg px-3 text-sm font-medium text-white transition-colors duration-150',
              canSubmit ? (action === 'cancel' || action === 'restore' ? 'bg-red-600 hover:bg-red-700' : 'bg-indigo-600 hover:bg-indigo-700') : 'bg-gray-300 cursor-not-allowed',
            )}
          >
            {pending ? '提交中…' : meta.confirm}
          </button>
        </div>
      </DialogContent>
    </Dialog>
  )
}
