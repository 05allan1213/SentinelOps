import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import Button from '@/components/common/Button'
import Pagination from '@/components/common/Pagination'
import StatePanel from '@/components/common/StatePanel'
import Alert from '@/components/common/Alert'

afterEach(cleanup)

it('loading button keeps its accessible name and cannot submit or invoke an action', () => {
  const click = vi.fn()
  const { rerender } = render(<Button onClick={click}>保存</Button>)
  expect(screen.getByRole('button', { name: '保存' })).toHaveAttribute('type', 'button')
  fireEvent.click(screen.getByRole('button'))
  expect(click).toHaveBeenCalledTimes(1)
  rerender(<Button loading onClick={click}>保存</Button>)
  expect(screen.getByRole('button', { name: '保存' })).toBeDisabled()
  expect(screen.getByRole('button')).toHaveAttribute('aria-busy', 'true')
  fireEvent.click(screen.getByRole('button'))
  expect(click).toHaveBeenCalledTimes(1)
})

it('pagination sends page and size requests without slicing data or resetting the page itself', () => {
  const page = vi.fn(), size = vi.fn()
  render(<Pagination page={2} pageSize={20} total={90} totalPages={5} onPageChange={page} onPageSizeChange={size} />)
  fireEvent.click(screen.getByRole('button', { name: '上一页' }))
  expect(page).toHaveBeenLastCalledWith(1)
  fireEvent.click(screen.getByRole('button', { name: '下一页' }))
  expect(page).toHaveBeenLastCalledWith(3)
  fireEvent.change(screen.getByLabelText('每页条数'), { target: { value: '50' } })
  expect(size).toHaveBeenCalledWith(50)
  expect(page).toHaveBeenCalledTimes(2)
  for (const value of ['0', '6', '2.5']) {
    fireEvent.change(screen.getByLabelText('跳转页码'), { target: { value } })
    fireEvent.keyDown(screen.getByLabelText('跳转页码'), { key: 'Enter' })
    expect(page).toHaveBeenCalledTimes(2)
  }
  fireEvent.change(screen.getByLabelText('跳转页码'), { target: { value: '4' } })
  fireEvent.keyDown(screen.getByLabelText('跳转页码'), { key: 'Enter' })
  expect(page).toHaveBeenLastCalledWith(4)
})

it('pagination respects empty, out-of-range and fetching states without inventing a page', () => {
  const change = vi.fn()
  const { rerender } = render(<Pagination page={1} pageSize={20} total={0} totalPages={0} onPageChange={change} />)
  expect(screen.getByText('共 0 条')).toBeVisible()
  expect(screen.getByRole('button', { name: '上一页' })).toBeDisabled()
  expect(screen.getByRole('button', { name: '下一页' })).toBeDisabled()
  rerender(<Pagination page={5} pageSize={20} total={30} totalPages={2} onPageChange={change} />)
  expect(screen.getByText('5 / 2')).toBeVisible()
  expect(change).not.toHaveBeenCalled()
  rerender(<Pagination page={2} pageSize={20} total={90} totalPages={5} onPageChange={change} onPageSizeChange={vi.fn()} isFetching />)
  for (const control of screen.getAllByRole('button')) expect(control).toBeDisabled()
  expect(screen.getByRole('combobox')).toBeDisabled()
  expect(screen.getByRole('spinbutton')).toBeDisabled()
})

it('retains the legacy callback and zero-page visibility contract', () => {
  const change = vi.fn()
  const { container, rerender } = render(<Pagination page={1} totalPages={0} total={0} onChange={change} />)
  expect(container).toBeEmptyDOMElement()
  rerender(<Pagination page={1} totalPages={2} total={30} onChange={change} />)
  fireEvent.click(screen.getAllByRole('button')[1])
  expect(change).toHaveBeenCalledWith(2)
})

it.each(['loading', 'empty', 'error', 'unavailable', 'not-run', 'partial'] as const)('keeps %s distinct and exposes caller-owned raw details', kind => {
  render(<StatePanel kind={kind} title={kind} description="unknown" technicalDetail="reason_code=not_observed; availability=unavailable; data_quality=partial; not_run" />)
  expect(screen.getByRole(kind === 'error' ? 'alert' : 'status')).toHaveTextContent(kind)
  expect(screen.getByText('unknown')).toBeVisible()
  expect(screen.getByText(/reason_code=not_observed/)).toBeInTheDocument()
  expect(screen.queryByText('success')).not.toBeInTheDocument()
})

it('alert keeps its action separate from caller-owned message semantics', () => {
  const click = vi.fn()
  render(<Alert tone="warning" action={<Button onClick={click}>重试</Button>}>not_observed</Alert>)
  expect(screen.getByRole('status')).toHaveTextContent('not_observed')
  fireEvent.click(screen.getByRole('button', { name: '重试' }))
  expect(click).toHaveBeenCalledOnce()
})
