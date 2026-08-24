import { useState, useEffect } from 'react'
import { Settings as SettingsIcon, Save, Loader2, User, LogOut } from 'lucide-react'
import { cn } from '@/utils'
import toast from 'react-hot-toast'
import { useSettingsStore } from '@/stores/settingsStore'
import { settingsService } from '@/services/settings'
import { useAuthStore } from '@/stores/authStore'

export default function Settings() {
  const { siteName, autoMarkRead, setSettings } = useSettingsStore()
  const { username, role, logout } = useAuthStore()
  const [draftName, setDraftName] = useState(siteName)
  const [isSaving, setIsSaving] = useState(false)
  const [loading, setLoading] = useState(true)

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

  if (loading) {
    return (
      <div className="flex items-center justify-center h-48">
        <Loader2 className="w-6 h-6 animate-spin text-primary-500" />
      </div>
    )
  }

  return (
    <div className="space-y-6">
      {/* Page Header */}
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
        {/* Sidebar */}
        <div className="w-48 flex-shrink-0">
          <nav className="card p-2">
            <button className="w-full flex items-center gap-3 px-4 py-3 rounded-lg bg-primary-50 text-primary-600 shadow-sm text-left text-sm font-medium">
              <SettingsIcon className="w-4 h-4 flex-shrink-0" />
              <span>通用设置</span>
            </button>
          </nav>
        </div>

        {/* Content */}
        <div className="flex-1 space-y-6">
          {/* Admin User Info Card */}
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
        </div>
      </div>
    </div>
  )
}
