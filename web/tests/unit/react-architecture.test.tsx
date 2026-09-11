import { render, screen, cleanup } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import RouteContent from '@/components/layout/RouteContent'
afterEach(() => { cleanup(); vi.restoreAllMocks() })
it('a failed route preserves a usable navigation sibling and gives recovery', () => {
  vi.spyOn(console, 'error').mockImplementation(() => {})
  function Broken(): never { throw new Error('chunk failure') }
  render(<><nav>Navigation remains</nav><RouteContent><Broken /></RouteContent></>)
  expect(screen.getByRole('navigation')).toHaveTextContent('Navigation remains')
  expect(screen.getByRole('alert')).toHaveTextContent('页面暂时无法显示')
  expect(screen.getByRole('button', { name: '刷新重试' })).toBeEnabled()
})
