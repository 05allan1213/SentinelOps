import { useState } from 'react'
import { ChevronDown, ChevronRight } from 'lucide-react'
import MarkdownRenderer from '@/components/markdown/MarkdownRenderer'
import { cn } from '@/utils'
import type { RuntimeEventDTO, TimelineRes } from '@/types/runtime'

interface Props {
  data: TimelineRes
  loading?: boolean
}

const formatTime = (value: string | undefined) => {
  if (!value) return '—'
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString('zh-CN', { hour12: false })
}

function EventRow({ event }: { event: RuntimeEventDTO }) {
  const [open, setOpen] = useState(false)
  const attributes = Object.entries(event.attributes ?? {})

  return (
    <li data-testid="runtime-timeline-row" data-seq={event.seq} data-event-type={event.event_type} className="rounded-lg border border-gray-200 bg-white">
      <button
        type="button"
        onClick={() => setOpen(value => !value)}
        aria-expanded={open}
        className="w-full flex items-center gap-3 px-3 py-2 text-left hover:bg-gray-50 transition-colors duration-150"
      >
        {open ? <ChevronDown className="w-3.5 h-3.5 text-gray-400 shrink-0" /> : <ChevronRight className="w-3.5 h-3.5 text-gray-400 shrink-0" />}
        <span className="w-14 shrink-0 font-mono text-xs text-gray-500 tabular-nums">#{event.seq}</span>
        <span className="w-44 shrink-0 font-mono text-xs text-gray-800 truncate" title={event.event_type}>{event.event_type}</span>
        <span className="flex-1 min-w-0 truncate text-xs text-gray-600" title={event.summary}>{event.summary || '—'}</span>
        <span className="hidden xl:inline w-40 shrink-0 text-xs text-gray-500 tabular-nums">{formatTime(event.created_at)}</span>
        <span className="w-24 shrink-0 text-xs text-gray-500 tabular-nums">att {event.attempt} · g{event.generation}</span>
      </button>
      {open && (
        <div data-testid="runtime-timeline-detail" className="border-t border-gray-100 px-3 py-2 space-y-2">
          <dl className="grid grid-cols-2 gap-2 text-xs">
            <div><dt className="text-gray-500">Trace ID</dt><dd className="font-mono text-gray-800 break-all">{event.trace_id || '—'}</dd></div>
            <div><dt className="text-gray-500">Operation ID</dt><dd className="font-mono text-gray-800 break-all">{event.operation_id || '—'}</dd></div>
            <div><dt className="text-gray-500">Reference</dt><dd className="font-mono text-gray-800 break-all">{event.reference || '—'}</dd></div>
            <div><dt className="text-gray-500">时间</dt><dd className="tabular-nums text-gray-800">{formatTime(event.created_at)}</dd></div>
          </dl>
          {event.summary && (
            <div className="rounded-lg bg-gray-50 p-2 text-xs">
              <MarkdownRenderer content={event.summary} variant="runtime" complete />
            </div>
          )}
          {attributes.length > 0 && (
            <dl data-testid="runtime-timeline-attributes" className="grid grid-cols-2 gap-1 text-xs">
              {attributes.map(([key, value]) => (
                <div key={key} className="min-w-0">
                  <dt className="text-gray-500 font-mono truncate">{key}</dt>
                  <dd className="font-mono text-gray-800 break-all">{value}</dd>
                </div>
              ))}
            </dl>
          )}
        </div>
      )}
    </li>
  )
}

export default function RunTimeline({ data, loading }: Props) {
  const items = data.items ?? []
  return (
    <div data-testid="runtime-timeline" className="min-w-0">
      <div className="flex items-center justify-between gap-3 text-xs text-gray-500">
        <span>共 {data.page?.total ?? items.length} 个 canonical Event；分组/折叠只影响展示，不删除事件</span>
        {loading && <span data-testid="runtime-timeline-loading">加载中…</span>}
      </div>
      {items.length === 0 ? (
        <p className="mt-3 rounded-xl border border-gray-200 bg-white px-4 py-8 text-center text-sm text-gray-500">暂无事件</p>
      ) : (
        <ul className={cn('mt-3 flex flex-col gap-1.5', loading && 'opacity-80')}>
          {items.map(event => <EventRow key={`${event.run_id}-${event.seq}`} event={event} />)}
        </ul>
      )}
    </div>
  )
}
