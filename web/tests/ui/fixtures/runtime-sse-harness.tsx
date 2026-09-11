import { useQuery } from '@tanstack/react-query'
import { runtimeQueryKeys } from '../../../src/hooks/useRuntimeQueries'
import { useRuntimeEventTail } from '../../../src/hooks/useRuntimeEventTail'
import type { RuntimeEventDTO } from '../../../src/types/runtime'
import { fixture } from './runtime-sse-fixture'

import { runtimeService } from '../../../src/services/runtime'
import { useRuntimeRun } from '../../../src/hooks/useRuntimeQueries'
import { runDetailRes } from './runtime-detail'
import type { GetRunRes } from '../../../src/types/runtime'

runtimeService.getRun = async () => {
  fixture.runFetches += 1
  return runDetailRes({ summary: { status: fixture.status } }) as unknown as GetRunRes
}

export function RuntimeSSEHarness() {
  const tail = useRuntimeEventTail({ runId: 'run-1', enabled: true })
  const timeline = useQuery<RuntimeEventDTO[]>({
    queryKey: runtimeQueryKeys.events('run-1', 0),
    queryFn: async () => [],
    enabled: false,
  })
  const run = useRuntimeRun('run-1')

  return (
    <main className="mx-auto w-full max-w-5xl p-8" data-run-status={run.data?.item?.summary?.status ?? 'unknown'}>
      <h1 className="text-lg font-semibold text-gray-900">Runtime SSE fixture</h1>
      <p data-testid="connected" className="mt-2 text-sm text-gray-600">{String(tail.connected)}</p>
      <p data-testid="after-seq" className="text-sm text-gray-600">{tail.afterSeq}</p>
      <ul data-testid="timeline" className="mt-4 space-y-1">
        {(timeline.data ?? []).map((item: RuntimeEventDTO) => (
          <li key={item.seq} data-testid="timeline-row" className="font-mono text-xs text-gray-800">{item.seq}:{item.event_type}</li>
        ))}
      </ul>
    </main>
  )
}
