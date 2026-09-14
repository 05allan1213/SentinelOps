import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import BaseCreateModal from '../../src/pages/knowledge/components/BaseCreateModal'
import RebuildModal from '../../src/pages/knowledge/components/RebuildModal'
import EventDetailModal from '../../src/pages/events/components/EventDetailModal'
import { knowledgeService } from '../../src/services/knowledge'
import { eventService } from '../../src/services/event'
import type { SecurityEvent } from '../../src/types'

vi.mock('../../src/services/knowledge', () => ({ knowledgeService: { createBase: vi.fn(), rebuildDoc: vi.fn(), batchRebuildDocs: vi.fn() } }))
vi.mock('../../src/services/event', () => ({ eventService: { updateStatus: vi.fn() } }))
vi.mock('react-hot-toast', () => ({ default: { success: vi.fn(), error: vi.fn() } }))
afterEach(cleanup)
beforeEach(() => vi.clearAllMocks())

describe('business dialog mutation contracts', () => {
  it('retains create validation, trims the request, and only signals success after resolution', async () => {
    const onSuccess = vi.fn()
    vi.mocked(knowledgeService.createBase).mockRejectedValueOnce(new Error('unavailable'))
    render(<BaseCreateModal onClose={vi.fn()} onSuccess={onSuccess} />)
    fireEvent.click(screen.getByRole('button', { name: '创建', exact: true }))
    expect(knowledgeService.createBase).not.toHaveBeenCalled()
    fireEvent.change(screen.getByPlaceholderText('如：CVE 漏洞处置手册'), { target: { value: '  test input  ' } })
    fireEvent.change(screen.getByPlaceholderText('知识库用途说明（可选）'), { target: { value: '  description  ' } })
    fireEvent.click(screen.getByRole('button', { name: '创建', exact: true }))
    await waitFor(() => expect(knowledgeService.createBase).toHaveBeenCalledWith('test input', 'description'))
    await waitFor(() => expect(screen.getByRole('button', { name: '创建', exact: true })).not.toBeDisabled())
    expect(onSuccess).not.toHaveBeenCalled()
    expect(screen.getByRole('dialog')).toBeInTheDocument()
  })

  it.each([false, true])('preserves single/batch rebuild request and failure state (batch=%s)', async batch => {
    const onSuccess = vi.fn()
    vi.mocked(knowledgeService.rebuildDoc).mockRejectedValue(new Error('unavailable'))
    vi.mocked(knowledgeService.batchRebuildDocs).mockRejectedValue(new Error('unavailable'))
    render(<RebuildModal docId={batch ? undefined : 'test-doc'} docIds={batch ? ['test-doc', 'test-doc-2'] : undefined} currentStrategy="code" onClose={vi.fn()} onSuccess={onSuccess} />)
    fireEvent.click(screen.getByRole('button', { name: '确认重建' }))
    await waitFor(() => expect(batch ? knowledgeService.batchRebuildDocs : knowledgeService.rebuildDoc).toHaveBeenCalledWith(batch ? ['test-doc', 'test-doc-2'] : 'test-doc', 'code'))
    await waitFor(() => expect(screen.getByRole('button', { name: '确认重建' })).not.toBeDisabled())
    expect(onSuccess).not.toHaveBeenCalled()
  })

  it('retains event status rollback on API failure without publishing an update', async () => {
    // Explicit unit-test input, never a product fallback or provider result.
    const event: SecurityEvent = { id: 'test-event', subscription_id: 1, title: 'Event contract test', severity: 'high', status: 'new', source: 'test', source_url: '', event_time: '', created_at: '' }
    const onUpdate = vi.fn()
    vi.mocked(eventService.updateStatus).mockRejectedValue(new Error('unavailable'))
    render(<EventDetailModal event={event} onClose={vi.fn()} onUpdate={onUpdate} />)
    fireEvent.click(screen.getByRole('combobox'))
    fireEvent.click(screen.getByRole('option', { name: '已解决' }))
    await waitFor(() => expect(eventService.updateStatus).toHaveBeenCalledWith('test-event', 'resolved'))
    await waitFor(() => expect(screen.getByRole('combobox')).toHaveTextContent('新建'))
    expect(onUpdate).not.toHaveBeenCalled()
  })
})
