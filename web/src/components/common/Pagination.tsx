import { useState, useRef, useEffect } from 'react'
import { ChevronLeft, ChevronRight, ChevronDown, Check } from 'lucide-react'
import { cn } from '@/utils'
import Button from './Button'

const PAGE_SIZE_OPTIONS = [5, 10, 20, 50, 100]

interface LegacyPaginationProps {
  page: number
  totalPages: number
  total: number
  onChange: (page: number) => void
  /** 当前每页条数（传入时显示每页条数切换器） */
  pageSize?: number
  /** 每页条数变更回调 */
  onPageSizeChange?: (size: number) => void
}

function PageSizeSelect({ value, onChange }: { value: number; onChange: (n: number) => void }) {
  const [open, setOpen] = useState(false)
  const ref = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!open) return
    const handler = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false)
    }
    document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [open])

  return (
    <div ref={ref} className="relative">
      <button
        onClick={() => setOpen(v => !v)}
        className={cn(
          'flex items-center gap-1 h-7 px-2.5 text-sm rounded-lg border transition-all duration-150',
          open
            ? 'border-indigo-400 bg-indigo-50 text-indigo-700 ring-2 ring-indigo-500/20'
            : 'border-gray-200 bg-white text-gray-600 hover:border-gray-300 hover:bg-gray-50'
        )}
      >
        <span className="tabular-nums font-semibold text-gray-800 tracking-tight">{value}</span>
        <span className="text-gray-500 text-sm ml-0.5">条</span>
        <ChevronDown className={cn('w-3 h-3 text-gray-400 ml-0.5 transition-transform duration-150', open && 'rotate-180')} />
      </button>

      {open && (
        <div className="absolute bottom-full mb-1.5 left-0 z-50 bg-white rounded-xl shadow-lg border border-gray-100 py-1 min-w-[88px] overflow-hidden"
          style={{ boxShadow: '0 4px 20px rgba(0,0,0,0.10), 0 1px 4px rgba(0,0,0,0.06)' }}
        >
          {PAGE_SIZE_OPTIONS.map(n => (
            <button
              key={n}
              onClick={() => { onChange(n); setOpen(false) }}
              className={cn(
                'w-full flex items-center justify-between px-3 py-1.5 text-sm transition-colors duration-100',
                n === value
                  ? 'bg-indigo-50 text-indigo-700'
                  : 'text-gray-700 hover:bg-gray-50'
              )}
            >
              <span>{n} 条</span>
              {n === value && <Check className="w-3.5 h-3.5 text-indigo-500" />}
            </button>
          ))}
        </div>
      )}
    </div>
  )
}

// Retained until the existing onChange callers migrate and pass their regression gates.
function LegacyPagination({ page, totalPages, total, onChange, pageSize, onPageSizeChange }: LegacyPaginationProps) {
  const [jumpInput, setJumpInput] = useState('')

  const handleJump = () => {
    const target = parseInt(jumpInput, 10)
    if (!isNaN(target) && target >= 1 && target <= totalPages) {
      onChange(target)
    }
    setJumpInput('')
  }

  if (totalPages <= 0) return null

  return (
    <div className="px-4 py-3 border-t border-gray-100 flex items-center justify-between flex-shrink-0 bg-white rounded-b-2xl">
      {/* 左侧：总条数 + 每页条数切换 */}
      <div className="flex items-center gap-3">
        <span className="text-sm text-gray-500">共 <span className="font-medium text-gray-700">{total}</span> 条</span>
        {onPageSizeChange && pageSize && (
          <div className="flex items-center gap-1.5">
            <span className="text-sm text-gray-400">每页</span>
            <PageSizeSelect value={pageSize} onChange={onPageSizeChange} />
          </div>
        )}
      </div>

      {/* 右侧：分页控件 */}
      <div className="flex items-center gap-1.5">
        {/* 上一页 */}
        <button
          className={cn('pagination-item', page <= 1 && 'disabled')}
          onClick={() => onChange(Math.max(1, page - 1))}
          disabled={page <= 1}
        >
          <ChevronLeft className="w-4 h-4" />
        </button>

        {/* 页码信息 */}
        <span className="text-sm text-gray-600 px-2 whitespace-nowrap tabular-nums">
          {page}
          <span className="text-gray-300 mx-1.5">/</span>
          {totalPages}
        </span>

        {/* 下一页 */}
        <button
          className={cn('pagination-item', page >= totalPages && 'disabled')}
          onClick={() => onChange(Math.min(totalPages, page + 1))}
          disabled={page >= totalPages}
        >
          <ChevronRight className="w-4 h-4" />
        </button>

        {/* 跳转 */}
        {totalPages > 2 && (
          <div className="flex items-center gap-1.5 ml-2 pl-2 border-l border-gray-200">
            <span className="text-sm text-gray-400">跳至</span>
            <input
              type="number"
              min={1}
              max={totalPages}
              value={jumpInput}
              onChange={e => setJumpInput(e.target.value)}
              onKeyDown={e => e.key === 'Enter' && handleJump()}
              className="w-12 h-7 text-sm text-center border border-gray-200 rounded-lg focus:outline-none focus:ring-1 focus:ring-gray-300 focus:border-gray-400"
              placeholder={String(page)}
            />
            <button
              onClick={handleJump}
              className="h-7 px-2 text-sm rounded-lg border border-gray-200 text-gray-600 hover:bg-gray-50 transition-colors"
            >
              确定
            </button>
          </div>
        )}
      </div>
    </div>
  )
}

export interface PaginationProps {
  page: number
  pageSize: number
  total: number
  totalPages: number
  onPageChange: (page: number) => void
  onPageSizeChange?: (size: number) => void
  disabled?: boolean
  isFetching?: boolean
  'aria-label'?: string
}

function PaginationControls({ page, pageSize, total, totalPages, onPageChange, onPageSizeChange, disabled = false, isFetching = false, 'aria-label': label = '分页' }: PaginationProps) {
  const [jumpInput, setJumpInput] = useState('')
  const blocked = disabled || isFetching
  const target = Number(jumpInput)
  const validJump = jumpInput.trim() !== '' && Number.isInteger(target) && target >= 1 && target <= totalPages
  const jump = () => {
    if (blocked || !validJump) return
    onPageChange(target)
    setJumpInput('')
  }
  const controlClass = 'h-8 rounded-lg border border-gray-300 bg-white text-sm text-gray-700 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 focus-visible:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-50'

  return <nav aria-label={label} aria-busy={isFetching || undefined} className="flex min-w-0 flex-wrap items-center justify-between gap-3">
    <div className="flex flex-wrap items-center gap-3 text-sm text-gray-600">
      <span className="tabular-nums">共 {total} 条</span>
      {onPageSizeChange && <label className="flex items-center gap-2">每页
        <select aria-label="每页条数" value={pageSize} disabled={blocked} className={cn(controlClass, 'px-2')}
          onChange={event => { if (!blocked) onPageSizeChange(Number(event.target.value)) }}>
          {Array.from(new Set([...PAGE_SIZE_OPTIONS, pageSize])).sort((a, b) => a - b).map(size => <option key={size} value={size}>{size} 条</option>)}
        </select>
      </label>}
    </div>
    <div className="flex flex-wrap items-center gap-2">
      <Button variant="icon" size="compact" aria-label="上一页" icon={<ChevronLeft />} disabled={blocked || page <= 1} onClick={() => onPageChange(Math.max(1, page - 1))} />
      <span className="min-w-16 text-center text-sm tabular-nums text-gray-600" aria-live="polite">{page} / {totalPages}</span>
      <Button variant="icon" size="compact" aria-label="下一页" icon={<ChevronRight />} disabled={blocked || page >= totalPages} onClick={() => onPageChange(Math.min(totalPages, page + 1))} />
      {totalPages > 2 && <div className="flex items-center gap-2">
        <label className="flex items-center gap-2 text-sm text-gray-600">跳至
          <input aria-label="跳转页码" type="number" min={1} max={totalPages} step={1} value={jumpInput} disabled={blocked}
            onChange={event => setJumpInput(event.target.value)} onKeyDown={event => { if (event.key === 'Enter') { event.preventDefault(); jump() } }}
            placeholder={String(page)} className={cn(controlClass, 'w-16 px-2 text-center')} />
        </label>
        <Button size="compact" disabled={blocked || !validJump} onClick={jump}>确定</Button>
      </div>}
    </div>
  </nav>
}

export default function Pagination(props: PaginationProps | LegacyPaginationProps) {
  return 'onPageChange' in props ? <PaginationControls {...props} /> : <LegacyPagination {...props} />
}
