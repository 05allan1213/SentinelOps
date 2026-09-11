import { useState, useRef, useEffect, useLayoutEffect, useCallback } from 'react'
import { useNavigate, useLocation } from 'react-router-dom'
import { ChevronDown, ChevronRight, Loader2, ListChecks, ThumbsUp, ThumbsDown, Pencil, Copy, Check, Zap, BarChart2, FileText, Shield, Globe, BookOpen, Clock, Brain, Lightbulb } from 'lucide-react'
import { cn } from '@/utils'
import { MarkdownRenderer } from '@/components/markdown'
import { chatService, ChatSession } from '@/services'
import type { DurableChatState } from '@/services/chat'
import { ragevalService } from '@/services/rageval'
import { useContextStore } from '@/stores/contextStore'
import SessionList from './components/SessionList'
import WelcomeScreen from './components/WelcomeScreen'
import ChatInput from './components/ChatInput'
import { useStreamRenderScheduler } from './useStreamRenderScheduler'

type MessageRole = 'user' | 'assistant'

interface Message {
  id: string
  role: MessageRole
  content: string
  timestamp: Date
  isStreaming?: boolean
  agentStatus?: string
  runId?: string
  createUnconfirmed?: boolean
  runStatus?: string
  operationStatus?: string
  streamError?: string
  // Plan Agent 规划过程字段
  planning?: string          // 中间步骤内容（plan_step 事件累积，不含 think 类型）
  isPlanRunning?: boolean    // true = 规划执行中
  planDone?: boolean         // true = 规划结束，最终答案已到达
  planStartAt?: number       // 首个 plan_step（非 think）到达时的时间戳
  planDuration?: number      // 规划用时（秒）
  // 预思考字段（think 事件）
  thinking?: string          // 预思考阶段内容（think 事件累积）
  isThinking?: boolean       // true = think 事件流式中
  thinkDone?: boolean        // true = 思考阶段结束
  thinkStartAt?: number      // 思考开始时间戳
  thinkDuration?: number     // 思考用时（秒）
}

// Snapshots written before D-07 recorded a pending assistant turn only as
// `isStreaming: true` with no Run identity. Promote that legacy signal to the
// explicit `createUnconfirmed` marker, so restored history keeps saying the
// creation outcome is unknown instead of looking like an empty finished answer.
// It also runs before switching away, where clearing the streaming flag would
// otherwise erase the only record of the unresolved create.
const migrateLegacyPending = (m: Message): Message =>
  m.role === 'assistant' && m.isStreaming === true && !m.runId && m.createUnconfirmed === undefined
    ? { ...m, createUnconfirmed: true }
    : m

export default function Chat() {
  const [sessions, setSessions] = useState<ChatSession[]>([])
  const [currentSessionId, setCurrentSessionId] = useState('')
  const [messages, setMessages] = useState<Message[]>([])
  const [input, setInput] = useState('')
  const [isLoading, setIsLoading] = useState(false)
  const [deepThinking, setDeepThinking] = useState(() => localStorage.getItem('chat_deep_thinking') === 'true')
  const [webSearch, setWebSearch] = useState(() => localStorage.getItem('chat_web_search') === 'true')
  // 记录每条消息的点赞/踩状态（key = message index）
  const [votes, setVotes] = useState<Record<number, 1 | -1>>({})
  // 编辑状态：记录正在编辑的消息索引和编辑内容
  const [editingIndex, setEditingIndex] = useState<number | null>(null)
  const [editingContent, setEditingContent] = useState('')

  const chatScrollRef = useRef<HTMLDivElement>(null)
  const messagesEndRef = useRef<HTMLDivElement>(null)
  const { schedule, finish } = useStreamRenderScheduler()
  const followBottomRef = useRef(true)
  const messageContentRef = useRef<HTMLDivElement>(null)
  const abortControllerRef = useRef<AbortController | null>(null)
  // 记录当前"正在 loading"的消息 ID，防止旧 stream 的 onDone 错误清除新 stream 的 loading 状态
  const loadingStreamRef = useRef<string | null>(null)

  const { currentEventId, currentEventTitle, setContext } = useContextStore()
  const navigate = useNavigate()
  const location = useLocation()

  // ── 初始化 ───────────────────────────────────────────────────────────────
  useEffect(() => {
    const loaded = chatService.listSessions()
    if (loaded.length === 0) {
      const s = chatService.createSession()
      setSessions([s])
      setCurrentSessionId(s.id)
    } else {
      setSessions(loaded)
      const sid = chatService.getSessionId()
      setCurrentSessionId(sid)
      loadMessages(sid)
    }
  }, [])

  useLayoutEffect(() => {
    if (chatScrollRef.current && followBottomRef.current) {
      chatScrollRef.current.scrollTop = chatScrollRef.current.scrollHeight
    }
  }, [messages])

  useEffect(() => {
    const content = messageContentRef.current
    if (!content || typeof ResizeObserver === 'undefined') return
    const observer = new ResizeObserver(() => {
      if (chatScrollRef.current && followBottomRef.current) {
        chatScrollRef.current.scrollTop = chatScrollRef.current.scrollHeight
      }
    })
    observer.observe(content)
    return () => observer.disconnect()
  }, [messages.length])

  // 从事件列表跳转：直接发送消息
  useEffect(() => {
    const state = location.state as { query?: string } | null
    if (!state?.query) return
    navigate(location.pathname, { replace: true, state: null })
    handleSend(state.query)
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [location.state])

  // Disconnecting a browser tail never cancels the backend Run.
  useEffect(() => () => {
    abortControllerRef.current?.abort()
    loadingStreamRef.current = null
  }, [])


  // ── 会话管理 ─────────────────────────────────────────────────────────────
  const loadMessages = (sid: string) => {
    const raw = localStorage.getItem(`chat_messages_${sid}`)
    const saved = ((raw ? JSON.parse(raw) : []) as Message[]).map(migrateLegacyPending)
    const acceptedMessageId = sessionStorage.getItem(`chat_run_message_${sid}`)
    const acceptedRunId = sessionStorage.getItem(`chat_run_id_${sid}`)
    const restored = saved.map(m => {
      const accepted = m.createUnconfirmed && m.id === acceptedMessageId && acceptedRunId
      return { ...m, isStreaming: false, isThinking: false, isPlanRunning: false,
        ...(accepted ? { runId: acceptedRunId, createUnconfirmed: false, streamError: undefined }
          : m.createUnconfirmed ? { streamError: '创建结果尚未确认，当前消息没有可恢复的工作流身份。请核对会话后再决定是否重新发送。' } : {}),
      }
    })
    saveMessages(sid, restored)
    setMessages(restored)
    // 恢复输入框草稿
    setInput(localStorage.getItem(`chat_draft_${sid}`) ?? '')
    // 恢复点赞/踩状态
    const votesRaw = localStorage.getItem(`chat_votes_${sid}`)
    setVotes(votesRaw ? JSON.parse(votesRaw) : {})
  }

  const saveMessages = (sid: string, msgs: Message[]) => {
    localStorage.setItem(`chat_messages_${sid}`, JSON.stringify(msgs))
  }

  // 持久化深度思考 / 联网搜索开关（跨会话、跨刷新保持用户选择）
  useEffect(() => {
    localStorage.setItem('chat_deep_thinking', String(deepThinking))
  }, [deepThinking])

  useEffect(() => {
    localStorage.setItem('chat_web_search', String(webSearch))
  }, [webSearch])

  // 持久化输入框草稿
  useEffect(() => {
    if (!currentSessionId) return
    if (input) {
      localStorage.setItem(`chat_draft_${currentSessionId}`, input)
    } else {
      localStorage.removeItem(`chat_draft_${currentSessionId}`)
    }
  }, [input, currentSessionId])

  // 持久化点赞/踩状态
  useEffect(() => {
    if (!currentSessionId) return
    if (Object.keys(votes).length > 0) {
      localStorage.setItem(`chat_votes_${currentSessionId}`, JSON.stringify(votes))
    }
  }, [votes, currentSessionId])

  const handleSelectSession = (sid: string) => {
    abortControllerRef.current?.abort()
    // 将进行中的流式消息标为完成后保存，防止切换回来时看到"卡住"的 streaming 状态
    const finalMessages = (JSON.parse(localStorage.getItem(`chat_messages_${currentSessionId}`) || '[]') as Message[])
      .map(migrateLegacyPending)
      .map(m =>
        m.isStreaming ? { ...m, isStreaming: false, agentStatus: undefined, isPlanRunning: false, isThinking: false } : m
      )
    saveMessages(currentSessionId, finalMessages)
    loadingStreamRef.current = null  // 旧 stream 的 onDone 不再清除 loading
    setCurrentSessionId(sid)
    setIsLoading(false)
    chatService.switchSession(sid)
    loadMessages(sid)
  }

  const handleNewSession = () => {
    if (currentSessionId && messages.length === 0) return
    abortControllerRef.current?.abort()
    const finalMessages = (JSON.parse(localStorage.getItem(`chat_messages_${currentSessionId}`) || '[]') as Message[])
      .map(migrateLegacyPending)
      .map(m =>
        m.isStreaming ? { ...m, isStreaming: false, agentStatus: undefined, isPlanRunning: false, isThinking: false } : m
      )
    saveMessages(currentSessionId, finalMessages)
    loadingStreamRef.current = null
    const s = chatService.createSession()
    setSessions([s, ...sessions])
    setCurrentSessionId(s.id)
    setMessages([])
    setInput('')
    setVotes({})
    setIsLoading(false)
    setDeepThinking(false)
    setWebSearch(false)
    setContext('chat')
  }

  const handleDeleteSession = (sid: string) => {
    if (sid === currentSessionId) abortControllerRef.current?.abort()
    chatService.deleteSession(sid)
    localStorage.removeItem(`chat_messages_${sid}`)
    localStorage.removeItem(`chat_draft_${sid}`)
    localStorage.removeItem(`chat_votes_${sid}`)
    const updated = sessions.filter((s) => s.id !== sid)
    setSessions(updated)
    if (sid === currentSessionId) {
      if (updated.length > 0) handleSelectSession(updated[0].id)
      else { setCurrentSessionId(''); setMessages([]) }
    }
  }

  const handleExportSession = (sid: string) => {
    chatService.exportSession(sid)
  }

  // 开始编辑消息
  const handleStartEdit = (index: number, content: string) => {
    setEditingIndex(index)
    setEditingContent(content)
  }

  // 取消编辑
  const handleCancelEdit = () => {
    setEditingIndex(null)
    setEditingContent('')
  }

  // 保存编辑（保留该消息，删除之后的 assistant 回复，重新发送）
  const handleSaveEdit = async (index: number) => {
    if (!currentSessionId || !editingContent.trim()) return

    // 保留 index 之前的消息（不含当前 user 消息），删除 index 及之后的所有消息
    const newMessages = messages.slice(0, index)
    setMessages(newMessages)
    saveMessages(currentSessionId, newMessages)

    // 清空编辑状态和投票记录
    setEditingIndex(null)
    setEditingContent('')
    const newVotes: Record<number, 1 | -1> = {}
    Object.keys(votes).forEach(k => {
      const idx = parseInt(k)
      if (idx < index) newVotes[idx] = votes[idx]
    })
    setVotes(newVotes)
    localStorage.setItem(`chat_votes_${currentSessionId}`, JSON.stringify(newVotes))

    // 重置加载状态，确保 handleSend 不被 isLoading 拦截
    setIsLoading(false)
    loadingStreamRef.current = null
    abortControllerRef.current?.abort()
    abortControllerRef.current = null

    // 调用后端回滚API（不阻塞发送）
    chatService.rollbackSession(currentSessionId, index - 1).catch(err => {
      console.error('[编辑] 回滚失败:', err)
    })

    // 重新发送编辑后的消息（force=true 跳过 isLoading 检查）
    handleSend(editingContent.trim(), true, newMessages)
  }

  // ── 发送消息（overrideText 来自技能卡片或事件跳转；force=true 跳过 isLoading 检查，用于编辑回溯） ──────────────────────────
  const handleSend = async (overrideText?: string, force?: boolean, previousMessages?: Message[]) => {
    const history = previousMessages ?? messages
    const rawContent = overrideText !== undefined ? overrideText : input
    if (!rawContent.trim() || (isLoading && !force)) return

    let sid = currentSessionId
    if (!sid) {
      const s = chatService.createSession()
      setSessions([s, ...sessions])
      setCurrentSessionId(s.id)
      sid = s.id
    }

    let messageContent = rawContent.trim()
    if (currentEventId && currentEventTitle) {
      messageContent = `[当前上下文: 事件#${currentEventId} "${currentEventTitle}"]\n${messageContent}`
    }

    const userMessage: Message = {
      id: genId(),
      role: 'user',
      content: rawContent.trim(),
      timestamp: new Date(),
    }
    setMessages((prev) => [...prev, userMessage])
    if (overrideText === undefined) setInput('')
    setIsLoading(true)

    // 更新会话标题
    if (history.length === 0) {
      const title = rawContent.trim().slice(0, 30) + (rawContent.trim().length > 30 ? '...' : '')
      chatService.updateSession(sid, { title })
      setSessions((prev) => prev.map((s) => (s.id === sid ? { ...s, title } : s)))
    }
    chatService.updateSession(sid, { lastMessageAt: userMessage.timestamp.getTime(), messageCount: history.length + 1 })
    setSessions((prev) =>
      prev.map((s) =>
        s.id === sid ? { ...s, lastMessageAt: userMessage.timestamp.getTime(), messageCount: history.length + 1 } : s
      )
    )

    const assistantMessage: Message = {
      id: genId(),
      role: 'assistant',
      content: '',
      timestamp: new Date(),
      isStreaming: true,
      createUnconfirmed: true,
    }
    setMessages((prev) => [...prev, assistantMessage])

    saveMessages(sid, [...history, userMessage, assistantMessage])
    startAssistantStream(sid, assistantMessage, messageContent, history.length + 1, true)
  }

  const startAssistantStream = (sid: string, assistantMessage: Message, messageContent: string, messageIndex: number, newTurn: boolean) => {
    setIsLoading(true)
    setMessages(prev => prev.map(m => m.id === assistantMessage.id ? { ...m, isStreaming: true, streamError: undefined } : m))
    // Each tail owns one message ID and retains its previously accepted content.
    let streamingContent = assistantMessage.content
    let streamingThinking = assistantMessage.thinking ?? ''
    followBottomRef.current = true

    // 中止上一个请求（如有），新建控制器
    abortControllerRef.current?.abort()
    const abortCtrl = new AbortController()
    abortControllerRef.current = abortCtrl

    // 捕获此次 stream 归属（用于 onDone 写入正确的 localStorage key）
    const capturedSessionId = sid
    const capturedMessageId = assistantMessage.id
    loadingStreamRef.current = capturedMessageId

    // Retain event order inside the existing message budget. Coalescing only
    // the latest callback per field would reorder interleaved phase changes or
    // lose planning events; the scheduled callback applies this batch once.
    let pendingMessageUpdates: Array<(message: Message) => Message> = []
    const persistUpdate = (update: (message: Message) => Message) => {
      const saved = JSON.parse(localStorage.getItem(`chat_messages_${capturedSessionId}`) || '[]') as Message[]
      saveMessages(capturedSessionId, saved.map(m => m.id === capturedMessageId ? update(m) : m))
    }
    const queueMessageUpdate = (update: (message: Message) => Message) => {
      persistUpdate(update)

      const scheduled = schedule(capturedMessageId, 'message', () => {
        const updates = pendingMessageUpdates
        pendingMessageUpdates = []
        setMessages((prev) => prev.map((m) => m.id === capturedMessageId
          ? updates.reduce((message, apply) => apply(message), m) : m))
      })
      // A closing tail cannot retain render batches after unmount.
      if (scheduled) pendingMessageUpdates.push(update)
    }

    const queueContent = () => {
      const content = streamingContent
      const receivedAt = Date.now()
      queueMessageUpdate((m) => {
        if (m.content === content) return m
        const planDuration = m.planStartAt && m.isPlanRunning
          ? Math.round((receivedAt - m.planStartAt) / 1000) : undefined
        return { ...m, content, isPlanRunning: false,
          planDone: m.isPlanRunning ? true : m.planDone,
          planDuration: m.planDuration ?? planDuration }
      })
    }

    try {
      // v2 uses the server agent configuration; legacy callback arguments remain compatible.
      chatService.multiAgentChat(
        messageContent,
        messageIndex,
        deepThinking,
        webSearch,
        (agent, content) => {
          if (agent === 'run_binding') {
            queueMessageUpdate(m => ({ ...m, runId: content, createUnconfirmed: false, streamError: undefined }))
          } else if (agent === 'run_state' || agent === 'operation_state' || agent === 'transport_state') {
            const state = JSON.parse(content) as DurableChatState
            queueMessageUpdate(m => state.kind === 'run'
              ? { ...m, runStatus: state.status, streamError: state.error }
              : state.kind === 'operation' ? { ...m, operationStatus: state.status }
              : { ...m, streamError: state.error })
          } else if (agent === 'plan_step') {
            // 解析事件类型：think = 预思考，其他 = 规划执行
            let eventType = ''
            try {
              const parsed = JSON.parse(content) as { type?: string }
              eventType = parsed.type ?? ''
            } catch { /* 忽略无法解析的事件 */ }

            if (eventType === 'think') {
              // 思考片段：追加到 thinking 字段，标记思考中
              const chunkContent = (() => {
                try { return (JSON.parse(content) as { content?: string }).content ?? content }
                catch { return content }
              })()
              streamingThinking += chunkContent
              const thinkStartAt = Date.now()
              const thinking = streamingThinking
              queueMessageUpdate((m) => ({
                ...m, thinking, isThinking: true, thinkStartAt: m.thinkStartAt ?? thinkStartAt,
              }))
            } else {
              // Nonterminal phase events share the same timer/RAF as text.
              const receivedAt = Date.now()
              queueMessageUpdate((m) => {
                const thinkDuration = m.thinkStartAt && m.isThinking
                  ? Math.round((receivedAt - m.thinkStartAt) / 1000) : undefined
                return {
                  ...m,
                  isThinking: false,
                  thinkDone: m.isThinking ? true : m.thinkDone,
                  thinkDuration: m.thinkDuration ?? thinkDuration,
                  planning: (m.planning ? m.planning + '\n\n' : '') + content,
                  isPlanRunning: true,
                  planStartAt: m.planStartAt ?? receivedAt,
                }
              })
            }
          } else if (agent === 'status') {
            setMessages((prev) =>
              prev.map((m) =>
                m.id === assistantMessage.id ? { ...m, agentStatus: content } : m
              )
            )
          } else if (agent === 'meta') {
            try {
              const meta = JSON.parse(content) as Record<string, string>
              if (meta.sessionId && !meta.deepThinking) {
                setMessages((prev) =>
                  prev.map((m) =>
                    m.id === assistantMessage.id
                      ? { ...m, agentStatus: '多智能体处理中...' }
                      : m
                  )
                )
              }
            } catch { /* 忽略无法解析的事件 */ }
          } else {
            if (content) {
              streamingContent += content
              queueContent()
            }
          }
        },
        () => {
          finish(capturedMessageId)
          setMessages((prev) =>
            prev.map((m) =>
              m.id === capturedMessageId
                ? {
                    ...m,
                    content: streamingContent,
                    isStreaming: false,
                    isPlanRunning: false,
                    isThinking: false,
                    planDone: (m.planning?.length ?? 0) > 0 ? true : m.planDone,
                    thinkDone: (m.thinking?.length ?? 0) > 0 ? true : m.thinkDone,
                    agentStatus: undefined,
                  }
                : m
            )
          )
          if (loadingStreamRef.current === capturedMessageId) {
            setIsLoading(false)
            loadingStreamRef.current = null
          }
          try {
            const raw = localStorage.getItem(`chat_messages_${capturedSessionId}`)
            if (raw) {
              const msgs = JSON.parse(raw) as Message[]
              const finalContent = streamingContent
              const updated = msgs.map(m =>
                m.id === capturedMessageId
                  ? { ...m, content: finalContent, isStreaming: false, isThinking: false, isPlanRunning: false, agentStatus: undefined }
                  : m
              )
              localStorage.setItem(`chat_messages_${capturedSessionId}`, JSON.stringify(updated))
            }
          } catch { /* 忽略无法解析的事件 */ }
        },
        (err) => {
          finish(capturedMessageId)
          const failed = (m: Message): Message => ({ ...m, content: streamingContent, isStreaming: false,
            isThinking: false, isPlanRunning: false, agentStatus: undefined,
            streamError: m.runId ? `连接失败：${err.message}。可重试连接以继续读取原工作流。`
              : `创建结果尚未确认：${err.message}。请核对会话后再决定是否重新发送。` })
          persistUpdate(failed)
          setMessages(prev => prev.map(m => m.id === capturedMessageId ? failed(m) : m))
          if (loadingStreamRef.current === capturedMessageId) {
            setIsLoading(false)
            loadingStreamRef.current = null
          }
        },
        abortCtrl.signal,
        sid,
        { newTurn, messageId: capturedMessageId }
      )
    } catch {
      finish(capturedMessageId)
      setMessages((prev) => prev.map((m) => m.id === capturedMessageId
        ? { ...m, content: streamingContent || '请求失败，请重试', isStreaming: false, isThinking: false, isPlanRunning: false, agentStatus: undefined } : m))
      if (loadingStreamRef.current === capturedMessageId) {
        loadingStreamRef.current = null
        setIsLoading(false)
      }
    }
  }

  useEffect(() => {
    let disposed = false
    void Promise.resolve().then(() => {
      if (disposed || !currentSessionId) return
      const runId = sessionStorage.getItem(`chat_run_id_${currentSessionId}`)
      const acceptedMessageId = sessionStorage.getItem(`chat_run_message_${currentSessionId}`)
      const saved = JSON.parse(localStorage.getItem(`chat_messages_${currentSessionId}`) || '[]') as Message[]
      const latestTurn = [...saved].reverse().find(m => m.role === 'assistant' && (m.createUnconfirmed !== undefined || m.runId))
      let assistant: Message | undefined
      if (latestTurn?.createUnconfirmed) {
        if (!chatService.hasPendingCreate(currentSessionId, latestTurn.id) && !(runId && acceptedMessageId === latestTurn.id)) return
        assistant = latestTurn
      } else {
        if (!runId) return
        assistant = saved.find(m => m.id === acceptedMessageId)
          ?? [...saved].reverse().find(m => m.role === 'assistant' && m.runId === runId)
          ?? [...saved].reverse().find(m => m.role === 'assistant' && !m.runId && m.createUnconfirmed === undefined)
      }
      if (!assistant) {
        assistant = { id: genId(), role: 'assistant', content: '', timestamp: new Date(), runId: runId ?? undefined }
        saved.push(assistant)
        saveMessages(currentSessionId, saved)
        setMessages(saved)
      }
      startAssistantStream(currentSessionId, assistant, '', saved.indexOf(assistant), false)
    })
    return () => { disposed = true }
    // Session selection owns this tail; message renders must not reconnect it.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [currentSessionId])

  // ── 文件上传成功回调 ──────────────────────────────────────────────────────
  const handleFileUploadSuccess = () => {
    const accepted = JSON.parse(localStorage.getItem(`chat_messages_${currentSessionId}`) || '[]') as Message[]
    // Flush queued transforms before replacing rendered messages with the
    // accepted snapshot, otherwise an already accepted plan could append twice.
    if (loadingStreamRef.current) finish(loadingStreamRef.current)
    const updated: Message[] = [...accepted, { id: genId(), role: 'assistant', content: '文件已上传到知识库', timestamp: new Date() }]
    saveMessages(currentSessionId, updated)
    setMessages(updated)
  }

  // ── 消息反馈 ──────────────────────────────────────────────────────────────
  const submitFeedback = async (messageIndex: number, vote: 1 | -1, reasons?: string[]) => {
    if (!currentSessionId) {
      console.warn('submitFeedback: currentSessionId is empty')
      return
    }
    const current = votes[messageIndex]
    // 带 reasons 时为"更新原因"，跳过 toggle 直接提交；否则 toggle
    const newVote = (reasons !== undefined) ? vote : (current === vote ? 0 : vote)
    const nextVotes = { ...votes }
    if (newVote === 0) delete nextVotes[messageIndex]
    else nextVotes[messageIndex] = newVote as 1 | -1

    setVotes(nextVotes)
    if (Object.keys(nextVotes).length > 0) {
      localStorage.setItem(`chat_votes_${currentSessionId}`, JSON.stringify(nextVotes))
    } else {
      localStorage.removeItem(`chat_votes_${currentSessionId}`)
    }

    try {
      // 若存在旧的不同 vote，先发取消请求
      if (current && current !== vote) {
        await ragevalService.submitFeedback(currentSessionId, messageIndex, 0)
      }
      if (newVote !== 0) {
        await ragevalService.submitFeedback(currentSessionId, messageIndex, newVote as 1 | -1, reasons)
      }
    } catch {
      // 静默失败，不打扰用户
    }
  }

  const genId = () => Math.random().toString(36).substring(2, 15)

  // ── 渲染 ─────────────────────────────────────────────────────────────────
  return (
    <div className="absolute inset-0 flex overflow-hidden">
      {/* 左侧会话列表 */}
      <SessionList
        sessions={sessions}
        currentSessionId={currentSessionId}
        onSelectSession={handleSelectSession}
        onNewSession={handleNewSession}
        onDeleteSession={handleDeleteSession}
        onExportSession={handleExportSession}
      />

      {/* 右侧主区域 */}
      <div className="relative flex flex-1 flex-col min-h-0 overflow-hidden">
        {/* 渐变背景层（欢迎屏与对话区共用） */}
        <div className="pointer-events-none absolute inset-0 bg-gradient-to-br from-[#F8FAFC] via-white to-[#EFF6FF]" />
        <div
          className="pointer-events-none absolute inset-0 opacity-[0.35]"
          style={{
            backgroundImage:
              'linear-gradient(to right, rgba(59,130,246,0.07) 1px, transparent 1px), linear-gradient(to bottom, rgba(59,130,246,0.07) 1px, transparent 1px)',
            backgroundSize: '40px 40px',
          }}
        />
        <div className="pointer-events-none absolute -top-32 right-[-40px] h-72 w-72 rounded-full bg-[#BFDBFE]/50 blur-3xl" />
        <div className="pointer-events-none absolute -bottom-36 left-[-80px] h-80 w-80 rounded-full bg-[#FDE68A]/30 blur-3xl" />

        {messages.length === 0 ? (
          /* ── 欢迎屏：标题 + 居中输入框 + 预设卡片 ── */
          <div className="relative flex-1 min-h-0 overflow-y-auto chat-sidebar-scroll">
            <WelcomeScreen
              onPresetSelect={(text, forceDeepThinking, forceWebSearch) => {
                if (forceDeepThinking !== undefined) setDeepThinking(forceDeepThinking)
                if (forceWebSearch !== undefined) setWebSearch(forceWebSearch)
                setInput(text)
              }}
              isLoading={isLoading}
              inputSlot={
                  <div className="relative w-full space-y-1">
                    <ChatInput
                      value={input}
                      onChange={setInput}
                      onSend={handleSend}
                      isLoading={isLoading}
                      onFileUpload={handleFileUploadSuccess}
                      deepThinking={deepThinking}
                      onDeepThinkingChange={setDeepThinking}
                      webSearch={webSearch}
                      onWebSearchChange={setWebSearch}
                    />
                  </div>
              }
            />
          </div>
        ) : (
          /* ── 对话区 ── */
          <>
            <div
              ref={chatScrollRef}
              data-testid="chat-scroll"
              onScroll={(event) => {
                const el = event.currentTarget
                followBottomRef.current = el.scrollHeight - el.scrollTop - el.clientHeight < 48
              }}
              className="relative flex-1 min-h-0 overflow-y-auto chat-sidebar-scroll"
            >
              <div ref={messageContentRef} className="min-w-0 max-w-[760px] ml-[max(32px,calc(50vw-660px))] px-6 py-8 space-y-3">
                {messages.map((m, i) => (
                  <MessageBubble
                    key={m.id}
                    message={m}
                    onRetry={m.runId && m.runId === sessionStorage.getItem(`chat_run_id_${currentSessionId}`)
                      ? () => startAssistantStream(currentSessionId, m, '', i, false) : undefined}
                    messageIndex={i}
                    vote={votes[i]}
                    onVote={submitFeedback}
                    isEditing={editingIndex === i}
                    editingContent={editingIndex === i ? editingContent : ''}
                    onStartEdit={handleStartEdit}
                    onCancelEdit={handleCancelEdit}
                    onSaveEdit={handleSaveEdit}
                    onEditingContentChange={setEditingContent}
                  />
                ))}
                <div ref={messagesEndRef} />
              </div>
            </div>

            {/* 底部输入区 */}
            <div className="relative border-t border-white/60 bg-white/60 backdrop-blur-sm px-4 py-4">
              <div className="max-w-[760px] ml-[max(32px,calc(50vw-660px))] space-y-2">
                {/* 对话输入框 ChatInput */}
                <div className="relative">
                  <ChatInput
                    value={input}
                    onChange={setInput}
                    onSend={handleSend}
                    isLoading={isLoading}
                    onFileUpload={handleFileUploadSuccess}
                    deepThinking={deepThinking}
                    onDeepThinkingChange={setDeepThinking}
                    webSearch={webSearch}
                    onWebSearchChange={setWebSearch}
                  />
                </div>
              </div>
            </div>
          </>
        )}

      </div>
    </div>
  )
}

// ── 消息气泡 MessageBubble ───────────────────────────────────────────────────

interface MessageBubbleProps {
  message: Message
  messageIndex: number
  vote?: 1 | -1
  onRetry?: () => void
  onVote: (messageIndex: number, vote: 1 | -1, reasons?: string[]) => void
  isEditing: boolean
  editingContent: string
  onStartEdit: (index: number, content: string) => void
  onCancelEdit: () => void
  onSaveEdit: (index: number) => void
  onEditingContentChange: (content: string) => void
}

function MessageBubble({ message, messageIndex, vote, onVote, onRetry, isEditing, editingContent, onStartEdit, onCancelEdit, onSaveEdit, onEditingContentChange }: MessageBubbleProps) {
  const [copied, setCopied] = useState(false)
  const [showReasonPicker, setShowReasonPicker] = useState(false)
  const [selectedReasons, setSelectedReasons] = useState<string[]>([])
  const editTextareaRef = useRef<HTMLTextAreaElement>(null)
  const reasonPickerRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!showReasonPicker) return
    const handler = (e: MouseEvent) => {
      if (reasonPickerRef.current && !reasonPickerRef.current.contains(e.target as Node)) {
        setShowReasonPicker(false)
      }
    }
    document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [showReasonPicker])

  const adjustEditHeight = useCallback(() => {
    const el = editTextareaRef.current
    if (!el) return
    el.style.height = 'auto'
    el.style.height = `${Math.min(el.scrollHeight, 200)}px`
  }, [])

  useEffect(() => { if (isEditing) adjustEditHeight() }, [isEditing, editingContent, adjustEditHeight])

  const handleCopy = async () => {
    try {
      await navigator.clipboard.writeText(message.content)
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    } catch (err) {
      console.error('复制失败:', err)
    }
  }
  if (message.role === 'user') {
    return (
      <div className="flex justify-end mb-4 group">
        <div className="relative max-w-[72%] inline-flex flex-col items-end gap-1">
          {isEditing ? (
            // 编辑模式：在原先气泡内，横条样式，取消/发送在右下角
            <div
              className="rounded-3xl rounded-br-lg px-5 py-2.5 flex flex-col gap-2 w-full min-w-0"
              style={{
                background: 'linear-gradient(135deg, #3B82F6 0%, #6366F1 100%)',
                boxShadow: '0 2px 12px rgba(59,130,246,0.25)',
              }}
            >
              <textarea
                ref={editTextareaRef}
                value={editingContent}
                onChange={(e) => { onEditingContentChange(e.target.value); adjustEditHeight() }}
                className="w-full min-w-0 bg-transparent text-white text-sm leading-relaxed placeholder-white/60 focus:outline-none resize-none py-0.5 overflow-y-auto"
                rows={1}
                placeholder="输入消息..."
                autoFocus
                style={{ minHeight: 24 }}
              />
              <div className="flex items-center justify-end gap-2">
                <button
                  type="button"
                  onClick={onCancelEdit}
                  className="px-4 py-2 text-xs font-medium text-white/95 hover:text-white border border-white/30 hover:border-white/50 hover:bg-white/15 rounded-full transition-all duration-200 active:scale-[0.98]"
                >
                  取消
                </button>
                <button
                  type="button"
                  onClick={() => onSaveEdit(messageIndex)}
                  disabled={!editingContent.trim()}
                  className="px-4 py-2 text-xs font-medium text-indigo-600 bg-white hover:bg-indigo-50 shadow-sm hover:shadow rounded-full transition-all duration-200 active:scale-[0.98] disabled:opacity-40 disabled:hover:bg-white disabled:hover:shadow-sm disabled:cursor-not-allowed disabled:active:scale-100"
                >
                  发送
                </button>
              </div>
            </div>
          ) : (
            // 显示模式
            <>
              <div
                className="rounded-3xl rounded-br-lg px-5 py-3 text-sm leading-relaxed text-white"
                style={{
                  background: 'linear-gradient(135deg, #3B82F6 0%, #6366F1 100%)',
                  boxShadow: '0 2px 12px rgba(59,130,246,0.25)',
                }}
              >
                {message.content}
              </div>
              <div className="flex items-center gap-1">
                <div className="relative group/copy">
                  <button
                    type="button"
                    onClick={handleCopy}
                    className="p-1.5 rounded-full text-gray-500 hover:text-gray-700 hover:bg-gray-100 transition-colors"
                  >
                    {copied ? (
                      <Check className="w-3.5 h-3.5 flex-shrink-0 text-green-600" />
                    ) : (
                      <Copy className="w-3.5 h-3.5 flex-shrink-0" />
                    )}
                  </button>
                  <div className="pointer-events-none absolute bottom-full left-1/2 mb-1.5 -translate-x-1/2 whitespace-nowrap rounded-lg bg-[#1F2937] px-2 py-1 text-[11px] font-medium text-white opacity-0 shadow-md transition-opacity duration-150 group-hover/copy:opacity-100">
                    {copied ? '已复制' : '复制'}
                    <span className="absolute left-1/2 top-full -translate-x-1/2 border-4 border-transparent border-t-[#1F2937]" />
                  </div>
                </div>
                <div className="relative group/edit">
                  <button
                    type="button"
                    onClick={() => onStartEdit(messageIndex, message.content)}
                    className={cn(
                      'p-1.5 rounded-full text-gray-500 transition-colors',
                      'hover:bg-indigo-50 hover:text-indigo-600',
                    )}
                  >
                    <Pencil className="w-3.5 h-3.5 flex-shrink-0" />
                  </button>
                  <div className="pointer-events-none absolute bottom-full left-1/2 mb-1.5 -translate-x-1/2 whitespace-nowrap rounded-lg bg-[#1F2937] px-2 py-1 text-[11px] font-medium text-white opacity-0 shadow-md transition-opacity duration-150 group-hover/edit:opacity-100">
                    编辑
                    <span className="absolute left-1/2 top-full -translate-x-1/2 border-4 border-transparent border-t-[#1F2937]" />
                  </div>
                </div>
              </div>
            </>
          )}
        </div>
      </div>
    )
  }

  // 助手消息
  return (
    <div data-message-id={message.id} className="flex min-w-0 max-w-full items-start gap-3 mb-1">
      {/* AI 头像 */}
      <div
        className="flex h-8 w-8 flex-shrink-0 items-center justify-center rounded-full text-white mt-1"
        style={{ background: 'linear-gradient(135deg, #3B82F6 0%, #6366F1 100%)' }}
      >
        <svg width="16" height="16" viewBox="0 0 24 24" fill="none">
          <path d="M12 2L15.09 8.26L22 9.27L17 14.14L18.18 21.02L12 17.77L5.82 21.02L7 14.14L2 9.27L8.91 8.26L12 2Z" fill="white" />
        </svg>
      </div>

      <div className="min-w-0 flex-1 space-y-2">
        {message.runStatus && (
          <div data-testid="run-status" className="text-xs text-slate-600">
            工作流：{({ pending: '等待执行', running: '执行中', waiting_approval: '等待审批', retryable_failed: '失败，等待恢复',
              parked: '已搁置', reconciling: '正在核对执行结果', succeeded: '已成功完成', failed: '执行失败', canceled: '已取消' } as Record<string, string>)[message.runStatus] ?? '状态未确认'}
          </div>
        )}
        {message.operationStatus && <div data-testid="operation-status" className="text-xs text-slate-500">操作事件：{message.operationStatus}</div>}
        {message.streamError && <div role="alert" className="text-sm text-amber-800 break-words">{message.streamError}</div>}
        {!message.isStreaming && onRetry && message.runStatus !== 'succeeded' && (
          <button type="button" onClick={onRetry} className="text-sm text-indigo-700 underline">重试连接</button>
        )}
        {/* Agent 状态 */}
        {message.isStreaming && message.agentStatus && (
          <div className="inline-flex items-center gap-1.5 rounded-full border border-[#BFDBFE] bg-[#EFF6FF] px-3 py-1 text-xs text-[#2563EB]">
            <span className="h-1.5 w-1.5 rounded-full bg-[#3B82F6] motion-safe:animate-pulse" />
            {message.agentStatus}
          </div>
        )}

        {/* 预思考区块（蓝色系，位于规划块上方） */}
        {(message.thinking !== undefined || message.isThinking) && (
          <ThinkingBlock
            thinking={message.thinking ?? ''}
            isThinking={!!message.isStreaming && !!message.isThinking}
            isDone={!!message.thinkDone}
            thinkDuration={message.thinkDuration}
          />
        )}

        {/* Plan Agent 规划过程区块 */}
        {(message.planning !== undefined) && (
          <PlanningBlock
            planning={message.planning || ''}
            isPlanRunning={!!message.isStreaming && !!message.isPlanRunning}
            isDone={!!message.planDone}
            planDuration={message.planDuration}
          />
        )}

        {/* 主内容 */}
        {message.content ? (
          <div className="flex min-w-0 max-w-full w-full flex-col items-start gap-1">
            {/* 深度规划徽章：规划完成后，标注在答案气泡上方 */}
            {message.planDone && !message.isStreaming && (
              <div className="flex items-center gap-1.5 rounded-full bg-gradient-to-r from-[#7C3AED]/10 to-[#6366F1]/10 border border-[#DDD6FE] px-2.5 py-1 text-[10px] font-medium text-[#6D28D9]">
                <Brain className="w-3 h-3 flex-shrink-0" />
                深度规划
              </div>
            )}
            <div className="min-w-0 max-w-full w-full rounded-3xl rounded-tl-lg border border-[#F0F0F0] bg-white px-5 py-3.5 shadow-[0_1px_6px_rgba(0,0,0,0.05)]">
              <MarkdownRenderer
                content={message.content}
                variant="chat"
                streaming={!!message.isStreaming}
                complete={!message.isStreaming}
                className="min-w-0 max-w-none text-[#1F2937]"
              />
              {message.isStreaming && <span className="inline-block h-4 w-1.5 motion-safe:animate-pulse bg-[#3B82F6] ml-0.5 rounded-sm align-text-bottom" />}
            </div>
            {/* 点赞/踩按钮（流式完成后显示，置于气泡左下角下方） */}
            {!message.isStreaming && (
              <div className="flex items-center gap-1">
                <div className="relative group/like">
                  <button
                    onClick={() => onVote(messageIndex, 1)}
                    className={cn(
                      'p-1.5 rounded-full hover:bg-emerald-50 transition-colors',
                      vote === 1 ? 'text-emerald-600' : 'text-gray-500 hover:text-emerald-500',
                    )}
                  >
                    <ThumbsUp className="w-3.5 h-3.5" />
                  </button>
                  <div className="pointer-events-none absolute bottom-full left-1/2 mb-1.5 -translate-x-1/2 whitespace-nowrap rounded-lg bg-[#1F2937] px-2 py-1 text-[11px] font-medium text-white opacity-0 shadow-md transition-opacity duration-150 group-hover/like:opacity-100">
                    有帮助
                    <span className="absolute left-1/2 top-full -translate-x-1/2 border-4 border-transparent border-t-[#1F2937]" />
                  </div>
                </div>
                <div className="relative" ref={reasonPickerRef}>
                  <button
                    onClick={() => {
                      if (vote === -1) { onVote(messageIndex, -1); setShowReasonPicker(false); setSelectedReasons([]) }
                      else { onVote(messageIndex, -1); setShowReasonPicker(true) }
                    }}
                    className={cn(
                      'p-1.5 rounded-full hover:bg-red-50 transition-colors',
                      vote === -1 ? 'text-red-500' : 'text-gray-500 hover:text-red-400',
                    )}
                  >
                    <ThumbsDown className="w-3.5 h-3.5" />
                  </button>
                  {showReasonPicker && (
                    <div className="absolute bottom-full left-0 mb-2 z-50 w-48 rounded-xl border border-gray-100 bg-white shadow-lg p-2 space-y-0.5">
                      <p className="px-2 py-1 text-[11px] text-gray-400 font-medium">哪里没帮助？</p>
                      {([
                        ['too_verbose',          '回答太啰嗦'],
                        ['too_brief',            '回答太简短'],
                        ['not_technical_enough', '技术深度不够'],
                        ['too_technical',        '过于技术化'],
                        ['inaccurate',           '内容不准确'],
                        ['off_topic',            '没有回答问题'],
                      ] as [string, string][]).map(([key, label]) => {
                        const checked = selectedReasons.includes(key)
                        return (
                          <button
                            key={key}
                            onClick={(e) => {
                              e.stopPropagation()
                              setSelectedReasons(prev => checked ? prev.filter(r => r !== key) : [...prev, key])
                            }}
                            className={cn(
                              'w-full text-left px-2 py-1.5 text-[12px] rounded-lg transition-colors flex items-center gap-1.5',
                              checked
                                ? 'bg-red-50 text-red-600'
                                : 'text-gray-700 hover:bg-gray-50',
                            )}
                          >
                            <span className={cn('flex h-3 w-3 flex-shrink-0 items-center justify-center rounded border text-[8px]', checked ? 'border-red-400 bg-red-400 text-white' : 'border-gray-300')}>
                              {checked && '✓'}
                            </span>
                            {label}
                          </button>
                        )
                      })}
                      <button
                        onClick={() => { onVote(messageIndex, -1, selectedReasons); setShowReasonPicker(false); setSelectedReasons([]) }}
                        className="w-full mt-1 py-1.5 text-[12px] font-medium text-white bg-red-400 hover:bg-red-500 rounded-lg transition-colors"
                      >
                        确认
                      </button>
                    </div>
                  )}
                </div>
              </div>
            )}
          </div>
        ) : (
          message.isStreaming && !message.isPlanRunning && (
            <div className="inline-flex items-center gap-1.5 rounded-3xl rounded-tl-lg border border-[#F0F0F0] bg-white px-5 py-4 shadow-[0_1px_6px_rgba(0,0,0,0.05)]">
              {[0, 1, 2].map((i) => (
                <span
                  key={i}
                  className="h-2 w-2 rounded-full bg-[#CBD5E1] motion-reduce:!animate-none"
                  style={{ animation: `chat-dot-pulse 1.2s ease-in-out ${i * 0.2}s infinite` }}
                />
              ))}
            </div>
          )
        )}
      </div>
    </div>
  )
}

// ── 规划过程 PlanningBlock ───────────────────────────────────────────────────

// 结构化步骤事件：后端推送的 JSON 格式，前端按 type 分类渲染
interface PlanStepEvent {
  type: 'plan_steps' | 'exec' | 'tool_result' | 'tool_call'
  steps?: string[]  // plan_steps：规划步骤清单
  content?: string  // exec/tool_result：内容或摘要
  name?: string     // tool_result/tool_call：工具友好名称
}

// 工具名到图标的映射
const WORKER_ICON_MAP: Record<string, React.ReactNode> = {
  '事件分析':   <Zap className="w-3 h-3" />,
  '风险评估':   <BarChart2 className="w-3 h-3" />,
  '报告生成':   <FileText className="w-3 h-3" />,
  '应急响应':   <Shield className="w-3 h-3" />,
  '威胁情报':   <Globe className="w-3 h-3" />,
  '知识库检索': <BookOpen className="w-3 h-3" />,
  '获取时间':   <Clock className="w-3 h-3" />,
}

// parsePlanning 将 planning 字符串（每行一个 JSON）解析为结构化步骤事件列表。
// 向后兼容：非 JSON 行当作 exec 类型。
function parsePlanning(planning: string): PlanStepEvent[] {
  if (!planning) return []
  return planning.split('\n').filter(Boolean).map(line => {
    try {
      return JSON.parse(line) as PlanStepEvent
    } catch {
      return { type: 'exec', content: line }
    }
  })
}

// ExecUnit 分组执行单元：exec 推理 + tool_call + tool_result 聚合为一个视觉单元
interface ExecUnit {
  thoughts: string[]           // 调用前 exec 推理文本（pre-call）
  workerName?: string          // tool_call 的 Worker 名称
  result?: string | null       // null = 调用中，string = 已完成，undefined = 纯推理单元
  analysis?: string            // 工具结果返回后的 exec 推理（post-result）
}

// groupEvents 将平铺事件流聚合为分组执行单元列表。
// exec 出现在 tool_result 之后时，视为对该结果的分析（post-result），附加到最近完成的单元；
// exec 出现在 tool_call 之前时，视为调用前推理（pre-call），作为该单元的 thoughts。
function groupEvents(events: PlanStepEvent[]): ExecUnit[] {
  const units: ExecUnit[] = []
  let currentThoughts: string[] = []
  let lastType = ''

  for (const ev of events) {
    if (ev.type === 'exec') {
      const content = ev.content || ''
      // post-result：紧跟 tool_result 后的 exec 视为结果分析，附加到最近完成的 Worker 单元
      if (lastType === 'tool_result' && units.length > 0) {
        const lastWorkerUnit = [...units].reverse().find(u => u.workerName !== undefined)
        if (lastWorkerUnit) {
          lastWorkerUnit.analysis = (lastWorkerUnit.analysis ?? '') + content
          lastType = ev.type
          continue
        }
      }
      // pre-call：积累为下一个 tool_call 的 thoughts
      currentThoughts.push(content)
    } else if (ev.type === 'tool_call') {
      units.push({ thoughts: currentThoughts, workerName: ev.name, result: null })
      currentThoughts = []
    } else if (ev.type === 'tool_result') {
      const pending = [...units].reverse().find(u => u.workerName === ev.name && u.result === null)
      if (pending) {
        pending.result = ev.content ?? ''
      } else {
        units.push({ thoughts: currentThoughts, workerName: ev.name, result: ev.content ?? '' })
        currentThoughts = []
      }
    }
    lastType = ev.type
  }

  // 尾部推理（Executor 最后一轮，无后续 tool_call）—— 保留为纯推理单元（执行中时展示）
  if (currentThoughts.length > 0) {
    units.push({ thoughts: currentThoughts })
  }

  return units
}

interface PlanningBlockProps {
  planning: string
  isPlanRunning: boolean
  isDone: boolean
  planDuration?: number
}

function PlanningBlock({ planning, isPlanRunning, isDone, planDuration }: PlanningBlockProps) {
  const [expanded, setExpanded] = useState(false)

  const events = parsePlanning(planning)

  // 取第一个 plan_steps 作为原始规划（完整步骤列表，不受 Replanner 更新影响）
  const firstPlanSteps = events.find(e => e.type === 'plan_steps')?.steps ?? []
  const latestPlanSteps = [...events].reverse().find(e => e.type === 'plan_steps')?.steps ?? []
  const displaySteps = firstPlanSteps.length > 0 ? firstPlanSteps : latestPlanSteps

  // 按 Worker tool_result 计算已完成步数
  const WORKER_NAMES = ['事件分析', '报告生成', '风险评估', '应急响应', '威胁情报']
  const completedWorkerCount = events.filter(
    e => e.type === 'tool_result' && e.name && WORKER_NAMES.includes(e.name)
  ).length
  const totalSteps = displaySteps.length

  // 分组执行单元（过滤掉 plan_steps 事件，只处理 exec/tool_call/tool_result）
  const execUnitsAll = groupEvents(events.filter(e => e.type !== 'plan_steps'))
  // 完成态下隐藏末尾纯推理单元（Executor 最终总结已在主答案气泡中展示，不重复显示）
  const execUnits = isDone
    ? execUnitsAll.filter(u => u.workerName !== undefined)
    : execUnitsAll
  const hasExecUnits = execUnits.some(u => u.workerName !== undefined || u.thoughts.some(Boolean))
  const hasPlanSteps = displaySteps.length > 0
  const toolCount = events.filter(e => e.type === 'tool_result').length

  if (!isPlanRunning && !isDone) return null
  if (!isPlanRunning && !planning) return null

  // ── 执行中 ──────────────────────────────────────────────────────────────────
  if (isPlanRunning) {
    return (
      <div className="rounded-2xl border border-[#A7F3D0] bg-[#ECFDF5] overflow-hidden">
        {/* 标题栏 */}
        <div className="flex items-center gap-2 px-4 py-3">
          <Loader2 className="h-4 w-4 motion-safe:animate-spin text-[#059669] flex-shrink-0" />
          <span className="text-sm font-semibold text-[#059669]">深度规划执行中</span>
          {totalSteps > 0 && (
            <span className="ml-auto rounded-full bg-[#A7F3D0] px-2 py-0.5 text-xs font-medium text-[#065F46] tabular-nums">
              步骤 {Math.min(completedWorkerCount + 1, totalSteps)}/{totalSteps}
            </span>
          )}
        </div>

        {/* 规划方案 */}
        {hasPlanSteps && (
          <div className="border-t border-[#A7F3D0]/60 px-4 py-3">
            <div className="mb-2 flex items-center gap-1.5 text-xs font-medium text-[#065F46]">
              <ListChecks className="w-3.5 h-3.5" /><span>规划方案</span>
            </div>
            <ol className="space-y-2 pl-1">
              {displaySteps.map((step, i) => {
                const isDoneStep = i < completedWorkerCount
                const isCurrent = i === completedWorkerCount
                return (
                  <li key={i} className="flex items-start gap-2 text-xs">
                    {isDoneStep ? (
                      <span className="mt-0.5 flex h-4 w-4 flex-shrink-0 items-center justify-center rounded-full bg-[#059669] text-white text-[9px] font-bold">✓</span>
                    ) : isCurrent ? (
                      <span className="mt-0.5 flex h-4 w-4 flex-shrink-0 items-center justify-center rounded-full bg-[#A7F3D0]">
                        <Loader2 className="h-2.5 w-2.5 text-[#059669] motion-safe:animate-spin" />
                      </span>
                    ) : (
                      <span className="mt-0.5 flex h-4 w-4 flex-shrink-0 items-center justify-center rounded-full bg-[#D1FAE5] text-[#065F46] text-[10px] font-bold">{i + 1}</span>
                    )}
                    <span className={cn(
                      'leading-relaxed',
                      isDoneStep ? 'text-[#6EE7B7] line-through' : isCurrent ? 'text-[#065F46] font-medium' : 'text-[#9CA3AF]'
                    )}>{step}</span>
                  </li>
                )
              })}
            </ol>
          </div>
        )}

        {/* 执行过程 - 分组执行单元 */}
        {hasExecUnits && (
          <div className="border-t border-[#A7F3D0]/60 px-4 py-3">
            <div className="mb-3 flex items-center gap-1.5 text-xs font-medium text-[#065F46]">
              <Zap className="w-3.5 h-3.5" /><span>执行过程</span>
            </div>
            <div className="space-y-2">
              {execUnits.map((unit, i) => (
                <ExecUnitCard
                  key={i}
                  unit={unit}
                  isLast={i === execUnits.length - 1}
                  isRunning={isPlanRunning}
                />
              ))}
            </div>
          </div>
        )}
      </div>
    )
  }

  // ── 完成（可折叠） ──────────────────────────────────────────────────────────
  return (
    <div className="overflow-hidden rounded-2xl border border-[#A7F3D0] bg-[#ECFDF5]">
      <button
        type="button"
        onClick={() => setExpanded(v => !v)}
        className="flex w-full items-center gap-2 px-4 py-3 text-left transition-colors hover:bg-[#A7F3D0]/30"
      >
        <div className="flex h-7 w-7 flex-shrink-0 items-center justify-center rounded-lg bg-[#A7F3D0]">
          <ListChecks className="h-4 w-4 text-[#059669]" />
        </div>
        <div className="flex flex-1 flex-wrap items-center gap-1.5 min-w-0">
          <span className="text-sm font-semibold text-[#059669]">深度规划</span>
          {planDuration !== undefined && (
            <span className="rounded-full bg-[#A7F3D0] px-2 py-0.5 text-xs text-[#065F46]">{planDuration} 秒</span>
          )}
          {totalSteps > 0 && (
            <span className="rounded-full bg-[#D1FAE5] px-2 py-0.5 text-xs text-[#065F46]">{totalSteps} 个步骤</span>
          )}
          {toolCount > 0 && (
            <span className="rounded-full bg-[#D1FAE5] px-2 py-0.5 text-xs text-[#065F46]">{toolCount} 个 Agent 协作</span>
          )}
        </div>
        {expanded ? <ChevronDown className="h-4 w-4 flex-shrink-0 text-[#059669]" /> : <ChevronRight className="h-4 w-4 flex-shrink-0 text-[#059669]" />}
      </button>
      {expanded && (
        <div className="border-t border-[#A7F3D0]">
          {hasPlanSteps && (
            <div className="px-4 py-3">
              <div className="mb-2 flex items-center gap-1.5 text-xs font-medium text-[#065F46]">
                <ListChecks className="w-3.5 h-3.5" /><span>规划方案</span>
              </div>
              <ol className="space-y-1.5 pl-1">
                {displaySteps.map((step, i) => (
                  <li key={i} className="flex items-start gap-2 text-xs text-[#065F46]">
                    <span className="mt-0.5 flex h-4 w-4 flex-shrink-0 items-center justify-center rounded-full bg-[#059669] text-white text-[9px] font-bold">✓</span>
                    <span className="leading-relaxed">{step}</span>
                  </li>
                ))}
              </ol>
            </div>
          )}
          {hasExecUnits && (
            <div className={cn('px-4 pb-3', hasPlanSteps && 'border-t border-[#A7F3D0]/60 pt-3')}>
              <div className="mb-3 flex items-center gap-1.5 text-xs font-medium text-[#065F46]">
                <Zap className="w-3.5 h-3.5" /><span>执行过程</span>
              </div>
              <div className="space-y-2">
                {execUnits.map((unit, i) => (
                  <ExecUnitCard
                    key={i}
                    unit={unit}
                    isLast={i === execUnits.length - 1}
                    isRunning={false}
                  />
                ))}
              </div>
            </div>
          )}
        </div>
      )}
    </div>
  )
}

// ExecUnitCard 单个分组执行单元：可展开的 Worker 卡片
// 默认折叠，点击展开后显示 Executor 推理过程和 Worker 执行结果
function ExecUnitCard({ unit, isLast, isRunning }: { unit: ExecUnit; isLast: boolean; isRunning: boolean }) {
  const [expanded, setExpanded] = useState(false)
  const isPending = unit.workerName !== undefined && unit.result === null && isLast && isRunning
  const isDone = unit.workerName !== undefined && unit.result !== null && unit.result !== undefined
  const thoughtsText = unit.thoughts.filter(Boolean).join(' ')
  const icon = (unit.workerName && WORKER_ICON_MAP[unit.workerName]) || <Zap className="w-3 h-3" />

  // 纯推理单元（无 Worker 调用）：简单显示思考文本
  if (!unit.workerName) {
    return thoughtsText ? (
      <div className="flex items-start gap-2">
        <span className="flex-shrink-0 text-[10px] text-[#6EE7B7] mt-0.5 leading-none select-none">💭</span>
        <p className="text-xs italic text-[#6B7280] leading-relaxed line-clamp-2">{thoughtsText}</p>
      </div>
    ) : null
  }

  return (
    <div className={cn(
      'rounded-xl border overflow-hidden transition-colors',
      isPending ? 'border-[#A7F3D0] bg-[#F0FDF4]' : 'border-[#D1FAE5] bg-white/70',
    )}>
      {/* 卡片头部：始终可见，点击展开/折叠详情 */}
      <button
        type="button"
        onClick={() => isDone && setExpanded(v => !v)}
        className={cn(
          'flex w-full items-center gap-2 px-3 py-2.5 text-left',
          isDone && 'hover:bg-[#F0FDF4]/60 transition-colors cursor-pointer',
          !isDone && 'cursor-default',
        )}
      >
        <div className="flex h-5 w-5 flex-shrink-0 items-center justify-center rounded-full bg-[#059669] text-white">
          {isPending ? <Loader2 className="h-2.5 w-2.5 motion-safe:animate-spin" /> : icon}
        </div>
        <span className="text-xs font-semibold text-[#065F46] flex-1">{unit.workerName}</span>
        <span className="text-[10px]">
          {isPending
            ? <span className="text-[#059669] motion-safe:animate-pulse">正在执行...</span>
            : isDone
              ? <span className="text-[#6EE7B7]">✓ 完成</span>
              : null
          }
        </span>
        {isDone && (
          expanded
            ? <ChevronDown className="h-3 w-3 flex-shrink-0 text-[#6EE7B7]" />
            : <ChevronRight className="h-3 w-3 flex-shrink-0 text-[#A7F3D0]" />
        )}
      </button>

      {/* 展开详情：推理过程 + 执行结果 + 结果分析 */}
      {isDone && expanded && (
        <div className="border-t border-[#D1FAE5] px-3 py-2.5 space-y-2.5">
          {thoughtsText && (
            <div>
              <p className="text-[10px] font-medium text-[#6EE7B7] mb-1">💭 调用前推理</p>
              <p className="text-xs italic text-[#6B7280] leading-relaxed">{thoughtsText}</p>
            </div>
          )}
          {unit.result && (
            <div>
              <p className="text-[10px] font-medium text-[#059669] mb-1">📋 执行结果</p>
              <p className="text-xs text-[#047857] leading-relaxed">{unit.result}</p>
            </div>
          )}
          {unit.analysis && (
            <div>
              <p className="text-[10px] font-medium text-[#0284C7] mb-1">🔍 结果分析</p>
              <p className="text-xs text-[#0369A1] leading-relaxed">{unit.analysis}</p>
            </div>
          )}
        </div>
      )}
    </div>
  )
}

// ── 思考过程 ThinkingBlock ───────────────────────────────────────────────────

interface ThinkingBlockProps {
  thinking: string
  isThinking: boolean
  isDone: boolean
  thinkDuration?: number
}

// ThinkingBlock 深度思考预思考阶段展示组件。
// 执行中：实时流式显示 Agent 的推理内容（蓝色系，带光标闪烁）。
// 完成后：可折叠，标题行显示用时和「已完成思考」标签。
function ThinkingBlock({ thinking, isThinking, isDone, thinkDuration }: ThinkingBlockProps) {
  const [expanded, setExpanded] = useState(!isDone)
  if (!isThinking && !isDone && !thinking) return null

  return (
    <div className="min-w-0 max-w-full overflow-hidden rounded-2xl border border-[#BAE6FD] bg-[#F0F9FF]">
      <button
        type="button"
        onClick={() => setExpanded(v => !v)}
        disabled={isThinking}
        className="flex w-full items-center gap-2 px-4 py-3 text-left transition-colors hover:bg-[#BAE6FD]/30"
      >
        <div className="relative flex h-7 w-7 flex-shrink-0 items-center justify-center rounded-lg bg-[#BAE6FD]">
          <Lightbulb className="h-4 w-4 text-[#0284C7]" />
          {isThinking && <span className="absolute -top-0.5 -right-0.5 h-1.5 w-1.5 rounded-full bg-[#38BDF8] motion-safe:animate-ping" />}
        </div>
        <div className="flex flex-1 flex-wrap items-center gap-1.5 min-w-0">
          <span className="text-sm font-semibold text-[#0284C7]">{isThinking ? '深度思考中...' : isDone ? '已完成思考' : '思考已停止'}</span>
          {!isThinking && thinkDuration !== undefined && (
            <span className="rounded-full bg-[#BAE6FD] px-2 py-0.5 text-xs text-[#0369A1]">{thinkDuration} 秒</span>
          )}
        </div>
        {!isThinking && (expanded
          ? <ChevronDown className="h-4 w-4 flex-shrink-0 text-[#0284C7]" />
          : <ChevronRight className="h-4 w-4 flex-shrink-0 text-[#0284C7]" />)}
      </button>
      {(isThinking || expanded) && thinking && (
        <div className="min-w-0 max-w-full border-t border-[#BAE6FD] px-4 py-3">
          <MarkdownRenderer content={thinking} variant="thinking" streaming={isThinking} complete={!isThinking} className="text-[#0369A1]" />
          {isThinking && <span className="inline-block h-3.5 w-0.5 bg-[#0284C7] ml-0.5 align-middle motion-safe:animate-pulse rounded-sm" />}
        </div>
      )}
    </div>
  )
}
