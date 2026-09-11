import { useCallback, useEffect, useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { useSSECursor } from './useSSECursor'
import { runtimeQueryKeys, useRuntimeRun } from './useRuntimeQueries'
import type { RuntimeEventDTO, RuntimeStatus } from '@/types/runtime'
import type { SSEError } from '@/utils/sse'

const TERMINAL_STATUSES: readonly RuntimeStatus[] = ['succeeded', 'failed', 'canceled', 'parked']

export interface UseRuntimeEventTailOptions {
  runId: string
  enabled?: boolean
  onError?: (error: Error) => void
}

export interface RuntimeEventTailState {
  connected: boolean
  afterSeq: number
  lastEventAt: string | null
  error: Error | null
  retry: () => void
}

/** Merge one accepted event into the stream cache without duplicating or reordering rows. */
export function mergeRuntimeEvent(items: RuntimeEventDTO[] | undefined, event: RuntimeEventDTO): RuntimeEventDTO[] {
  const existing = items ?? []
  if (existing.some(item => item.seq === event.seq)) return existing
  return [...existing, event].sort((a, b) => a.seq - b.seq)
}

function parseRuntimeEvent(content: string, id: string | undefined, runId: string): RuntimeEventDTO | null {
  if (!id || !/^[1-9]\d*$/.test(id)) return null
  let parsed: unknown
  try {
    parsed = JSON.parse(content)
  } catch {
    return null
  }
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) return null
  const event = parsed as RuntimeEventDTO
  if (event.run_id && event.run_id !== runId) return null
  if (typeof event.seq !== 'number' || event.seq !== Number(id)) return null
  return event
}

/**
 * Single Runtime Run SSE reader. Consumes GET /runtime/v1/runs/{run_id}/events through the shared
 * D-06 transport (finite retry, monotonic cursor, visibility pause, abort), maps accepted events into
 * the stream cache, invalidates only the server-owned queries whose facts changed and never creates a Run.
 */
export function useRuntimeEventTail({ runId, enabled = true, onError }: UseRuntimeEventTailOptions): RuntimeEventTailState {
  const queryClient = useQueryClient()
  const [afterSeq, setAfterSeq] = useState(0)
  const [lastEventAt, setLastEventAt] = useState<string | null>(null)
  // Subscribe to the same server-owned query as the detail page. Historical events
  // only refresh this fact; they cannot latch a terminal state for a recovered Run.
  const runQuery = useRuntimeRun(runId, { enabled: enabled && Boolean(runId) })
  const isTerminal = TERMINAL_STATUSES.includes(runQuery.data?.item?.summary?.status as RuntimeStatus)

  const invalidate = useCallback((queryKey: readonly unknown[]) => {
    void queryClient.invalidateQueries({ queryKey }, { cancelRefetch: true })
  }, [queryClient])

  const onChunk = useCallback((_type: string, content: string, id?: string) => {
    const event = parseRuntimeEvent(content, id, runId)
    if (!event) return

    // Keep accepted frames in a separate stream cache. Paginated/filter-specific
    // timelines must be refreshed from the server, never appended indiscriminately.
    queryClient.setQueryData<RuntimeEventDTO[]>(runtimeQueryKeys.events(runId, 0), previous => mergeRuntimeEvent(previous, event))
    invalidate(['runtime', 'timeline', runId])

    setAfterSeq(previous => Math.max(previous, event.seq))
    setLastEventAt(event.created_at ?? null)

    const family = event.event_type.split('.')[0]
    if (family === 'run') {
      invalidate(runtimeQueryKeys.run(runId))
      invalidate(['runtime', 'attempts', runId])
      invalidate(['runtime', 'checkpoints', runId])
    }
    if (family === 'agent') {
      invalidate(runtimeQueryKeys.run(runId))
      invalidate(['runtime', 'attempts', runId])
    }
    if (family === 'approval' || family === 'effect') {
      invalidate(runtimeQueryKeys.run(runId))
      invalidate(['runtime', family === 'approval' ? 'approvals' : 'effects', runId])
    }
    if (family === 'checkpoint') invalidate(['runtime', 'checkpoints', runId])
    if (family === 'evidence') invalidate(['runtime', 'evidence', runId])
    if (family === 'trace') invalidate(['runtime', 'traces', runId])
    if (family === 'budget') invalidate(runtimeQueryKeys.run(runId))
    if (event.operation_id) invalidate(runtimeQueryKeys.operation(event.operation_id))
  }, [invalidate, queryClient, runId])

  const handleError = useCallback((error: SSEError) => {
    onError?.(error)
  }, [onError])

  const { error, done, retry } = useSSECursor({
    url: `/api/runtime/v1/runs/${runId}/events`,
    runId,
    initialAfterSeq: 0,
    stopOnRunTerminal: false,
    enabled: enabled && Boolean(runId) && !isTerminal,
    onChunk,
    onError: handleError,
  })

  // Foreground restore refreshes server facts once; hidden tabs keep their cursor and pause reconnects.
  useEffect(() => {
    if (!enabled || !runId) return
    const change = () => {
      if (document.visibilityState !== 'visible') return
      invalidate(runtimeQueryKeys.run(runId))
    }
    document.addEventListener('visibilitychange', change)
    return () => document.removeEventListener('visibilitychange', change)
  }, [enabled, invalidate, runId])

  return {
    connected: enabled && Boolean(runId) && !isTerminal && !error && !done,
    afterSeq,
    lastEventAt,
    error: error ?? null,
    retry,
  }
}

export default useRuntimeEventTail
