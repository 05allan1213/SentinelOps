import api from './api'
import { streamFetch, bindSSEVisibility, SSEError } from '@/utils/sse'
import { useAuthStore } from '@/stores/authStore'
import {
  ApiResponse,
  UploadConfig,
} from '@/types'

export interface DurableRun { run_id: string; session_id: string; status: string }
export interface DurableChatState { kind: 'run' | 'operation' | 'transport'; status: string; error?: string }
interface DurablePayload { summary?: string; data?: { to_status?: string; retryable?: boolean } }
const runStatuses = ['pending', 'running', 'waiting_approval', 'retryable_failed', 'parked', 'reconciling', 'succeeded', 'failed', 'canceled']
function canonicalStatus(status?: string) { return status && runStatuses.includes(status) ? status : undefined }
function runEventStatus(type: string, payload: DurablePayload): string | undefined {
  if (type === 'run.failed') return payload.data?.retryable ? 'retryable_failed' : 'failed'
  return ({ 'run.completed': 'succeeded', 'run.parked': 'parked', 'run.reconciling': 'reconciling', 'run.claimed': 'running', 'run.resumed': 'running', 'run.replayed': 'running', 'approval.requested': 'waiting_approval', 'run.created': 'pending' } as Record<string, string>)[type]
}

// Only in-flight POST promises live here; settled results remain in the existing
// session binding. Returning consumers can await the original request by message.
const pendingCreates = new Map<string, { messageId?: string; promise: Promise<DurableRun> }>()

// 会话接口
export interface ChatSession {
  id: string
  title: string
  createdAt: number
  lastMessageAt: number
  messageCount: number
}

// 生成会话ID
const generateSessionId = () => {
  return Date.now() + '-' + Math.random().toString(36).substr(2, 9)
}

// 获取当前会话ID
const getCurrentSessionId = () => {
  return localStorage.getItem('current_session_id') || ''
}

// 设置当前会话ID
const setCurrentSessionId = (sessionId: string) => {
  localStorage.setItem('current_session_id', sessionId)
}

// 获取所有会话
const getSessions = (): ChatSession[] => {
  const data = localStorage.getItem('chat_sessions')
  return data ? JSON.parse(data) : []
}

// 保存会话列表
const saveSessions = (sessions: ChatSession[]) => {
  localStorage.setItem('chat_sessions', JSON.stringify(sessions))
}

export const chatService = {
  hasPendingCreate(sessionId: string, messageId: string) {
    return pendingCreates.get(sessionId)?.messageId === messageId
  },
  // 获取当前会话ID
  getSessionId: (): string => {
    let sessionId = getCurrentSessionId()
    if (!sessionId) {
      sessionId = generateSessionId()
      setCurrentSessionId(sessionId)
      // 创建新会话
      const sessions = getSessions()
      sessions.unshift({
        id: sessionId,
        title: '新对话',
        createdAt: Date.now(),
        lastMessageAt: Date.now(),
        messageCount: 0,
      })
      saveSessions(sessions)
    }
    return sessionId
  },

  // 获取所有会话
  listSessions: (): ChatSession[] => {
    return getSessions()
  },

  // 创建新会话
  createSession: (): ChatSession => {
    const sessionId = generateSessionId()
    const session: ChatSession = {
      id: sessionId,
      title: '新对话',
      createdAt: Date.now(),
      lastMessageAt: Date.now(),
      messageCount: 0,
    }
    const sessions = getSessions()
    sessions.unshift(session)
    saveSessions(sessions)
    setCurrentSessionId(sessionId)
    return session
  },

  // 切换会话
  switchSession: (sessionId: string) => {
    setCurrentSessionId(sessionId)
  },

  // 删除会话
  deleteSession: (sessionId: string) => {
    const sessions = getSessions().filter(s => s.id !== sessionId)
    saveSessions(sessions)
    if (getCurrentSessionId() === sessionId) {
      const newSession = sessions[0]
      if (newSession) {
        setCurrentSessionId(newSession.id)
      } else {
        localStorage.removeItem('current_session_id')
      }
    }
  },

  // 更新会话信息
  updateSession: (sessionId: string, updates: Partial<ChatSession>) => {
    const sessions = getSessions()
    const index = sessions.findIndex(s => s.id === sessionId)
    if (index !== -1) {
      sessions[index] = { ...sessions[index], ...updates }
      saveSessions(sessions)
    }
  },

  async createDurableRun({ sessionId, query, agent }: { sessionId: string; query: string; agent?: string }) {
    const response = await api.post<ApiResponse<DurableRun>>('/chat/v2/runs', {
      session_id: sessionId, query, ...(agent ? { agent } : {}),
    }, { skipRateLimitRetry: true })
    const run = response.data.data
    if (!run?.run_id || run.session_id !== sessionId) throw new Error('工作流响应与当前会话不匹配')
    return run
  },

  // This adapter only tails an existing authorized Run. D-06 owns parsing,
  // deduplication, bounded retries, visibility and reader cleanup.
  tailDurableRun({ runId, afterSeq = 0, onEvent, onState, onDone = () => {}, onError, signal }: {
    runId: string; afterSeq?: number; onEvent: (type: string, content: string, seq: number) => void
    onState?: (state: DurableChatState) => void; onDone?: () => void
    onError?: (error: Error) => void; signal?: AbortSignal
  }) {
    const control = streamFetch(`/api/chat/v2/runs/${encodeURIComponent(runId)}/events`, {
      method: 'GET', runId, initialAfterSeq: afterSeq, signal,
      onRetry: (attempt) => onState?.({ kind: 'transport', status: 'reconnecting', error: `连接中断，正在重连（${attempt}/5）` }),
    }, (type, content, id) => {
      const payload = JSON.parse(content) as DurablePayload
      const summary = payload.summary ?? ''
      if (type.startsWith('run.') || type === 'approval.requested') {
        const status = payload.data?.to_status
          ? canonicalStatus(payload.data.to_status) ?? 'unknown' : runEventStatus(type, payload)
        if (status) onState?.({ kind: 'run', status,
          ...(['failed', 'retryable_failed', 'canceled', 'parked'].includes(status) ? { error: summary || '工作流尚未成功完成' } : {}),
        })
      } else if (type.startsWith('operation.')) onState?.({ kind: 'operation', status: type })
      onEvent(type, content, Number(id))
    }, onDone, onError)
    bindSSEVisibility(control)
    return control
  },

  // Existing positional callbacks remain compatible. Only an intentional new
  // user turn may opt into creation while a session already has a Run binding.
  async multiAgentChat(
    query: string, _messageIndex: number, _deepThinking: boolean, _webSearch: boolean,
    onMessage: (intent: string, content: string) => void, onDone: () => void,
    onError?: (error: Error) => void, signal?: AbortSignal, sessionId?: string,
    options?: { newTurn?: boolean; messageId?: string },
  ) {
    const sid = sessionId || getCurrentSessionId()
    let runId = sessionStorage.getItem(`chat_run_id_${sid}`) || ''
    const stateKey = `chat_run_state_${sid}`
    const emitState = (state: DurableChatState) => {
      if (state.kind === 'run') sessionStorage.setItem(stateKey, JSON.stringify(state))
      onMessage(`${state.kind}_state`, JSON.stringify(state))
    }
    try {
      if (signal?.aborted) return
      let pending = pendingCreates.get(sid)
      if (pending && (options?.newTurn || (options?.messageId && pending.messageId !== options.messageId))) {
        throw new Error('当前会话的创建请求尚未返回，请等待原工作流身份')
      }
      if (!pending && !runId && options?.newTurn === false) throw new Error('没有可恢复的工作流身份；重试连接不会创建新工作流')
      if (!pending && (!runId || options?.newTurn)) {
        // Retain the accepted message binding even if its original consumer left.
        // No consumer aborts the POST or automatically retries it.
        const promise = chatService.createDurableRun({ sessionId: sid, query }).then(run => {
          sessionStorage.setItem(`chat_run_id_${sid}`, run.run_id)
          sessionStorage.setItem(`chat_last_seq_${sid}`, '0')
          sessionStorage.setItem(stateKey, JSON.stringify({ kind: 'run', status: run.status }))
          if (options?.messageId) sessionStorage.setItem(`chat_run_message_${sid}`, options.messageId)
          else sessionStorage.removeItem(`chat_run_message_${sid}`)
          return run
        }).finally(() => { pendingCreates.delete(sid) })
        pending = { messageId: options?.messageId, promise }
        pendingCreates.set(sid, pending)
      }
      if (pending) runId = (await pending.promise).run_id
      if (signal?.aborted) return
      onMessage('run_binding', runId)
      const savedState = sessionStorage.getItem(stateKey)
      if (savedState) emitState(JSON.parse(savedState) as DurableChatState)
      const savedSeq = Number(sessionStorage.getItem(`chat_last_seq_${sid}`) || '0')
      const control = chatService.tailDurableRun({
        runId, afterSeq: savedSeq, signal, onDone,
        onState: emitState,
        onError: (error) => { if (!(error instanceof SSEError && error.code === 'aborted')) onError?.(error) },
        onEvent: (type, content, seq) => {
          const payload = JSON.parse(content) as DurablePayload
          const summary = payload.summary ?? ''
          if (type === 'agent.plan' && summary) {
            let plan: { response?: string; steps?: string[] } | undefined
            try { plan = JSON.parse(summary) } catch { /* Unstructured planner response. */ }
            if (plan?.response) onMessage('assistant', plan.response)
            else if (Array.isArray(plan?.steps)) onMessage('plan_step', JSON.stringify({ type: 'plan_steps', steps: plan.steps }))
            else onMessage('assistant', summary)
          } else if (type === 'agent.tool_result' && summary) onMessage('tool_result', summary)
          // Consumer delivery (including the page's durable message snapshot)
          // precedes the accepted cursor; a reload cannot skip buffered text.
          sessionStorage.setItem(`chat_last_seq_${sid}`, String(seq))
        },
      })
      await control.finished
    } catch (error) {
      if (!signal?.aborted) onError?.(error instanceof Error ? error : new Error(String(error)))
    }
  },

  // Explicit legacy adapter retains the v1 payload and callback envelope.
  async multiAgentChatV1(
    query: string, messageIndex: number, deepThinking: boolean, webSearch: boolean,
    onMessage: (intent: string, content: string) => void, onDone: () => void,
    onError?: (error: Error) => void, signal?: AbortSignal, sessionId?: string,
  ) {
    const sid = sessionId || getCurrentSessionId()
    let returnedRunId = ''
    let failed = false
    const legacy = streamFetch('/api/chat/v1/chat', {
      query, session_id: sid, message_index: messageIndex, deep_thinking: deepThinking,
      web_search: webSearch, user_id: useAuthStore.getState().userID ?? '',
      run_id: sessionStorage.getItem(`chat_run_id_${sid}`) || '',
      last_seq: Number(sessionStorage.getItem(`chat_last_seq_${sid}`) || '0'),
    }, (type, content) => {
      if (type !== 'run.created') { onMessage(type, content); return }
      const meta = JSON.parse(content) as { runId?: string; sessionId?: string; status?: string; after_seq?: number }
      if (!meta.runId || meta.sessionId !== sid) throw new Error('旧版工作流响应与当前会话不匹配')
      returnedRunId = meta.runId
      sessionStorage.setItem(`chat_run_id_${sid}`, returnedRunId)
      // v1's synthetic identity event ID is not a workflow_events cursor.
      sessionStorage.setItem(`chat_last_seq_${sid}`, String(meta.after_seq ?? 0))
      sessionStorage.setItem(`chat_run_state_${sid}`, JSON.stringify({ kind: 'run', status: meta.status ?? 'unknown' }))
    }, () => {}, (error) => { failed = true; onError?.(error) }, signal)
    await legacy.finished
    if (failed || signal?.aborted) return
    if (returnedRunId) {
      await chatService.multiAgentChat(query, messageIndex, deepThinking, webSearch, onMessage, onDone, onError, signal, sid)
    } else onDone()
  },

  // 文件上传（支持多格式和分块配置）
  async uploadFile(
    file: File,
    config?: UploadConfig,
    onProgress?: (progress: number) => void
  ): Promise<{ file_id: string; filename: string }> {
    const formData = new FormData()
    formData.append('file', file)
    if (config) {
      formData.append('strategy', config.strategy)
      if (config.chunk_size)   formData.append('chunk_size',   String(config.chunk_size))
      if (config.overlap_size) formData.append('overlap_size', String(config.overlap_size))
      if (config.target_chars) formData.append('target_chars', String(config.target_chars))
      if (config.max_chars)    formData.append('max_chars',    String(config.max_chars))
      if (config.min_chars)    formData.append('min_chars',    String(config.min_chars))
    }
    const res = await api.post<ApiResponse<{ fileName: string; filePath: string; fileSize: number }>>(
      '/upload',
      formData,
      {
        headers: {
          'Content-Type': 'multipart/form-data',
        },
        onUploadProgress: (progressEvent) => {
          if (onProgress && progressEvent.total) {
            const progress = Math.round((progressEvent.loaded * 100) / progressEvent.total)
            onProgress(progress)
          }
        },
      }
    )
    const d = res.data.data
    return { file_id: d.filePath || d.fileName, filename: d.fileName }
  },

  // 导出会话快照
  exportSession: (sessionId: string) => {
    window.open(`/api/trace/v1/export_session_snapshot?sessionId=${sessionId}`, '_blank')
  },

  // 回溯会话
  async rollbackSession(sessionId: string, targetIndex: number): Promise<{ success: boolean; removedCount: number }> {
    const res = await api.post<ApiResponse<{ success: boolean; rolledBackTo: number; removedCount: number }>>(
      '/chat/v1/rollback',
      { sessionId, targetIndex }
    )
    return { success: res.data.data.success, removedCount: res.data.data.removedCount }
  },
}
