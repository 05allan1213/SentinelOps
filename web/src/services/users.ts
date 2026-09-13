import api from './api'
import type { ApiResponse } from '@/types'

export type UserRole = 'viewer' | 'operator' | 'approver' | 'admin'

export interface UserItem {
  id: string
  username: string
  role: string
  created_at: string
}

export interface CreateUserInput {
  username: string
  password: string
  role: UserRole
}

// 用户管理复用 admin-only 的 /auth/v1/register 与 /auth/v1/users；不开放匿名注册。
export const usersService = {
  async list(): Promise<UserItem[]> {
    const res = await api.get<ApiResponse<{ items: UserItem[] }>>('/auth/v1/users')
    return res.data.data?.items ?? []
  },

  async create(input: CreateUserInput): Promise<UserItem> {
    const res = await api.post<ApiResponse<{ user_id: string; username: string; role: string }>>(
      '/auth/v1/register',
      input,
    )
    const data = res.data.data
    if (!data) throw new Error('创建用户失败')
    return { id: data.user_id, username: data.username, role: data.role, created_at: new Date().toISOString() }
  },
}
