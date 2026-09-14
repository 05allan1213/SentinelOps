import Pagination from '@/components/common/Pagination'
import PaginationBar from '@/components/common/PaginationBar'
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
  return <PaginationBar><Pagination aria-label={`${label} 分页`} page={page} pageSize={meta.page_size}
    total={meta.total} totalPages={Math.max(1, Math.ceil(meta.total / meta.page_size))}
    isFetching={loading} onPageChange={onChange} /></PaginationBar>
}
