import api from './api'
import { streamFetch } from '@/utils/sse'
import { useAuthStore } from '@/stores/authStore'
import {
  ApiResponse,
  UploadConfig,
} from '@/types'

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

  // ── 问题三：流式中断恢复能力（前端服务层）──────────────────────────────────
  // 设计思路：
  //   每次发起请求前从 sessionStorage 读取上次的 run_id 和 last_seq。
  //   首次请求两者均为空，后端正常创建新 run 并从 seq=1 开始推送。
  //   断线重连时携带非空的 run_id + last_seq，后端跳过 Agent 重新执行，
  //   直接从 workflow_events 表补发 seq > last_seq 的历史事件。
  //   meta 事件到达时保存 runId（首次请求后端新建 run，runId 在此确定）；
  //   每条事件到达时更新 last_seq，确保游标始终指向最后一条已收到的事件。
  // Intent 意图驱动多 Agent 对话（支持断线重连）
  multiAgentChat: (
    query: string,
    messageIndex: number,
    deepThinking: boolean,
    webSearch: boolean,
    onMessage: (intent: string, content: string) => void,
    onDone: () => void,
    onError?: (e: Error) => void,
    signal?: AbortSignal,
    sessionId?: string
  ) => {
    const sid = sessionId || getCurrentSessionId()
    const runId = sessionStorage.getItem(`chat_run_id_${sid}`) || ''
    const lastSeq = parseInt(sessionStorage.getItem(`chat_last_seq_${sid}`) || '0')
    const userID = useAuthStore.getState().userID ?? ''

    let durableRunId = runId
    let durableStarted = false
    const emittedAssistant = new Set<string>()

    const streamDurableEvents = (activeRunId: string) => {
      if (durableStarted || !activeRunId) return
      durableStarted = true
      const token = localStorage.getItem('token')
      fetch(`/api/chat/v2/runs/${encodeURIComponent(activeRunId)}/events?after_seq=${lastSeq}`, {
        headers: token ? { Authorization: `Bearer ${token}` } : {},
      }).then(async response => {
        if (!response.ok) throw new Error(`请求工作流事件失败（${response.status}）`)
        const reader = response.body?.getReader()
        if (!reader) throw new Error('工作流事件流不可用')
        const decoder = new TextDecoder()
        let buffer = ''
        let eventType = ''
        let eventData = ''
        const flushEvent = () => {
          if (!eventType || !eventData) return false
          let payload: { summary?: string; data?: { agent_name?: string; retryable?: boolean } } = {}
          try { payload = JSON.parse(eventData) } catch { payload = { summary: eventData } }
          const summary = payload.summary ?? ''
          if (eventType === 'agent.plan' && summary) {
            // planner/replanner 的结构化计划进入规划面板；最终 response 回到正文。
            try {
              const parsed = JSON.parse(summary) as { response?: string; steps?: string[] }
              if (parsed.response && !emittedAssistant.has(parsed.response)) {
                emittedAssistant.add(parsed.response)
                onMessage('assistant', parsed.response)
              }
              else if (Array.isArray(parsed.steps)) onMessage('plan_step', JSON.stringify({ type: 'plan_steps', steps: parsed.steps }))
            } catch {
              if (!emittedAssistant.has(summary)) {
                emittedAssistant.add(summary)
                onMessage('assistant', summary)
              }
            }
          } else if (eventType === 'agent.tool_result' && summary) {
            onMessage('tool_result', summary)
          } else if (eventType === 'run.failed') {
            throw new Error(summary || '工作流执行失败')
          }
          return eventType === 'run.completed' || eventType === 'run.parked' || eventType === 'run.failed'
        }
        let terminal = false
        while (!terminal) {
          const { done, value } = await reader.read()
          if (done) break
          buffer += decoder.decode(value, { stream: true })
          const lines = buffer.split('\n')
          buffer = lines.pop() ?? ''
          for (const line of lines) {
            if (line.startsWith('event: ')) eventType = line.slice(7).trim()
            else if (line.startsWith('data: ')) eventData += (eventData ? '\n' : '') + line.slice(6)
            else if (line === '') {
              terminal = flushEvent()
              eventType = ''
              eventData = ''
              if (terminal) break
            }
          }
        }
        onDone()
      }).catch(error => onError?.(error instanceof Error ? error : new Error(String(error))))
    }

    streamFetch(
      '/api/chat/v1/chat',
      {
        query,
        session_id: sid,
        message_index: messageIndex,
        deep_thinking: deepThinking,
        web_search: webSearch,
        user_id: userID,
        run_id: runId,
        last_seq: lastSeq
      },
      (type, content, id) => {
        if (type === 'run.created') {
          try {
            const meta = JSON.parse(content) as { runId?: string }
            if (meta.runId) {
              durableRunId = meta.runId
              sessionStorage.setItem(`chat_run_id_${sid}`, meta.runId)
              streamDurableEvents(meta.runId)
            }
          } catch { /* 忽略无效元数据 */ }
          return
        }
        if (type === 'meta') {
          try {
            const meta = JSON.parse(content) as { runId?: string }
            if (meta.runId) {
              sessionStorage.setItem(`chat_run_id_${sid}`, meta.runId)
            }
          } catch { /* 忽略无法解析的事件 */ }
        }
        if (id) {
          sessionStorage.setItem(`chat_last_seq_${sid}`, id)
        }
        onMessage(type, content)
      },
      () => {
        if (durableRunId) streamDurableEvents(durableRunId)
        else onDone()
      },
      onError,
      signal
    )
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
