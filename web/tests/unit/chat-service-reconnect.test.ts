import { chatService } from '@/services/chat'
import api from '@/services/api'
vi.mock('@/services/api', () => ({ default: { post: vi.fn() } }))
const frame = (seq: number, type: string, summary = '', data = {}) => `id: ${seq}\nevent: ${type}\ndata: ${JSON.stringify({ summary, data })}\n\n`
const response = (body: string) => new Response(body, { headers: { 'Content-Type': 'text/event-stream' } })
beforeEach(() => { localStorage.clear(); sessionStorage.clear(); vi.mocked(api.post).mockReset(); vi.stubGlobal('fetch', vi.fn()); vi.mocked(api.post).mockResolvedValue({ data: { data: { run_id: 'run-1', session_id: 'session', status: 'pending' } } }) })
afterEach(() => { vi.unstubAllGlobals(); vi.useRealTimers() })
function chat(options?: { newTurn: boolean }) {
  const message = vi.fn(), done = vi.fn(), error = vi.fn()
  const finished = chatService.multiAgentChat('question', 1, false, false, message, done, error, undefined, 'session', options)
  return { message, done, error, finished }
}
it('creates once via Axios v2 and deduplicates body, plan and tool with monotonic seq', async () => {
  vi.mocked(fetch).mockResolvedValue(response(frame(1, 'agent.plan', '{"steps":["inspect"]}') + frame(1, 'agent.plan', '{"steps":["inspect"]}') + frame(3, 'agent.plan', '{"response":"retained"}') + frame(2, 'agent.plan', '{"response":"stale"}') + frame(4, 'agent.tool_result', 'tool evidence') + frame(4, 'agent.tool_result', 'tool evidence') + frame(5, 'run.completed', '', { to_status: 'succeeded' })))
  const call = chat(); await call.finished
  expect(api.post).toHaveBeenCalledTimes(1)
  expect(vi.mocked(api.post).mock.calls[0].slice(0, 2)).toEqual(['/chat/v2/runs', { session_id: 'session', query: 'question' }])
  expect(fetch).toHaveBeenCalledWith('/api/chat/v2/runs/run-1/events?after_seq=0', expect.objectContaining({ method: 'GET' }))
  expect(call.message.mock.calls.filter(([type]) => type === 'assistant')).toEqual([['assistant', 'retained']])
  expect(call.message.mock.calls.filter(([type]) => type === 'plan_step')).toHaveLength(1)
  expect(call.message.mock.calls.filter(([type]) => type === 'tool_result')).toHaveLength(1)
  expect(sessionStorage.getItem('chat_last_seq_session')).toBe('5')
  expect(call.done).toHaveBeenCalledOnce()
})
it('reload tails stored cursor and never replaces an unauthorized stored run', async () => {
  sessionStorage.setItem('chat_run_id_session', 'stored'); sessionStorage.setItem('chat_last_seq_session', '12')
  vi.mocked(fetch).mockResolvedValue(new Response('', { status: 403 }))
  const call = chat(); await call.finished
  expect(api.post).not.toHaveBeenCalled()
  expect(fetch).toHaveBeenCalledWith('/api/chat/v2/runs/stored/events?after_seq=12', expect.anything())
  expect(call.error).toHaveBeenCalledOnce()
  expect(sessionStorage.getItem('chat_run_id_session')).toBe('stored')
})
it.each(['succeeded', 'failed', 'canceled', 'retryable_failed', 'parked', 'reconciling'])('maps server %s without treating operation success as Run success', async (status) => {
  const type = status === 'succeeded' ? 'run.completed' : status === 'parked' ? 'run.parked' : status === 'reconciling' ? 'run.reconciling' : 'run.failed'
  vi.mocked(fetch).mockResolvedValue(response(frame(1, 'operation.completed', '', { to_status: 'succeeded' }) + frame(2, type, 'server detail', { to_status: status, retryable: status === 'retryable_failed' }) + 'data: [DONE]\n\n'))
  const call = chat(); await call.finished
  const states = call.message.mock.calls.filter(([type]) => type === 'run_state').map(([, value]) => JSON.parse(value).status)
  expect(states).toEqual(['pending', status])
})
it('DONE-only cannot invent success; an intentional next turn creates a new Run', async () => {
  sessionStorage.setItem('chat_run_id_session', 'old'); sessionStorage.setItem('chat_last_seq_session', '9')
  vi.mocked(fetch).mockImplementation(async () => response('data: [DONE]\n\n'))
  const reconnect = chat(); await reconnect.finished
  expect(api.post).not.toHaveBeenCalled()
  expect(reconnect.message.mock.calls.some(([type, value]) => type === 'run_state' && JSON.parse(value).status === 'succeeded')).toBe(false)
  await chat({ newTurn: true }).finished
  expect(api.post).toHaveBeenCalledTimes(1)
  expect(sessionStorage.getItem('chat_last_seq_session')).toBe('0')
})
it('retries only GET with accepted cursor after disconnect and retains delivered body on error', async () => {
  vi.useFakeTimers()
  vi.mocked(fetch).mockResolvedValueOnce(response(frame(1, 'agent.plan', '{"response":"retained"}'))).mockResolvedValue(new Response('', { status: 403 }))
  const call = chat(); await vi.runAllTimersAsync(); await call.finished
  expect(api.post).toHaveBeenCalledTimes(1)
  expect(vi.mocked(fetch).mock.calls[1][0]).toContain('after_seq=1')
  expect(call.message).toHaveBeenCalledWith('assistant', 'retained')
  expect(call.error).toHaveBeenCalledOnce()
})
it('retains old binding on failed deliberate create and never tails a replacement', async () => {
  sessionStorage.setItem('chat_run_id_session', 'old'); sessionStorage.setItem('chat_last_seq_session', '9')
  vi.mocked(api.post).mockRejectedValue(new Error('409 active run'))
  const call = chat({ newTurn: true }); await call.finished
  expect(sessionStorage.getItem('chat_run_id_session')).toBe('old')
  expect(sessionStorage.getItem('chat_last_seq_session')).toBe('9')
  expect(fetch).not.toHaveBeenCalled()
  expect(call.error).toHaveBeenCalledOnce()
})
it('retains an accepted identity after unmount during create without tailing or delivering UI callbacks', async () => {
  let accept!: (value: unknown) => void
  vi.mocked(api.post).mockImplementation(() => new Promise(resolve => { accept = resolve }))
  const controller = new AbortController(), message = vi.fn(), done = vi.fn()
  const finished = chatService.multiAgentChat('question', 1, false, false, message, done, vi.fn(), controller.signal, 'session')
  controller.abort()
  accept({ data: { data: { run_id: 'accepted', session_id: 'session', status: 'pending' } } })
  await finished
  expect(sessionStorage.getItem('chat_run_id_session')).toBe('accepted')
  expect(message).not.toHaveBeenCalled(); expect(done).not.toHaveBeenCalled(); expect(fetch).not.toHaveBeenCalled()
})
it('keeps legacy v1 an explicit one-shot adapter with existing request compatibility', async () => {
  sessionStorage.setItem('chat_run_id_session', 'legacy'); sessionStorage.setItem('chat_last_seq_session', '8')
  vi.mocked(fetch).mockResolvedValue(response('event: assistant\ndata: plain text\n\nevent: done\ndata: complete\n\n'))
  const message = vi.fn()
  const finished = chatService.multiAgentChatV1('legacy query', 3, true, true, message, vi.fn(), vi.fn(), undefined, 'session')
  await finished
  expect(api.post).not.toHaveBeenCalled()
  const [url, request] = vi.mocked(fetch).mock.calls[0]
  expect(url).toBe('/api/chat/v1/chat')
  expect(JSON.parse(request?.body as string)).toMatchObject({ query: 'legacy query', run_id: 'legacy', last_seq: 8, message_index: 3, deep_thinking: true, web_search: true })
  expect(message).toHaveBeenCalledWith('assistant', 'plain text')
})
it('exposes canonical state directly from the public GET tail adapter', async () => {
  vi.mocked(fetch).mockResolvedValue(response(frame(1, 'run.reconciling', '', { to_status: 'reconciling' }) + frame(2, 'run.completed', '', { to_status: 'unknown' })))
  const state = vi.fn()
  await chatService.tailDurableRun({ runId: 'tail-only', onEvent: vi.fn(), onState: state }).finished
  expect(state.mock.calls.map(([value]) => value.status)).toEqual(['reconciling', 'unknown'])
  expect(api.post).not.toHaveBeenCalled()
})
it('tails the explicit v1 identity through v2 without a second create or accepting its synthetic ID', async () => {
  vi.mocked(fetch).mockResolvedValueOnce(response('id: 1\nevent: run.created\ndata: {"runId":"v1-run","sessionId":"session","status":"pending","after_seq":0}\n\ndata: [DONE]\n\n'))
    .mockResolvedValueOnce(response(frame(1, 'agent.plan', '{"response":"v1 continued"}') + frame(2, 'run.completed', '', { to_status: 'succeeded' })))
  const message = vi.fn(), done = vi.fn()
  await chatService.multiAgentChatV1('q', 1, false, false, message, done, vi.fn(), undefined, 'session')
  expect(api.post).not.toHaveBeenCalled()
  expect(vi.mocked(fetch).mock.calls.map(([url]) => url)).toEqual(['/api/chat/v1/chat', '/api/chat/v2/runs/v1-run/events?after_seq=0'])
  expect(message).toHaveBeenCalledWith('assistant', 'v1 continued')
  expect(sessionStorage.getItem('chat_last_seq_session')).toBe('2')
  expect(done).toHaveBeenCalledOnce()
})
it('an explicit resume never creates when its stored identity is missing', async () => {
  const call = chat({ newTurn: false }); await call.finished
  expect(api.post).not.toHaveBeenCalled()
  expect(fetch).not.toHaveBeenCalled()
  expect(call.error).toHaveBeenCalledOnce()
})
it('maps real claim, approval publication and recovery envelopes without invented to_status', async () => {
  // claim.go, approval_lifecycle.go and recovery.go write these data fields.
  vi.mocked(fetch).mockResolvedValue(response(
    frame(1, 'run.claimed', '', { owner: 'worker', lease_generation: 1, attempt: 1 }) +
    frame(2, 'approval.requested', '', { approval_id: 'a', proposal_hash: 'hash', checkpoint_id: 'cp', checkpoint_payload_sha256: 'sha', checkpoint_lease_generation: 1 }) +
    frame(3, 'run.resumed', '', { mode: 'resume', attempt: 2, lease_generation: 2, runtime_version: 'v1' }) +
    frame(4, 'run.replayed', '', { mode: 'replay', attempt: 3, lease_generation: 3, runtime_version: 'v1' }) + 'data: [DONE]\n\n'))
  const state = vi.fn()
  await chatService.tailDurableRun({ runId: 'real-events', onEvent: vi.fn(), onState: state }).finished
  expect(state.mock.calls.map(([value]) => value.status)).toEqual(['running', 'waiting_approval', 'running', 'running'])
})
it('a current consumer joins an unresolved create after the original consumer leaves', async () => {
  let accept!: (value: unknown) => void
  vi.mocked(api.post).mockImplementation(() => new Promise(resolve => { accept = resolve }))
  vi.mocked(fetch).mockResolvedValue(response('data: [DONE]\n\n'))
  const obsolete = new AbortController(), oldMessage = vi.fn(), currentMessage = vi.fn(), error = vi.fn()
  const first = chatService.multiAgentChat('q', 1, false, false, oldMessage, vi.fn(), error, obsolete.signal, 'session', { newTurn: true, messageId: 'assistant' })
  obsolete.abort()
  const second = chatService.multiAgentChat('', 1, false, false, currentMessage, vi.fn(), error, undefined, 'session', { newTurn: false, messageId: 'assistant' })
  accept({ data: { data: { run_id: 'accepted', session_id: 'session', status: 'pending' } } })
  await Promise.all([first, second])
  expect(api.post).toHaveBeenCalledOnce()
  expect(fetch).toHaveBeenCalledOnce()
  expect(currentMessage).toHaveBeenCalledWith('run_binding', 'accepted')
  expect(oldMessage).not.toHaveBeenCalled()
  expect(error).not.toHaveBeenCalled()
})
