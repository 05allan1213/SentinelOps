import { useCallback, useEffect, useEffectEvent, useMemo, useState } from 'react'
import { bindSSEVisibility, SSECursor, streamFetch, type SSEChunkHandler, type SSEError } from '@/utils/sse'

export interface UseSSECursorOptions {
  url: string
  runId: string
  initialAfterSeq?: number
  maxRetries?: number
  enabled?: boolean
  stopOnRunTerminal?: boolean
  signal?: AbortSignal
  onChunk: SSEChunkHandler
  onDone?: () => void
  onError?: (error: SSEError) => void
  onRetry?: (attempt: number, delayMs: number) => void
}

/** Transport state only. Event bodies stay with the consumer; done is not Run success. */
export function useSSECursor({ url, runId, initialAfterSeq = 0, maxRetries = 5, enabled = true, stopOnRunTerminal = true, signal, onChunk, onDone, onError, onRetry }: UseSSECursorOptions) {
  const [cursor] = useState(() => new SSECursor())
  const [revision, setRevision] = useState(0)
  const source = useMemo(() => ({ url, runId, initialAfterSeq, maxRetries, enabled, stopOnRunTerminal, signal, revision }), [url, runId, initialAfterSeq, maxRetries, enabled, stopOnRunTerminal, signal, revision])
  const [state, setState] = useState<{ source: typeof source | null; error: SSEError | null; done: boolean }>({ source: null, error: null, done: false })
  const chunk = useEffectEvent(onChunk)
  const complete = useEffectEvent(() => onDone?.())
  const fail = useEffectEvent((error: SSEError) => onError?.(error))
  const reconnect = useEffectEvent((attempt: number, delay: number) => onRetry?.(attempt, delay))

  useEffect(() => {
    const { url, runId, initialAfterSeq, maxRetries, enabled, stopOnRunTerminal, signal } = source
    if (!enabled) return
    let active = true
    const control = streamFetch(url, {
      method: 'GET', runId, cursor, signal, initialAfterSeq, maxRetries, stopOnRunTerminal,
      paused: document.visibilityState === 'hidden',
      onRetry: (attempt, delay) => { if (active) reconnect(attempt, delay) },
    }, (type, content, id) => { if (active) chunk(type, content, id) }, () => {
      if (active) { setState({ source, done: true, error: null }); complete() }
    }, error => {
      if (active) { setState({ source, done: false, error }); fail(error) }
    })
    const unbind = bindSSEVisibility(control)
    return () => { active = false; unbind(); control.abort() }
  }, [source, cursor])

  const retry = useCallback(() => setRevision(value => value + 1), [])
  return { cursor, error: state.source === source ? state.error : null, done: state.source === source && state.done, retry }
}
