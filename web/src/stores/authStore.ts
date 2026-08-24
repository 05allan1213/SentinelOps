import { create } from 'zustand'
import { persist } from 'zustand/middleware'

interface AuthState {
  token: string | null
  userID: string | null
  role: string | null
  username: string | null
  setAuth: (token: string, userID: string, role: string, username: string) => void
  logout: () => void
}

export const useAuthStore = create<AuthState>()(
  persist(
    (set) => ({
      token: null,
      userID: null,
      role: null,
      username: null,
      setAuth: (token, userID, role, username) => {
        localStorage.setItem('token', token)
        set({ token, userID, role, username })
      },
      logout: () => {
        localStorage.removeItem('token')
        set({ token: null, userID: null, role: null, username: null })
      },
    }),
    { name: 'auth-storage' }
  )
)
