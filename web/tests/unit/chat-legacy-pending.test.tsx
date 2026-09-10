import { act, render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import Chat from '@/pages/chat'

// The page owns the legacy-snapshot migration; the service stub only needs to
// prove that restoring or leaving a session never starts a new Run.
const service = vi.hoisted(() => ({
  hasPendingCreate: vi.fn(() => false),
  multiAgentChat: vi.fn(),
  switchSession: vi.fn(),
  updateSession: vi.fn(),
  getSessionId: () => 'legacy',
  createSession: () => ({ id: 'fresh', title: '新会话', createdAt: 2, lastMessageAt: 2, messageCount: 0 }),
  listSessions: () => [
    { id: 'legacy', title: 'Legacy', createdAt: 1, lastMessageAt: 1, messageCount: 2 },
    { id: 'other', title: 'Other', createdAt: 1, lastMessageAt: 1, messageCount: 1 },
  ],
}))
vi.mock('@/services', () => ({ chatService: service }))
vi.mock('@/pages/chat/components/ChatInput', () => ({ default: ({ onSend }: { onSend: (text: string) => void }) => <button onClick={() => onSend('question')}>Send fixture</button> }))
vi.mock('@/pages/chat/components/WelcomeScreen', () => ({ default: ({ inputSlot }: { inputSlot: React.ReactNode }) => inputSlot }))
vi.mock('@/pages/chat/components/SessionList', () => ({ default: ({ sessions, onSelectSession, onNewSession }: {
  sessions: { id: string; title: string }[]
  onSelectSession: (sessionId: string) => void
  onNewSession: () => void
}) => (
  <div>
    {sessions.map((session) => <button key={session.id} onClick={() => onSelectSession(session.id)}>打开 {session.id}</button>)}
    <button onClick={onNewSession}>新建会话</button>
  </div>
) }))

// A pre-D-07 snapshot: the assistant turn is pending, but the only record is
// `isStreaming: true` — no Run identity and no `createUnconfirmed` marker.
const legacyPending = { id: 'assistant-1', role: 'assistant', content: '', timestamp: 1720000000000, isStreaming: true, agentStatus: 'running' }
const legacyHistory = [{ id: 'user-1', role: 'user', content: 'hello', timestamp: 1720000000000 }, legacyPending]

function save(sid: string, messages: unknown[]) {
  localStorage.setItem(`chat_messages_${sid}`, JSON.stringify(messages))
}
function saved(sid: string) {
  return JSON.parse(localStorage.getItem(`chat_messages_${sid}`)!) as { createUnconfirmed?: boolean; isStreaming?: boolean; runId?: string }[]
}
function mount() {
  return render(<MemoryRouter><Chat /></MemoryRouter>)
}

beforeEach(() => { localStorage.clear(); sessionStorage.clear(); vi.clearAllMocks() })

it('restores a legacy pending snapshot as an explicit unconfirmed create', async () => {
  save('legacy', legacyHistory)
  mount()
  await act(async () => {})

  expect(screen.getByRole('alert')).toHaveTextContent('创建结果尚未确认')
  expect(saved('legacy')[1]).toMatchObject({ createUnconfirmed: true, isStreaming: false })
  expect(service.multiAgentChat).not.toHaveBeenCalled()
})

it('keeps the legacy uncertainty when leaving the session without clearing it', async () => {
  save('legacy', legacyHistory)
  mount()
  await act(async () => {})
  // An older build sharing this origin can rewrite the snapshot after restore;
  // leaving the session must not turn that pending create into a finished reply.
  save('legacy', legacyHistory)

  screen.getByText('打开 other').click()
  await act(async () => {})

  expect(saved('legacy')[1]).toMatchObject({ createUnconfirmed: true, isStreaming: false })
  expect(service.switchSession).toHaveBeenCalledWith('other')
  expect(service.multiAgentChat).not.toHaveBeenCalled()
})
