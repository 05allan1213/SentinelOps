import { useState, useEffect } from 'react'
import { Settings as SettingsIcon, Save, Loader2, User, LogOut, UserPlus } from 'lucide-react'
import { cn } from '@/utils'
import toast from 'react-hot-toast'
import { useSettingsStore } from '@/stores/settingsStore'
import { settingsService } from '@/services/settings'
import { useAuthStore } from '@/stores/authStore'
import { usersService, type CreateUserInput, type UserItem, type UserRole } from '@/services/users'

const roleLabels: Record<string, string> = {
  viewer: '只读',
  operator: '操作员',
  approver: '审批人',
  admin: '管理员',
}

export default function Settings() {
  const { siteName, autoMarkRead, setSettings } = useSettingsStore()
  const { username, role, logout } = useAuthStore()
  const [draftName, setDraftName] = useState(siteName)
  const [isSaving, setIsSaving] = useState(false)
  const [loading, setLoading] = useState(true)
  const isAdmin = role === 'admin'
  const [users, setUsers] = useState<UserItem[]>([])
  const [userDraft, setUserDraft] = useState<CreateUserInput>({ username: '', password: '', role: 'viewer' })
  const [creatingUser, setCreatingUser] = useState(false)

  // 页面加载时从后端拉取最新配置，同步到 store 和本地草稿
  useEffect(() => {
    settingsService.getGeneral()
      .then(s => {
        setSettings({ siteName: s.siteName, autoMarkRead: s.autoMarkRead })
        setDraftName(s.siteName)
      })
      .catch(() => { /* 后端不可用时保留 localStorage 中的值 */ })
      .finally(() => setLoading(false))
  }, [])

  useEffect(() => {
    if (!isAdmin) return
    usersService.list()
      .then(setUsers)
      .catch(() => { /* 非管理员或后端不可用时保持空列表 */ })
  }, [isAdmin])

  const handleSiteNameChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const value = e.target.value
    setDraftName(value)           // 本地 state 控制输入（无批处理延迟）
    setSettings({ siteName: value }) // 同步更新 store → 侧边栏实时响应
  }

  const handleSave = async () => {
    setIsSaving(true)
    try {
      await settingsService.saveGeneral({ siteName: draftName, autoMarkRead })
      setSettings({ siteName: draftName }) // 确保 store 与草稿一致
      toast.success('设置已保存')
    } catch {
      toast.error('保存失败，请检查服务连接')
    } finally {
      setIsSaving(false)
    }
  }

  const handleCreateUser = async () => {
    const username = userDraft.username.trim()
    if (username.length < 3) {
      toast.error('用户名至少 3 个字符')
      return
    }
    if (userDraft.password.length < 6) {
      toast.error('密码至少 6 位')
      return
    }
    setCreatingUser(true)
    try {
      await usersService.create({ ...userDraft, username })
      toast.success('用户已创建')
      setUserDraft({ username: '', password: '', role: 'viewer' })
      setUsers(await usersService.list())
    } catch (error) {
      toast.error(error instanceof Error ? error.message : '创建用户失败')
    } finally {
      setCreatingUser(false)
    }
  }

  if (loading) {
    return (
      <div className="flex items-center justify-center h-48">
        <Loader2 className="w-6 h-6 animate-spin text-primary-500" />
      </div>
    )
  }

  return (
    <div className="space-y-6">
      {/* 页面标题 */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold text-gray-900 tracking-tight">系统设置</h1>
          <p className="text-sm text-gray-500 mt-1">配置系统参数</p>
        </div>
        <button onClick={handleSave} disabled={isSaving} className="btn-primary">
          {isSaving ? (
            <><Loader2 className="w-4 h-4 animate-spin" />保存中...</>
          ) : (
            <><Save className="w-4 h-4" />保存设置</>
          )}
        </button>
      </div>

      <div className="flex gap-6">
        {/* 侧边导航 */}
        <div className="w-48 flex-shrink-0">
          <nav className="card p-2">
            <button className="w-full flex items-center gap-3 px-4 py-3 rounded-lg bg-primary-50 text-primary-600 shadow-sm text-left text-sm font-medium">
              <SettingsIcon className="w-4 h-4 flex-shrink-0" />
              <span>通用设置</span>
            </button>
          </nav>
        </div>

        {/* 设置内容 */}
        <div className="flex-1 space-y-6">
          {/* 管理员信息卡片 */}
          <div className="card">
            <div className="card-body">
              <div className="flex items-center justify-between">
                <div className="flex items-center gap-4">
                  <div className="flex h-12 w-12 items-center justify-center rounded-full bg-gradient-to-br from-blue-500 to-purple-600">
                    <User className="h-6 w-6 text-white" />
                  </div>
                  <div>
                    <h3 className="text-base font-semibold text-gray-900">{username || 'User'}</h3>
                    <p className="text-sm text-gray-500">{role === 'admin' ? '管理员' : '普通用户'}</p>
                  </div>
                </div>
                <button
                  onClick={() => {
                    logout()
                    window.location.href = '/login'
                  }}
                  className="flex items-center gap-2 rounded-lg border border-gray-300 bg-white px-4 py-2 text-sm font-medium text-gray-700 transition-colors hover:bg-gray-50"
                >
                  <LogOut className="h-4 w-4" />
                  退出登录
                </button>
              </div>
            </div>
          </div>

          <div className="card">
            <div className="card-body space-y-6">
              <div>
                <h3 className="text-base font-semibold text-gray-900 mb-4">通用设置</h3>
                <div className="space-y-4">
                  <div className="form-item">
                    <label className="label">系统名称</label>
                    <input
                      type="text"
                      value={draftName}
                      onChange={handleSiteNameChange}
                      className="input"
                    />
                  </div>
                </div>
              </div>

              <div className="border-t border-gray-200 pt-6">
                <h3 className="text-base font-semibold text-gray-900 mb-4">功能开关</h3>
                <div className="flex items-center justify-between p-4 rounded-lg bg-gray-50 border border-gray-200">
                  <div>
                    <p className="text-sm font-medium text-gray-900">自动标记已读</p>
                    <p className="text-xs text-gray-500 mt-1">查看事件详情后自动标记为已读</p>
                  </div>
                  <button
                    onClick={() => setSettings({ autoMarkRead: !autoMarkRead })}
                    className={cn(
                      'w-11 h-6 rounded-full transition-colors relative flex-shrink-0',
                      autoMarkRead ? 'bg-primary-500' : 'bg-gray-300'
                    )}
                  >
                    <span className={cn(
                      'absolute top-0.5 w-5 h-5 rounded-full bg-white transition-transform shadow-sm',
                      autoMarkRead ? 'left-5' : 'left-0.5'
                    )} />
                  </button>
                </div>
              </div>
            </div>
          </div>

          {isAdmin && (
            <div className="card">
              <div className="card-body space-y-6">
                <div>
                  <h3 className="text-base font-semibold text-gray-900">用户管理</h3>
                  <p className="mt-1 text-xs text-gray-500">
                    仅管理员可创建账号；审批人（approver）用于处理 HITL 审批。
                  </p>
                </div>

                <div className="grid gap-3 sm:grid-cols-[1fr_1fr_150px_auto]">
                  <input
                    className="input"
                    placeholder="用户名（3-32 字符）"
                    value={userDraft.username}
                    onChange={(e) => setUserDraft((draft) => ({ ...draft, username: e.target.value }))}
                  />
                  <input
                    className="input"
                    type="password"
                    placeholder="密码（至少 6 位）"
                    value={userDraft.password}
                    onChange={(e) => setUserDraft((draft) => ({ ...draft, password: e.target.value }))}
                  />
                  <select
                    className="input"
                    value={userDraft.role}
                    onChange={(e) => setUserDraft((draft) => ({ ...draft, role: e.target.value as UserRole }))}
                  >
                    <option value="viewer">只读</option>
                    <option value="operator">操作员</option>
                    <option value="approver">审批人</option>
                    <option value="admin">管理员</option>
                  </select>
                  <button
                    type="button"
                    onClick={handleCreateUser}
                    disabled={creatingUser}
                    className="btn-primary"
                  >
                    {creatingUser ? (
                      <><Loader2 className="w-4 h-4 animate-spin" />创建中...</>
                    ) : (
                      <><UserPlus className="w-4 h-4" />创建用户</>
                    )}
                  </button>
                </div>

                <div className="divide-y divide-gray-100 rounded-lg border border-gray-200">
                  {users.length === 0 ? (
                    <p className="px-4 py-3 text-sm text-gray-500">暂无用户数据</p>
                  ) : users.map((item) => (
                    <div key={item.id} className="flex items-center justify-between px-4 py-3">
                      <span className="text-sm font-medium text-gray-900">{item.username}</span>
                      <span className="text-xs text-gray-500">{roleLabels[item.role] ?? item.role}</span>
                    </div>
                  ))}
                </div>
              </div>
            </div>
          )}
        </div>
      </div>
    </div>
  )
}
