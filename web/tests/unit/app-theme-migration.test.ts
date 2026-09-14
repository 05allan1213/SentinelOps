import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createStore } from 'zustand/vanilla'
import { createJSONStorage, persist } from 'zustand/middleware'

beforeEach(() => {
  localStorage.clear()
  vi.resetModules()
})

describe('Zustand 5.0.15 malformed storage behavior', () => {
  it('keeps defaults but aborts hydration before migrate on invalid JSON', async () => {
    localStorage.setItem('app-storage', '{broken')
    const migrate = vi.fn(() => ({ theme: 'light' }))
    const onFinish = vi.fn()
    const store = createStore()(persist(() => ({ theme: 'light' }), {
      name: 'app-storage',
      version: 2,
      storage: createJSONStorage(() => localStorage),
      migrate,
      onRehydrateStorage: () => onFinish,
    }))

    await store.persist.rehydrate()

    expect(store.getState().theme).toBe('light')
    expect(migrate).not.toHaveBeenCalled()
    expect(store.persist.hasHydrated()).toBe(false)
    expect(onFinish).toHaveBeenLastCalledWith(undefined, expect.any(SyntaxError))
    expect(localStorage.getItem('app-storage')).toBe('{broken')
  })
})

describe('app light-only migration', () => {
  it.each([
    ['absent storage', null],
    ['malformed JSON', '{broken'],
    ['null envelope', 'null'],
    ['missing state', JSON.stringify({ version: 1 })],
    ['missing fields', JSON.stringify({ state: {}, version: 0 })],
    ['missing version', JSON.stringify({ state: { theme: 'dark' } })],
    ['version 0 dark', JSON.stringify({ state: { theme: 'dark' }, version: 0 })],
    ['version 1 dark', JSON.stringify({ state: { theme: 'dark' }, version: 1 })],
    ['version 1 light', JSON.stringify({ state: { theme: 'light' }, version: 1 })],
    ['version 2 light', JSON.stringify({ state: { theme: 'light' }, version: 2 })],
    ['version 2 dark', JSON.stringify({ state: { theme: 'dark' }, version: 2 })],
  ])('%s hydrates to light without touching other storage', async (_, raw) => {
    if (raw !== null) localStorage.setItem('app-storage', raw)
    localStorage.setItem('token', 'unchanged-token')
    localStorage.setItem('chat-storage', '{untouched')
    const { useAppStore } = await import('../../src/stores/app')

    expect(useAppStore.persist.hasHydrated()).toBe(true)
    expect(useAppStore.getState()).toMatchObject({ theme: 'light', sidebarCollapsed: false, sidebarWidth: 288 })
    expect(JSON.parse(localStorage.getItem('app-storage')!)).toMatchObject({ state: { theme: 'light' }, version: 2 })
    expect(localStorage.getItem('token')).toBe('unchanged-token')
    expect(localStorage.getItem('chat-storage')).toBe('{untouched')
  })

  it.each([0, 1, 2])('preserves sidebar preferences and actions through version %i and reload', async (version) => {
    localStorage.setItem('app-storage', JSON.stringify({
      state: { theme: 'dark', sidebarCollapsed: true, sidebarWidth: 320 }, version,
    }))
    const { useAppStore } = await import('../../src/stores/app')
    expect(useAppStore.getState()).toMatchObject({ theme: 'light', sidebarCollapsed: true, sidebarWidth: 320 })
    useAppStore.getState().toggleSidebar()
    useAppStore.getState().setSidebarWidth(304)
    useAppStore.getState().setTheme('dark')
    expect(useAppStore.getState()).toMatchObject({ theme: 'light', sidebarCollapsed: false, sidebarWidth: 304 })
    await useAppStore.persist.rehydrate()
    expect(useAppStore.getState()).toMatchObject({ theme: 'light', sidebarCollapsed: false, sidebarWidth: 304 })
    expect(JSON.parse(localStorage.getItem('app-storage')!)).toMatchObject({
      state: { theme: 'light', sidebarCollapsed: false, sidebarWidth: 304 }, version: 2,
    })
  })

  it('ignores invalid persisted fields without overwriting store actions', async () => {
    localStorage.setItem('app-storage', JSON.stringify({
      state: { theme: null, sidebarCollapsed: 'yes', sidebarWidth: null, setTheme: null }, version: 2,
    }))
    const { useAppStore } = await import('../../src/stores/app')
    expect(useAppStore.getState()).toMatchObject({ theme: 'light', sidebarCollapsed: false, sidebarWidth: 288 })
    expect(useAppStore.getState().setTheme).toBeTypeOf('function')
  })
})
