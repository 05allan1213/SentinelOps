/**
 * streamFetch 统一标准 SSE 协议的读流工具。
 *
 * 解析后端推送的标准 SSE 格式：
 *   id: <seq>\n
 *   event: <type>\n
 *   data: <content>\n
 *   \n
 *
 * 流结束标志：data: [DONE]\n\n
 *
 * ── 问题三：流式中断恢复能力（前端协议层）──────────────────────────────────
 * 问题根因：
 *   浏览器 EventSource 不支持 POST，且断线后无法携带自定义游标重连。
 *   原始实现忽略了 SSE 协议的 id 字段，导致前端无法追踪已收到的事件位置，
 *   断线后只能从头重新请求，后端被迫重新执行 Agent。
 *
 * 设计思路：
 *   解析每条 SSE 事件的 id 字段（后端用 atomic.Int64 保证全局单调递增），
 *   通过第三个参数 id 透传给调用方（chat.ts）。
 *   调用方将 id 持久化到 sessionStorage（key: chat_last_seq_<sessionId>），
 *   断线重连时作为 last_seq 随请求体发送给后端，后端据此补发缺失事件。
 */
export function streamFetch(
  url: string,
  body: unknown,
  onChunk: (type: string, content: string, id?: string) => void,
  onDone: () => void,
  onError?: (e: Error) => void,
  signal?: AbortSignal
): void {
  const token = localStorage.getItem('token')
  fetch(url, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
    },
    body: JSON.stringify(body),
    signal,
  })
    .then(async res => {
      if (!res.ok) {
        const errMap: Record<number, string> = {
          401: '请先登录',
          429: '请求过于频繁，请稍后重试',
          503: '当前请求过多，请稍后重试',
        }
        const msg = errMap[res.status] ?? `请求失败（${res.status}）`
        onError?.(new Error(msg))
        return
      }
      const reader = res.body?.getReader()
      if (!reader) { onDone(); return }

      const decoder = new TextDecoder()
      let buffer = ''
      let currentId = ''
      let currentEvent = ''
      let currentData = ''

      while (true) {
        const { done, value } = await reader.read()
        if (done) break

        buffer += decoder.decode(value, { stream: true })
        const lines = buffer.split('\n')
        buffer = lines.pop() ?? ''

        for (const line of lines) {
          if (line.startsWith('id: ')) {
            currentId = line.slice(4).trim()
          } else if (line.startsWith('event: ')) {
            currentEvent = line.slice(7).trim()
          } else if (line.startsWith('data: ')) {
            const data = line.slice(6)
            if (data === '[DONE]') { onDone(); return }
            if (currentData) currentData += '\n'
            currentData += data
          } else if (line === '') {
            if (currentEvent && currentData) {
              if (currentEvent === 'done') { onDone(); return }
              if (currentEvent === 'error') { onError?.(new Error(currentData)); return }
              if (currentEvent !== 'connected') {
                onChunk(currentEvent, currentData, currentId || undefined)
              }
            }
            currentId = ''
            currentEvent = ''
            currentData = ''
          }
        }
      }
      onDone()
    })
    .catch(e => {
      if (e instanceof Error && e.name === 'AbortError') return
      onError?.(e instanceof Error ? e : new Error(String(e)))
    })
}
