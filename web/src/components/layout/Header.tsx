import { Bell, Search, User, ChevronDown } from 'lucide-react'
import { useState } from 'react'
import { useAuthStore } from '@/stores/authStore'

export default function Header() {
  const [searchValue, setSearchValue] = useState('')
  const username = useAuthStore((s) => s.username)
  const role = useAuthStore((s) => s.role)

  return (
    <header className="sticky top-0 z-40 border-b border-slate-200/70 bg-white/80 backdrop-blur">
      <div className="h-16 px-8 flex items-center justify-between gap-4">
        {/* 搜索框 */}
        <div className="relative w-full max-w-[420px]">
          <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-slate-400" />
          <input
            type="text"
            value={searchValue}
            onChange={e => setSearchValue(e.target.value)}
            placeholder="搜索安全事件..."
            className="h-10 w-full rounded-xl border border-slate-200 bg-white pl-10 pr-20 text-sm text-slate-800 placeholder:text-slate-400 focus:border-slate-300 focus:outline-none transition-colors"
          />
          <span className="absolute right-3 top-1/2 -translate-y-1/2 rounded-md border border-slate-200 bg-white px-2 py-0.5 text-[10px] text-slate-400">
            Ctrl K
          </span>
        </div>

        {/* 右侧操作区 */}
        <div className="flex items-center gap-2">
          {/* 通知铃 */}
          <button className="relative p-2.5 rounded-xl text-slate-500 hover:text-slate-700 hover:bg-slate-100 transition-colors">
            <Bell style={{ width: 18, height: 18 }} />
            <span className="absolute top-2 right-2 h-1.5 w-1.5 rounded-full bg-red-500" />
          </button>

          <div className="h-6 w-px bg-slate-200" />

          {/* 用户菜单 */}
          <button className="flex items-center gap-2 rounded-full border border-slate-200 bg-white px-2.5 py-1.5 text-sm text-slate-600 shadow-sm hover:bg-slate-50 transition-colors">
            <div className="h-8 w-8 rounded-full bg-indigo-50 flex items-center justify-center">
              <User className="h-4 w-4 text-indigo-600" />
            </div>
            <span className="hidden sm:inline font-medium text-slate-700">{username || 'User'}</span>
            {role === 'admin' && (
              <span className="hidden sm:inline text-xs bg-indigo-50 text-indigo-600 border border-indigo-200 rounded px-1.5 py-0.5">管理员</span>
            )}
            <ChevronDown className="h-4 w-4 text-slate-400" />
          </button>
        </div>
      </div>
    </header>
  )
}
