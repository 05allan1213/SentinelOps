import { fireEvent, render, screen, cleanup } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import ConfirmDialog from '@/components/common/ConfirmDialog'
afterEach(cleanup)
it('has labelled dialog, description, required reason and escape dismissal', () => {
  const close = vi.fn()
  render(<ConfirmDialog open title="Confirm recovery" description="Audited operation" reasonRequired onReasonChange={() => {}} onClose={close} onConfirm={() => {}} />)
  const dialog = screen.getByRole('dialog', { name: 'Confirm recovery' })
  expect(dialog).toHaveAccessibleDescription('Audited operation')
  expect(screen.getByRole('button', { name: '删除' })).toBeDisabled()
  expect(screen.getByRole('button', { name: '关闭' })).toBeInTheDocument()
  fireEvent.keyDown(dialog, { key: 'Escape' })
  expect(close).toHaveBeenCalled()
})
