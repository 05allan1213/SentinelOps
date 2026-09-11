import type { PageMeta } from '@/types/runtime'

interface Props {
  page: number
  meta?: PageMeta
  loading: boolean
  onChange: (page: number) => void
  label: string
}

export default function RuntimeResourcePagination({ page, meta, loading, onChange, label }: Props) {
  if (!meta || (page === 1 && !meta.has_next)) return null
  return (
    <nav aria-label={`${label} 分页`} className="mt-3 flex items-center justify-end gap-3 text-sm text-gray-600">
      <span>第 {page} 页 · 共 {meta.total} 条</span>
      <button type="button" disabled={loading || page <= 1} onClick={() => onChange(page - 1)} className="rounded border px-3 py-1 disabled:opacity-40">上一页</button>
      <button type="button" disabled={loading || !meta.has_next} onClick={() => onChange(page + 1)} className="rounded border px-3 py-1 disabled:opacity-40">下一页</button>
    </nav>
  )
}
