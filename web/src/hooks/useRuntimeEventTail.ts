import { useCallback, useEffect, useRef, useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { useSSECursor } from './useSSECursor'
import { runtimeQueryKeys } from './useRuntimeQueries'
import type { RuntimeEventDTO, RuntimeStatus, TimelineRes } from '@/types/runtime'
import type { SSEError } from '@/utils/sse'

const TERMINAL_STATUSES: readonly RuntimeStatus[] = ['succeeded', 'failed', 'canceled', 'parked']
const TERMINAL_EVENT_TYPES = new Set(['run.completed', 'run.parked', 'run.canceled'])

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

/** Canonical terminal fact carried by an event, never inferred from the transport closing. */
export function runtimeEventStatus(event: RuntimeEventDTO): string {
  return event.attributes?.to_status ?? event.attributes?.status ?? ''
}

export function runtimeEventIsTerminal(event: RuntimeEventDTO): boolean {
  if (TERMINAL_STATUSES.includes(runtimeEventStatus(event) as RuntimeStatus)) return true
  return TERMINAL_EVENT_TYPES.has(event.event_type)
}

/** Merge one accepted event into a timeline page without duplicating or reordering rows. */
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
 * the Timeline cache, invalidates only the server-owned queries whose facts changed and never creates a Run.
 */
export function useRuntimeEventTail({ runId, enabled = true, onError }: UseRuntimeEventTailOptions): RuntimeEventTailState {
  const queryClient = useQueryClient()
  const [afterSeq, setAfterSeq] = useState(0)
  const [lastEventAt, setLastEventAt] = useState<string | null>(null)
  const [terminalSeen, setTerminalSeen] = useState(false)
  const terminalRef = useRef(false)

  const invalidate = useCallback((queryKey: readonly unknown[]) => {
    void queryClient.invalidateQueries({ queryKey })
  }, [queryClient])

  const onChunk = useCallback((_type: string, content: string, id?: string) => {
    const event = parseRuntimeEvent(content, id, runId)
    if (!event) return

    // Merge into every cached timeline page for this Run; each page keeps its own filters.
    queryClient.getQueryCache()
      .findAll({ queryKey: ['runtime', 'timeline', runId] })
      .forEach(query => {
        queryClient.setQueryData<TimelineRes>(query.queryKey, previous => (
          previous ? { ...previous, items: mergeRuntimeEvent(previous.items, event) } : previous
        ))
      })

    setAfterSeq(previous => Math.max(previous, event.seq))
    setLastEventAt(event.created_at ?? null)

    const family = event.event_type.split('.')[0]
    if (family === 'run') invalidate(runtimeQueryKeys.run(runId))
    if (family === 'agent') {
      invalidate(runtimeQueryKeys.run(runId))
      invalidate(['runtime', 'attempts', runId])
    }
    if (family === 'approval') invalidate(['runtime', 'approvals', runId])
    if (family === 'effect') invalidate(['runtime', 'effects', runId])
    if (family === 'budget') invalidate(runtimeQueryKeys.run(runId))
    if (event.operation_id) invalidate(runtimeQueryKeys.operation(event.operation_id))

    if (runtimeEventIsTerminal(event)) {
      terminalRef.current = true
      setTerminalSeen(true)
    }
  }, [invalidate, queryClient, runId])

  const handleError = useCallback((error: SSEError) => {
    onError?.(error)
  }, [onError])

  const { error, done, retry } = useSSECursor({
    url: `/api/runtime/v1/runs/${runId}/events`,
    runId,
    initialAfterSeq: 0,
    enabled: enabled && Boolean(runId) && !terminalSeen,
    onChunk,
    onError: handleError,
  })

  // Foreground restore refreshes server facts once; hidden tabs keep their cursor and pause reconnects.
  useEffect(() => {
    if (!enabled || !runId) return
    const change = () => {
      if (document.visibilityState !== 'visible' || terminalRef.current) return
      invalidate(runtimeQueryKeys.run(runId))
    }
    document.addEventListener('visibilitychange', change)
    return () => document.removeEventListener('visibilitychange', change)
  }, [enabled, invalidate, runId])

  return {
    connected: enabled && Boolean(runId) && !terminalSeen && !error && !done,
    afterSeq,
    lastEventAt,
    error: error ?? null,
    retry,
  }
}

export default useRuntimeEventTail
