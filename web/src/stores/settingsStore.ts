import { create } from 'zustand'
import { persist } from 'zustand/middleware'

export interface GeneralSettings {
  siteName: string
  autoMarkRead: boolean
}

interface SettingsStore extends GeneralSettings {
  setSettings: (s: Partial<GeneralSettings>) => void
}

export const useSettingsStore = create<SettingsStore>()(
  persist(
    (set) => ({
      siteName: '安全事件智能研判多智能体协同平台',
      autoMarkRead: true,
      setSettings: (s) => set(s),
    }),
    { name: 'sentinelops-settings' },
  ),
)
