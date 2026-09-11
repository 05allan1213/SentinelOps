// Test-only controlled Runtime SSE transport. No product routes, stores or business services.

export interface RuntimeSSEFixture {
  urls: string[]
  cancels: number
  runFetches: number
  status: string
  emit: (seq: number, type: string, extra?: Record<string, unknown>) => void
  close: () => void
}

declare global {
  interface Window { runtimeSSEFixture: RuntimeSSEFixture }
}

export const fixture: RuntimeSSEFixture = {
  urls: [],
  cancels: 0,
  runFetches: 0,
  status: 'running',
  emit: () => {},
  close: () => {},
}

const encoder = new TextEncoder()

const eventBody = (seq: number, type: string, extra: Record<string, unknown> = {}) => JSON.stringify({
  seq,
  run_id: 'run-1',
  event_type: type,
  attempt: 1,
  generation: 1,
  trace_id: 'trace-1',
  summary: `event-${seq}`,
  created_at: `2026-09-10T00:00:0${seq}.000Z`,
  availability: 'available',
  data_quality: 'complete',
  ...extra,
})

export function installRuntimeSSEFixture() {
  window.runtimeSSEFixture = fixture
  const realFetch = window.fetch.bind(window)
  window.fetch = async (input, init) => {
    const url = String(input)
    if (url.startsWith('/api/runtime/v1/runs/run-1/events')) {
      fixture.urls.push(url)
      return new Response(new ReadableStream<Uint8Array>({
        start(controller) {
          fixture.emit = (seq, type, extra) => {
            controller.enqueue(encoder.encode(`id: ${seq}\nevent: ${type}\ndata: ${eventBody(seq, type, extra)}\n\n`))
          }
          fixture.close = () => controller.close()
        },
        cancel() { fixture.cancels += 1 },
      }), { headers: { 'Content-Type': 'text/event-stream' } })
    }
    return realFetch(input, init)
  }
}
