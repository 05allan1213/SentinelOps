import axios, { AxiosInstance, AxiosResponse } from 'axios'
import { ApiResponse } from '@/types'
import toast from 'react-hot-toast'

const api: AxiosInstance = axios.create({
  baseURL: '/api',
  timeout: 30000,
  headers: {
    'Content-Type': 'application/json',
  },
})

// 请求拦截器
api.interceptors.request.use(
  (config) => {
    const token = localStorage.getItem('token')
    if (token) {
      config.headers.Authorization = `Bearer ${token}`
    }
    return config
  },
  (error) => Promise.reject(error)
)

// 响应拦截器
api.interceptors.response.use(
  (response: AxiosResponse<ApiResponse<unknown>>) => {
    const { data } = response
    if (data.code !== undefined && data.code !== 0 && data.code !== 200) {
      return Promise.reject(new Error(data.message || '请求失败'))
    }
    if (data.message && data.message !== 'OK' && (data.data === undefined || data.data === null)) {
      return Promise.reject(new Error(data.message))
    }
    return response
  },
  async (error) => {
    const status = error.response?.status
    const config = error.config

    if (status === 401) {
      localStorage.removeItem('token')
      window.location.href = '/login'
      return Promise.reject(error)
    }

    // 429 自动重试（指数退避，最多 3 次）
    if (status === 429) {
      config._retryCount = (config._retryCount ?? 0) + 1
      if (config._retryCount <= 3) {
        const delay = Math.min(1000 * 2 ** (config._retryCount - 1), 8000) // 1s, 2s, 4s
        await new Promise(r => setTimeout(r, delay))
        return api(config)
      }
      // 重试耗尽，提示用户
      toast.error('操作过于频繁，请稍后再试', { id: 'rate-limit', duration: 4000 })
      return Promise.reject(new Error('请求过于频繁，请稍后重试'))
    }

    const statusMsgMap: Record<number, string> = {
      503: '当前请求过多，请稍后重试',
    }
    const message = statusMsgMap[status] ?? error.response?.data?.message ?? error.message ?? '网络错误'
    return Promise.reject(new Error(message))
  }
)

export default api
