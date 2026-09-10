import { chatService } from '@/services/chat'
import api from '@/services/api'

it('does not retry a v2 create rejected with HTTP 429 through the actual Axios interceptor', async () => {
  vi.useFakeTimers()
  const previous = api.defaults.adapter
  const adapter = vi.fn(async (config) => { throw { config, response: { status: 429, data: { message: 'rate limited' } }, message: 'rate limited' } })
  api.defaults.adapter = adapter
  try {
    const outcome = chatService.createDurableRun({ sessionId: 's', query: 'q' }).catch(error => error)
    await vi.runAllTimersAsync()
    expect(await outcome).toBeInstanceOf(Error)
    expect(adapter).toHaveBeenCalledTimes(1)
    expect(adapter.mock.calls[0][0].url).toBe('/chat/v2/runs')
  } finally { api.defaults.adapter = previous; vi.useRealTimers() }
})
