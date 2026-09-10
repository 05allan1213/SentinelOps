import { useQuery } from '@tanstack/react-query'
import { runtimeQueryKeys } from '../../../src/hooks/useRuntimeQueries'
import { useRuntimeEventTail } from '../../../src/hooks/useRuntimeEventTail'
import type { RuntimeEventDTO, TimelineRes } from '../../../src/types/runtime'
import { fixture } from './runtime-sse-fixture'

const page = { page: 1, page_size: 50, total: 0, has_next: false }
const timelineKey = runtimeQueryKeys.timeline('run-1', { page: 1, page_size: 50 })
const runKey = runtimeQueryKeys.run('run-1')
const emptyTimeline: TimelineRes = { items: [], page, availability: 'available', data_quality: 'complete' }

export function RuntimeSSEHarness() {
  const tail = useRuntimeEventTail({ runId: 'run-1', enabled: true })
  const timeline = useQuery<TimelineRes>({
    queryKey: timelineKey,
    queryFn: async () => emptyTimeline,
    initialData: emptyTimeline,
  })
  const run = useQuery({
    queryKey: runKey,
    queryFn: async () => {
      fixture.runFetches += 1
      return { item: { summary: { run_id: 'run-1', status: 'running' }, availability: 'available', data_quality: 'complete' } }
    },
    initialData: { item: { summary: { run_id: 'run-1', status: 'running' }, availability: 'available', data_quality: 'complete' } },
  })

  return (
    <main className="mx-auto w-full max-w-5xl p-8" data-run-status={run.data?.item?.summary?.status ?? 'unknown'}>
      <h1 className="text-lg font-semibold text-gray-900">Runtime SSE fixture</h1>
      <p data-testid="connected" className="mt-2 text-sm text-gray-600">{String(tail.connected)}</p>
      <p data-testid="after-seq" className="text-sm text-gray-600">{tail.afterSeq}</p>
      <ul data-testid="timeline" className="mt-4 space-y-1">
        {(timeline.data?.items ?? []).map((item: RuntimeEventDTO) => (
          <li key={item.seq} data-testid="timeline-row" className="font-mono text-xs text-gray-800">{item.seq}:{item.event_type}</li>
        ))}
      </ul>
    </main>
  )
}
