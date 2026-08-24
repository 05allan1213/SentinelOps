import { useState } from 'react'
import { Plus, Trash2, Search, X, Download } from 'lucide-react'
import { ChatSession } from '@/services/chat'
import { cn } from '@/utils'

interface SessionListProps {
  sessions: ChatSession[]
  currentSessionId: string
  onSelectSession: (sessionId: string) => void
  onNewSession: () => void
  onDeleteSession: (sessionId: string) => void
  onExportSession: (sessionId: string) => void
}

/** 按时间将会话分组：今天 / 7天内 / 30天内 / 更早 */
function groupByTime(sessions: ChatSession[]) {
  const now = Date.now()
  const DAY = 86400000
  const groups: { label: string; items: ChatSession[] }[] = []
  const map = new Map<string, ChatSession[]>()
  const order: string[] = []

  for (const s of sessions) {
    const days = (now - s.lastMessageAt) / DAY
    let label: string
    if (days < 1) label = '今天'
    else if (days <= 7) label = '7 天内'
    else if (days <= 30) label = '30 天内'
    else label = '更早'

    if (!map.has(label)) {
      map.set(label, [])
      order.push(label)
    }
    map.get(label)!.push(s)
  }

  for (const label of order) {
    groups.push({ label, items: map.get(label)! })
  }
  return groups
}

export default function SessionList({
  sessions,
  currentSessionId,
  onSelectSession,
  onNewSession,
  onDeleteSession,
  onExportSession,
}: SessionListProps) {
  const [query, setQuery] = useState('')

  const filtered = query.trim()
    ? sessions.filter((s) =>
        s.title.toLowerCase().includes(query.trim().toLowerCase())
      )
    : sessions

  const groups = groupByTime(filtered)

  return (
    <aside className="fixed left-0 top-0 z-40 flex h-screen w-[280px] flex-shrink-0 flex-col bg-[#FAFAFA] p-3 lg:static lg:h-screen">
      <div className="py-3 space-y-4">
        {/* 快速开始卡片 — 对角渐变 + 双色光晕 */}
        <div className="relative overflow-hidden rounded-2xl border border-[#E6EEF6] bg-gradient-to-br from-[#F0F9FF] via-white to-[#FEF3C7] p-3 shadow-[0_14px_30px_rgba(15,23,42,0.08)]">
          {/* 右上角蓝色光晕 */}
          <span aria-hidden="true" className="absolute -right-10 -top-10 h-24 w-24 rounded-full bg-[#BAE6FD]/70 blur-2xl" />
          {/* 左下角黄色光晕 */}
          <span aria-hidden="true" className="absolute -left-12 -bottom-10 h-28 w-28 rounded-full bg-[#FDE68A]/70 blur-2xl" />

          <div className="relative">
            {/* 标题行 */}
            <div className="flex items-center justify-between px-1">
              <span className="text-[11px] font-semibold text-[#94A3B8]">快速开始</span>
              <span className="rounded-full bg-white/80 px-2 py-0.5 text-[10px] font-semibold text-[#2563EB]">
                新内容
              </span>
            </div>

            {/* 新建对话按钮 */}
            <button
              type="button"
              className="mt-2 flex w-full items-center gap-3 rounded-2xl bg-white/90 px-4 py-3 text-left shadow-[0_10px_20px_rgba(15,23,42,0.08)] transition-all hover:-translate-y-[1px] hover:shadow-[0_16px_30px_rgba(15,23,42,0.12)]"
              onClick={onNewSession}
            >
              <span className="flex h-11 w-11 items-center justify-center rounded-2xl bg-gradient-to-br from-[#60A5FA] to-[#2563EB] text-white shadow-[0_6px_14px_rgba(37,99,235,0.3)]">
                <Plus className="h-4 w-4" />
              </span>
              <span className="flex-1">
                <span className="block text-sm font-semibold text-[#1F2937]">新建对话</span>
                <span className="block text-xs text-[#94A3B8]">从空白开始</span>
              </span>
            </button>

          </div>
        </div>

        {/* 搜索对话 — 独立白色卡片 */}
        <div className="rounded-2xl border border-[#E6EEF6] bg-white p-3 shadow-[0_12px_26px_rgba(15,23,42,0.06)]">
          <div className="flex items-center justify-between px-1">
            <span className="text-[11px] font-semibold text-[#94A3B8]">搜索对话</span>
            <span className="text-[10px] text-[#CBD5E1]">Ctrl / Cmd + K</span>
          </div>
          <div className="mt-2 relative">
            <Search className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-[#9CA3AF]" />
            <input
              type="text"
              placeholder="搜索对话..."
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              className="h-10 w-full rounded-xl border border-[#E5E7EB] bg-[#F8FAFC] pl-9 pr-8 text-sm text-[#1F2937] placeholder:text-[#9CA3AF] focus:border-[#93C5FD] focus:outline-none transition-colors"
            />
            {query && (
              <button
                onClick={() => setQuery('')}
                className="absolute right-2.5 top-1/2 -translate-y-1/2 p-0.5 text-[#9CA3AF] hover:text-[#6B7280]"
              >
                <X className="h-3.5 w-3.5" />
              </button>
            )}
          </div>
        </div>
      </div>

      {/* 会话列表 */}
      <div className="relative flex-1 min-h-0">
        <div className="h-full overflow-y-auto chat-sidebar-scroll">
          {groups.length === 0 ? (
            <div className="flex h-full flex-col items-center justify-center text-[#999999]">
              <p className="mt-2 text-sm">暂无对话记录</p>
            </div>
          ) : (
            <div>
              {groups.map((group, index) => (
                <div key={group.label} className={cn('flex flex-col', index === 0 ? 'mt-0' : 'mt-4')}>
                  <p className="mb-1.5 pl-3 text-[12px] font-normal leading-[18px] text-[#999999]">
                    {group.label}
                  </p>
                  {group.items.map((session) => {
                    const isActive = session.id === currentSessionId
                    return (
                      <div
                        key={session.id}
                        onClick={() => onSelectSession(session.id)}
                        className={cn(
                          'group my-[1px] flex min-h-[40px] cursor-pointer items-center justify-between gap-2 rounded-lg px-3 py-2 text-[14px] leading-[22px] transition-colors duration-200',
                          isActive
                            ? 'bg-[#DBEAFE] text-[#2563EB]'
                            : 'text-[#333333] hover:bg-[#F5F5F5]'
                        )}
                      >
                        <span className="min-w-0 flex-1 truncate font-normal">
                          {session.title}
                        </span>
                        <div className="flex items-center gap-1">
                          <button
                            type="button"
                            onClick={(e) => {
                              e.stopPropagation()
                              onExportSession(session.id)
                            }}
                            className={cn(
                              'flex h-6 w-6 flex-shrink-0 items-center justify-center rounded transition-opacity duration-150 hover:bg-[rgba(0,0,0,0.06)]',
                              isActive
                                ? 'opacity-100 text-[#2563EB]'
                                : 'pointer-events-none opacity-0 group-hover:pointer-events-auto group-hover:opacity-100 text-[#666666]'
                            )}
                          >
                            <Download className="h-3.5 w-3.5" />
                          </button>
                          <button
                            type="button"
                            onClick={(e) => {
                              e.stopPropagation()
                              onDeleteSession(session.id)
                            }}
                            className={cn(
                              'flex h-6 w-6 flex-shrink-0 items-center justify-center rounded transition-opacity duration-150 hover:bg-[rgba(0,0,0,0.06)]',
                              isActive
                                ? 'opacity-100 text-[#2563EB]'
                                : 'pointer-events-none opacity-0 group-hover:pointer-events-auto group-hover:opacity-100 text-[#666666]'
                            )}
                          >
                            <Trash2 className="h-3.5 w-3.5" />
                          </button>
                        </div>
                      </div>
                    )
                  })}
                </div>
              ))}
            </div>
          )}
        </div>
        {/* 底部渐隐遮罩 */}
        <div aria-hidden="true" className="pointer-events-none absolute inset-x-0 bottom-0 z-10 h-5 bg-gradient-to-b from-transparent to-[#FAFAFA]" />
      </div>
    </aside>
  )
}
