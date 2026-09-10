import { useState } from 'react'
import { History, UserRound } from 'lucide-react'
import { useRuntimeContext } from '@/hooks/useRuntimeQueries'
import { cn } from '@/utils'
import RuntimeQualityState from './RuntimeQualityState'

interface Props {
  runId: string
}

const dash = (value: unknown) => (value === null || value === undefined || value === '' ? '—' : String(value))

const formatTime = (value: string | null | undefined) => {
  if (!value) return '—'
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString('zh-CN', { hour12: false })
}

export default function ContextPanel({ runId }: Props) {
  const [includeHistory, setIncludeHistory] = useState(false)
  const metadata = useRuntimeContext(runId)
  const withHistory = useRuntimeContext(runId, 'history', { enabled: includeHistory })
  const data = (includeHistory ? withHistory.data?.item : metadata.data?.item)

  return (
    <section data-testid="runtime-context-panel" className="rounded-xl border border-gray-200 bg-white p-4">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h3 className="flex items-center gap-2 text-sm font-medium text-gray-900">
          <UserRound className="w-4 h-4 text-indigo-500" />
          Runtime Context（默认仅元数据）
        </h3>
        <RuntimeQualityState availability={metadata.data?.availability} dataQuality={metadata.data?.data_quality} reasonCode={metadata.data?.reason_code} notRun={metadata.data?.not_run} />
      </div>

      {metadata.isLoading && !data ? (
        <p data-testid="runtime-context-loading" className="mt-3 text-sm text-gray-500">加载中…</p>
      ) : !data ? (
        <p data-testid="runtime-context-empty" className="mt-3 text-sm text-gray-500">Context 当前不可用</p>
      ) : (
        <>
          <dl className="mt-3 grid grid-cols-2 gap-2 text-xs lg:grid-cols-4">
            <div><dt className="text-gray-500">身份</dt><dd className="text-gray-800">{dash(data.identity?.username || data.identity?.user_id)}</dd></div>
            <div><dt className="text-gray-500">角色 / Scope</dt><dd className="text-gray-800">{dash(data.identity?.role)} / <span className="font-mono">{dash(data.identity?.scope)}</span></dd></div>
            <div><dt className="text-gray-500">Session Revision（使用 / 提交）</dt><dd className="tabular-nums text-gray-800">{data.session_revision_used} / {data.session_revision_committed}</dd></div>
            <div><dt className="text-gray-500">History 条数</dt><dd className="tabular-nums text-gray-800">{data.history_count}</dd></div>
            <div><dt className="text-gray-500">Summary Hash</dt><dd className="font-mono text-gray-800 break-all">{dash(data.summary_hash)}</dd></div>
            <div><dt className="text-gray-500">Budget Limits Hash</dt><dd className="font-mono text-gray-800 break-all">{dash(data.budget_limits_hash)}</dd></div>
            <div><dt className="text-gray-500">Policy / Config Hash</dt><dd className="font-mono text-gray-800 break-all">{dash(data.policy_hash)} / {dash(data.config_hash)}</dd></div>
            <div><dt className="text-gray-500">Runtime / Compatibility</dt><dd className="font-mono text-gray-800 break-all">{dash(data.runtime_version)} / {dash(data.runtime_compatibility_hash)}</dd></div>
            <div><dt className="text-gray-500">Deadline</dt><dd className="tabular-nums text-gray-800">{formatTime(data.deadline_at)}</dd></div>
            <div className="lg:col-span-3"><dt className="text-gray-500">Gate keys</dt><dd className="font-mono text-gray-800 break-all">{data.gate_keys?.join(', ') || '—'}</dd></div>
          </dl>

          <div className="mt-3 border-t border-gray-100 pt-2">
            <button
              type="button"
              onClick={() => setIncludeHistory(value => !value)}
              className="inline-flex items-center gap-1.5 text-xs text-indigo-700 hover:text-indigo-900"
            >
              <History className="w-3.5 h-3.5" />
              {includeHistory ? '收起历史（include=history）' : '展开历史（需内容权限，include=history）'}
            </button>
            {includeHistory && (
              <div data-testid="runtime-context-history" className={cn('mt-2 rounded-lg bg-gray-50 p-2 text-xs', withHistory.isFetching && 'opacity-80')}>
                {withHistory.isLoading ? (
                  <p className="text-gray-500">加载中…</p>
                ) : withHistory.isError || !withHistory.data?.item ? (
                  <p data-testid="runtime-context-history-unavailable" className="text-gray-600">历史不可用或无权限，服务端未返回内容。</p>
                ) : (
                  <>
                    {(withHistory.data.item.history ?? []).length === 0 ? (
                      <p className="text-gray-500">服务端未返回历史条目</p>
                    ) : (
                      <ul className="space-y-1">
                        {(withHistory.data.item.history ?? []).map((message, index) => (
                          <li key={`${message.role}-${index}`} data-testid="runtime-context-history-item" className="flex gap-2">
                            <span className="shrink-0 font-mono text-gray-500">{message.role}</span>
                            <span className="min-w-0 break-words text-gray-800">{message.content}</span>
                          </li>
                        ))}
                      </ul>
                    )}
                    <p className="mt-1 text-[11px] text-gray-500">
                      truncated {withHistory.data.item.history_truncated ? '是' : '否'} · redaction {withHistory.data.item.redaction_applied ? 'applied' : 'not applied'}
                    </p>
                  </>
                )}
              </div>
            )}
          </div>
        </>
      )}
    </section>
  )
}
