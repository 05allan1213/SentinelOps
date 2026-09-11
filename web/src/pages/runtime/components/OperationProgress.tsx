import { useEffect, useRef } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { CheckCircle2, Clock, RefreshCw, XCircle } from 'lucide-react'
import { useRuntimeOperationPolling } from '@/hooks/useRuntimeQueries'
import { cn } from '@/utils'
import type { OperationDTO } from '@/types/runtime'

interface Props {
  operationId: string
  onTerminal?: (operation: OperationDTO) => void
}

const STATUS_META: Record<string, { label: string; className: string }> = {
  accepted: { label: '已接受', className: 'border-blue-200 bg-blue-50 text-blue-700' },
  running: { label: '执行中', className: 'border-blue-200 bg-blue-50 text-blue-700' },
  succeeded: { label: '成功', className: 'border-emerald-200 bg-emerald-50 text-emerald-700' },
  failed: { label: '失败', className: 'border-red-200 bg-red-50 text-red-700' },
  canceled: { label: '已取消', className: 'border-gray-300 bg-gray-100 text-gray-600' },
  rejected: { label: '被拒绝', className: 'border-amber-200 bg-amber-50 text-amber-800' },
}

const formatTime = (value: string | null | undefined) => {
  if (!value) return '—'
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString('zh-CN', { hour12: false })
}

/** Polls GET /runtime/v1/operations/{id} while non-terminal; terminal facts come from the server only. */
export default function OperationProgress({ operationId, onTerminal }: Props) {
  const queryClient = useQueryClient()
  const query = useRuntimeOperationPolling(operationId)
  const operation = query.data?.item
  const notified = useRef<string | null>(null)

  useEffect(() => {
    if (!operation?.terminal || !operation.run_id || notified.current === operation.operation_id) return
    notified.current = operation.operation_id
    void queryClient.invalidateQueries({ queryKey: ['runtime', 'run', operation.run_id] })
    void queryClient.invalidateQueries({ queryKey: ['runtime', 'attempts', operation.run_id] })
    void queryClient.invalidateQueries({ queryKey: ['runtime', 'timeline', operation.run_id] })
    void queryClient.invalidateQueries({ queryKey: ['runtime', 'effects', operation.run_id] })
    void queryClient.invalidateQueries({ queryKey: ['runtime', 'checkpoints', operation.run_id] })
    onTerminal?.(operation)
  }, [onTerminal, operation, queryClient])

  const meta = operation ? STATUS_META[operation.status] ?? { label: operation.status, className: 'border-dashed border-gray-300 bg-white text-gray-500' } : undefined

  return (
    <section data-testid="runtime-operation-progress" data-operation-id={operationId} className="rounded-xl border border-gray-200 bg-white p-4">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h3 className="text-sm font-medium text-gray-900">Recovery Operation</h3>
        {query.isFetching && !operation?.terminal && (
          <span className="inline-flex items-center gap-1 text-xs text-gray-500"><RefreshCw className="w-3 h-3 animate-spin" />轮询中（仅非终态）</span>
        )}
      </div>
      {query.isError && (
        <div role="status" data-testid="runtime-operation-error" className="mt-3 flex flex-wrap items-center gap-2 text-sm text-red-700">
          <span>Operation 状态查询失败。{operation ? '保留上次状态，等待服务端确认。' : ''}</span>
          <button type="button" onClick={() => void query.refetch()} className="rounded border border-red-200 px-2 py-1 underline">重试</button>
        </div>
      )}
      {query.isLoading && !operation ? (
        <p className="mt-3 text-sm text-gray-500">加载中…</p>
      ) : operation ? (
        <>
          <div className="mt-3 flex flex-wrap items-center gap-2 text-xs">
            <span className="font-mono text-gray-900 break-all">{operation.operation_id}</span>
            <span data-testid="runtime-operation-status" data-status={operation.status} data-terminal={String(operation.terminal)} className={cn('rounded border px-1.5 py-0.5', meta?.className)}>
              {meta?.label}
            </span>
            {operation.status === 'succeeded' ? <CheckCircle2 className="w-3.5 h-3.5 text-emerald-500" /> : operation.terminal ? <XCircle className="w-3.5 h-3.5 text-gray-500" /> : <Clock className="w-3.5 h-3.5 text-blue-500" />}
            {operation.idempotent_replay && <span data-testid="runtime-operation-replay" className="rounded border border-violet-200 bg-violet-50 px-1.5 py-0.5 text-violet-700">幂等复用</span>}
          </div>
          <dl className="mt-3 grid grid-cols-2 gap-2 text-xs lg:grid-cols-4">
            <div><dt className="text-gray-500">Action</dt><dd className="text-gray-800">{operation.action}</dd></div>
            <div><dt className="text-gray-500">接受时间</dt><dd className="tabular-nums text-gray-800">{formatTime(operation.accepted_at)}</dd></div>
            <div><dt className="text-gray-500">开始 / 结束</dt><dd className="tabular-nums text-gray-800">{formatTime(operation.started_at)} / {formatTime(operation.finished_at)}</dd></div>
            <div><dt className="text-gray-500">Correlation Seq</dt><dd className="tabular-nums text-gray-800">{operation.correlation_seq ?? '—'}</dd></div>
            <div className="col-span-2"><dt className="text-gray-500">Reason</dt><dd className="text-gray-800 break-words">{operation.reason || '—'}</dd></div>
            <div><dt className="text-gray-500">Error Code</dt><dd className="font-mono text-gray-800 [overflow-wrap:anywhere]">{operation.error_code || '—'}</dd></div>
          </dl>
        </>
      ) : (
        <p className="mt-3 text-sm text-gray-500">暂无 Operation 状态</p>
      )}
    </section>
  )
}
