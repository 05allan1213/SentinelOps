import { create } from 'zustand'
import { createJSONStorage, persist } from 'zustand/middleware'

interface AppState {
  sidebarCollapsed: boolean
  sidebarWidth: number
  theme: 'dark' | 'light'
  toggleSidebar: () => void
  setSidebarWidth: (width: number) => void
  setTheme: (theme: 'dark' | 'light') => void
}

function lightPreferences(persisted: unknown): Partial<AppState> {
  const state = persisted && typeof persisted === 'object' ? persisted as Record<string, unknown> : {}
  return {
    theme: 'light',
    ...(typeof state.sidebarCollapsed === 'boolean' ? { sidebarCollapsed: state.sidebarCollapsed } : {}),
    ...(typeof state.sidebarWidth === 'number' && Number.isFinite(state.sidebarWidth) ? { sidebarWidth: state.sidebarWidth } : {}),
  }
}

export const useAppStore = create<AppState>()(
  persist(
    (set) => ({
      sidebarCollapsed: false,
      sidebarWidth: 288,
      theme: 'light',
      toggleSidebar: () => set((state) => ({ sidebarCollapsed: !state.sidebarCollapsed })),
      setSidebarWidth: (width) => set({ sidebarWidth: width }),
      setTheme: () => set({ theme: 'light' }),
    }),
    {
      name: 'app-storage',
      version: 2,
      storage: createJSONStorage(() => ({
        getItem: (name) => {
          const raw = localStorage.getItem(name)
          // Zustand 5.0.15 aborts hydration on JSON errors before migrate runs.
          try {
            if (raw !== null) JSON.parse(raw)
            return raw
          } catch (error) {
            if (error instanceof SyntaxError) return null
            throw error
          }
        },
        setItem: (name, value) => localStorage.setItem(name, value),
        removeItem: (name) => localStorage.removeItem(name),
      })),
      migrate: lightPreferences,
      merge: (persisted, current) => ({ ...current, ...lightPreferences(persisted) }),
      onRehydrateStorage: () => (state, error) => {
        if (!error) state?.setTheme('light')
      },
    }
  )
)
