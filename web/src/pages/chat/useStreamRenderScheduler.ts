import { useCallback, useEffect, useRef } from 'react'

interface Cursor {
  lastRender: number
  pending: Map<string, () => void>
  timer?: ReturnType<typeof setTimeout>
  frame?: number
}

// Only transient render cursors live here. Chat's existing messages state stays
// authoritative; each message shares one 100 ms + animation-frame budget.
export function useStreamRenderScheduler() {
  const cursors = useRef(new Map<string, Cursor>())
  const mounted = useRef(true)
  const flush = useCallback((id: string) => {
    const cursor = cursors.current.get(id)
    if (!cursor) return
    clearTimeout(cursor.timer)
    if (cursor.frame !== undefined) cancelAnimationFrame(cursor.frame)
    cursor.timer = undefined
    cursor.frame = undefined
    cursor.lastRender = performance.now()
    const pending = [...cursor.pending.values()]
    cursor.pending.clear()
    if (mounted.current) pending.forEach((render) => render())
  }, [])
  const schedule = useCallback((id: string, field: string, render: () => void) => {
    if (!mounted.current) return false
    let cursor = cursors.current.get(id)
    if (!cursor) {
      cursor = { lastRender: performance.now(), pending: new Map() }
      cursors.current.set(id, cursor)
    }
    cursor.pending.set(field, render)
    if (cursor.timer !== undefined || cursor.frame !== undefined) return true
    const active = cursor
    active.timer = setTimeout(() => {
      active.timer = undefined
      active.frame = requestAnimationFrame(() => flush(id))
    }, Math.max(0, 100 - (performance.now() - active.lastRender)))
    return true
  }, [flush])
  const finish = useCallback((id: string) => {
    flush(id)
    cursors.current.delete(id)
  }, [flush])
  useEffect(() => {
    mounted.current = true
    const active = cursors.current
    return () => {
      mounted.current = false
      active.forEach((cursor) => {
        clearTimeout(cursor.timer)
        if (cursor.frame !== undefined) cancelAnimationFrame(cursor.frame)
      })
      active.clear()
    }
  }, [])
  return { schedule, finish }
}
