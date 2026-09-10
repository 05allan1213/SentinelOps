export type SSEErrorCode = 'aborted' | 'http' | 'network' | 'protocol' | 'retries_exhausted' | 'consumer'

export class SSEError extends Error {
  constructor(public readonly code: SSEErrorCode, message: string, public readonly status?: number) {
    super(message)
    this.name = 'SSEError'
  }
}

const validSeq = (seq: number) => Number.isSafeInteger(seq) && seq >= 0

/** Per-Run high-water marks. reset is an explicit new/reloaded consumer boundary. */
export class SSECursor {
  private positions = new Map<string, number>()
  private activeRun = ''
  get lastSeq(): number { return this.positions.get(this.activeRun) ?? 0 }
  accept(runId: string, seq: number): boolean {
    if (!runId || !validSeq(seq)) return false
    this.activeRun = runId
    if (seq <= this.lastSeq) return false
    this.positions.set(runId, seq)
    return true
  }
  reset(runId: string, seq = 0): void {
    if (!runId || !validSeq(seq)) throw new SSEError('protocol', 'Invalid Run cursor')
    this.activeRun = runId
    this.positions.set(runId, seq)
  }
  /** Includes sequences covered by an initial/recovered high-water mark. */
  seen(runId: string, seq: number): boolean {
    return validSeq(seq) && seq <= (this.positions.get(runId) ?? 0)
  }
}

export interface StreamFetchOptions {
  method?: 'GET' | 'POST'
  body?: unknown
  signal?: AbortSignal
  runId?: string
  initialAfterSeq?: number
  maxRetries?: number
  onRetry?: (attempt: number, delayMs: number) => void
  cursor?: SSECursor
  paused?: boolean
}
export type SSEChunkHandler = (type: string, content: string, id?: string) => void
export interface SSEStreamControl {
  /** Settles after cleanup. Errors are delivered through onError, not rejection. */
  finished: Promise<void>
  cursor: SSECursor
  abort: () => void
  /** Pause initial requests/reconnects; an already live tail continues receiving. */
  setPaused: (paused: boolean) => void
}

const delays = [250, 500, 1000, 2000, 4000]
const aborted = () => new SSEError('aborted', 'Stream aborted')
const optionsKeys = ['method', 'body', 'signal', 'runId', 'initialAfterSeq', 'maxRetries', 'onRetry', 'cursor', 'paused']
function isOptions(value: unknown): value is StreamFetchOptions {
  return !!value && typeof value === 'object' && optionsKeys.some(key => key in value)
}
function resumeURL(url: string, seq: number): string {
  const parsed = new URL(url, window.location.href)
  parsed.searchParams.set('after_seq', String(seq))
  return /^[a-z][a-z\d+.-]*:/i.test(url) ? parsed.href : `${parsed.pathname}${parsed.search}${parsed.hash}`
}

/**
 * One fetch/parser for legacy POST text and durable GET JSON tails. POST is never
 * replayed. onDone means transport completion, never an inferred Run success.
 * Specify method for new options callers; positional bodies remain compatible.
 */
export function streamFetch(url: string, options: StreamFetchOptions, onChunk: SSEChunkHandler, onDone: () => void, onError?: (error: SSEError) => void): SSEStreamControl
export function streamFetch(url: string, body: unknown, onChunk: SSEChunkHandler, onDone: () => void, onError?: (error: Error) => void, signal?: AbortSignal): SSEStreamControl
export function streamFetch(
  url: string, bodyOrOptions: unknown, onChunk: SSEChunkHandler, onDone: () => void,
  onError?: (error: SSEError) => void, legacySignal?: AbortSignal,
): SSEStreamControl {
  const legacy = !isOptions(bodyOrOptions) || legacySignal !== undefined
  const options: StreamFetchOptions = legacy ? { body: bodyOrOptions, signal: legacySignal } : bodyOrOptions
  const method = options.method ?? 'POST'
  const durable = method === 'GET'
  const runId = options.runId ?? url
  const cursor = options.cursor ?? new SSECursor()
  const controller = new AbortController()
  let reader: ReadableStreamDefaultReader<Uint8Array> | undefined
  let cancelReader: Promise<void> | undefined
  let timer: ReturnType<typeof setTimeout> | undefined
  let wake: (() => void) | undefined
  let paused = options.paused ?? false
  let stopped = false
  const clearTimer = () => { if (timer !== undefined) clearTimeout(timer); timer = undefined }
  const cancel = () => { if (reader && !cancelReader) cancelReader = reader.cancel().catch(() => {}) }
  const abort = () => { controller.abort(); cancel(); clearTimer(); wake?.() }
  const checkAbort = () => { if (controller.signal.aborted) throw aborted() }
  const setPaused = (next: boolean) => {
    if (stopped || paused === next) return
    paused = next
    clearTimer()
    if (!paused) wake?.()
  }
  const wait = async (delay: number) => {
    checkAbort()
    if (!paused && delay === 0) return
    do {
      await new Promise<void>(resolve => {
        wake = resolve
        if (!paused) timer = setTimeout(resolve, delay)
      })
      clearTimer()
      wake = undefined
      checkAbort()
    } while (paused)
  }
  const emit = (event: string, data: string, id?: string) => {
    try { onChunk(event, data, id) }
    catch (error) { throw new SSEError('consumer', error instanceof Error ? error.message : String(error)) }
  }

  const readResponse = async (response: Response): Promise<boolean> => {
    reader = response.body?.getReader()
    if (!reader) return !durable
    cancelReader = undefined
    const decoder = new TextDecoder()
    let buffer = '', id = '', event = ''
    let data: string[] = []
    const dispatch = (): boolean => {
      checkAbort()
      const content = data.join('\n')
      const frameId = id, frameEvent = event || 'message'
      id = ''; event = ''; data = []
      if (content === '[DONE]' || frameEvent === 'done') return true
      if (frameEvent === 'error') throw new SSEError('protocol', content || 'Server stream error')
      if (!content || frameEvent === 'connected') return false
      let payload: unknown
      if (durable) {
        if (!/^[1-9]\d*$/.test(frameId) || !validSeq(Number(frameId))) throw new SSEError('protocol', 'Invalid durable event cursor')
        try { payload = JSON.parse(content) }
        catch { throw new SSEError('protocol', 'Invalid durable event JSON') }
        if (!payload || typeof payload !== 'object' || Array.isArray(payload)) throw new SSEError('protocol', 'Invalid durable event envelope')
        if (!cursor.accept(runId, Number(frameId))) return false
      }
      emit(frameEvent, content, frameId || undefined)
      if (!durable) return false
      const envelope = payload as { data?: { retryable?: unknown } }
      return frameEvent === 'run.completed' || frameEvent === 'run.parked' ||
        (frameEvent === 'run.failed' && envelope.data?.retryable !== true)
    }
    const line = (value: string): boolean => {
      if (!value) return dispatch()
      if (value.startsWith(':')) return false
      const colon = value.indexOf(':')
      const field = colon < 0 ? value : value.slice(0, colon)
      let content = colon < 0 ? '' : value.slice(colon + 1)
      if (content.startsWith(' ')) content = content.slice(1)
      if (field === 'id') id = content
      if (field === 'event') event = content
      if (field === 'data') data.push(content)
      return false
    }
    try {
      while (true) {
        checkAbort()
        const result = await reader.read()
        checkAbort()
        buffer += result.done ? decoder.decode() : decoder.decode(result.value, { stream: true })
        // A trailing CR may be half a CRLF delimiter, so retain it until next read.
        let match: RegExpExecArray | null
        while ((match = /\r\n|\r|\n/.exec(buffer))) {
          if (!result.done && match[0] === '\r' && match.index === buffer.length - 1) break
          const current = buffer.slice(0, match.index)
          buffer = buffer.slice(match.index + match[0].length)
          if (line(current)) return true
        }
        if (result.done) {
          if (buffer && line(buffer)) return true
          return dispatch() || !durable
        }
      }
    } finally {
      cancel()
      await cancelReader
      reader.releaseLock()
      reader = undefined
      cancelReader = undefined
    }
  }

  const finished = (async () => {
    options.signal?.addEventListener('abort', abort, { once: true })
    if (options.signal?.aborted) abort()
    try {
      if (durable) {
        if (!validSeq(options.initialAfterSeq ?? 0)) throw new SSEError('protocol', 'Invalid initial cursor')
        if (!options.cursor) cursor.reset(runId, options.initialAfterSeq ?? 0)
        else cursor.accept(runId, options.initialAfterSeq ?? 0) // Seed/select without regression.
      }
      const retryLimit = Math.min(5, Math.max(0, Math.floor(options.maxRetries ?? 5)))
      let attempt = 0
      await wait(0)
      while (true) {
        if (paused) await wait(0)
        checkAbort()
        try {
          const token = localStorage.getItem('token')
          const response = await fetch(durable ? resumeURL(url, cursor.lastSeq) : url, {
            method,
            headers: { 'Content-Type': 'application/json', ...(token ? { Authorization: `Bearer ${token}` } : {}) },
            ...(!durable ? { body: JSON.stringify(options.body) } : {}),
            signal: controller.signal,
          })
          if (controller.signal.aborted) {
            await response.body?.cancel().catch(() => {})
            throw aborted()
          }
          if (!response.ok) {
            await response.body?.cancel().catch(() => {})
            throw new SSEError('http', `请求失败（${response.status}）`, response.status)
          }
          if (await readResponse(response)) {
            checkAbort()
            try { onDone() }
            catch (error) { throw new SSEError('consumer', String(error)) }
            return
          }
          throw new SSEError('network', 'Event stream disconnected')
        } catch (error) {
          checkAbort()
          const typed = error instanceof SSEError ? error : new SSEError('network', error instanceof Error ? error.message : String(error))
          const recoverable = typed.code === 'network' || (typed.code === 'http' && (typed.status ?? 0) >= 500)
          if (!durable || !recoverable) throw typed
          if (attempt >= retryLimit || !Number.isFinite(retryLimit)) throw new SSEError('retries_exhausted', typed.message, typed.status)
          const delay = delays[attempt++]
          options.onRetry?.(attempt, delay)
          await wait(delay)
        }
      }
    } catch (error) {
      const typed = error instanceof SSEError ? error : new SSEError('network', String(error))
      if (!(legacy && typed.code === 'aborted')) onError?.(typed)
    } finally {
      stopped = true
      clearTimer()
      wake = undefined
      options.signal?.removeEventListener('abort', abort)
    }
  })()
  return { finished, cursor, abort, setPaused }
}

/** Service adapters may opt in; React callers should use only the hook's binding. */
export function bindSSEVisibility(control: SSEStreamControl): () => void {
  const change = () => control.setPaused(document.visibilityState === 'hidden')
  document.addEventListener('visibilitychange', change)
  change()
  let bound = true
  const unbind = () => {
    if (bound) document.removeEventListener('visibilitychange', change)
    bound = false
  }
  void control.finished.then(unbind, unbind)
  return unbind
}
