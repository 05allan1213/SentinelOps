import RuntimeQueryError from './RuntimeQueryError'
import RuntimeResourcePagination from './RuntimeResourcePagination'
import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { FileSearch, Quote } from 'lucide-react'
import MarkdownRenderer from '@/components/markdown/MarkdownRenderer'
import { runtimeQueryKeys, useRuntimeEvidence } from '@/hooks/useRuntimeQueries'
import { runtimeService } from '@/services/runtime'
import { cn } from '@/utils'
import RuntimeQualityState from './RuntimeQualityState'
import type { EvidenceDTO } from '@/types/runtime'

interface Props {
  runId: string
}

const dash = (value: unknown) => (value === null || value === undefined || value === '' ? '—' : String(value))

function EvidenceRow({ evidence, runId }: { evidence: EvidenceDTO; runId: string }) {
  const [expanded, setExpanded] = useState(false)
  const expansion = useQuery({
    queryKey: [...runtimeQueryKeys.evidence(runId, { page: 1, page_size: 20 }), 'quote', evidence.evidence_id],
    queryFn: ({ signal }) => runtimeService.expandEvidence(runId, evidence.evidence_id, { include: 'quote', signal }),
    enabled: expanded,
  })
  const content = expansion.data?.item

  return (
    <li data-testid="runtime-evidence-row" data-evidence-id={evidence.evidence_id} className="rounded-lg border border-gray-200 bg-white p-3">
      <div className="flex flex-wrap items-center gap-2">
        <FileSearch className="w-3.5 h-3.5 text-gray-400" />
        <span className="font-mono text-xs text-gray-900">{evidence.evidence_id}</span>
        <span className="rounded border border-gray-200 bg-gray-50 px-1.5 py-0.5 text-xs text-gray-600">{dash(evidence.source_type)} / {dash(evidence.source_id)}</span>
        <span className="text-xs text-gray-500">scope: <span className="font-mono">{dash(evidence.access_scope)}</span></span>
        <RuntimeQualityState availability={evidence.availability} dataQuality={evidence.data_quality} reasonCode={evidence.reason_code} />
      </div>
      <dl className="mt-2 grid grid-cols-2 gap-2 text-xs lg:grid-cols-4">
        <div><dt className="text-gray-500">Source version</dt><dd className="font-mono text-gray-800">{dash(evidence.source_version)}</dd></div>
        <div><dt className="text-gray-500">Content hash</dt><dd className="font-mono text-gray-800 break-all">{dash(evidence.content_hash)}</dd></div>
        <div><dt className="text-gray-500">Indexed version</dt><dd className="font-mono text-gray-800">{dash(evidence.indexed_version)}</dd></div>
        <div><dt className="text-gray-500">Base / Document / Chunk</dt><dd className="font-mono text-gray-800 break-all">{dash(evidence.base_id)} / {dash(evidence.document_id)} / {dash(evidence.chunk_id)}</dd></div>
        <div><dt className="text-gray-500">Vector score</dt><dd className="tabular-nums text-gray-800">{dash(evidence.vector_score)}</dd></div>
        <div><dt className="text-gray-500">Rerank score</dt><dd className="tabular-nums text-gray-800">{dash(evidence.rerank_score)}</dd></div>
        <div><dt className="text-gray-500">Answer 关联</dt><dd className="font-mono text-gray-800 break-all">{evidence.answer_references?.join(', ') || '—'}</dd></div>
        <div><dt className="text-gray-500">引用可展开</dt><dd className="text-gray-800">{evidence.quote_available ? '是（需显式展开）' : '否'}</dd></div>
      </dl>
      {evidence.quote_available && (
        <div className="mt-2 border-t border-gray-100 pt-2">
          <button
            type="button"
            onClick={() => setExpanded(value => !value)}
            className="inline-flex items-center gap-1.5 text-xs text-indigo-700 hover:text-indigo-900"
          >
            <Quote className="w-3.5 h-3.5" />
            {expanded ? '收起引用' : '展开引用（资源级授权 + 脱敏）'}
          </button>
          {expanded && (
            <div data-testid="runtime-evidence-quote" className="mt-2 rounded-lg bg-gray-50 p-2 text-xs">
              {expansion.isLoading ? (
                <p className="text-gray-500">加载中…</p>
              ) : expansion.isError || !content ? (
                <p data-testid="runtime-evidence-quote-unavailable" className="text-gray-600">引用当前不可用或无权限，服务端未返回内容。</p>
              ) : (
                <>
                  <MarkdownRenderer content={content.quote} variant="runtime" complete />
                  <p className="mt-1 text-[11px] text-gray-500">
                    hash {content.content_hash} · redaction {content.redaction_applied ? 'applied' : 'not applied'}
                  </p>
                </>
              )}
            </div>
          )}
        </div>
      )}
    </li>
  )
}

export default function EvidenceInspector({ runId }: Props) {
  const [page, setPage] = useState(1)
  const query = useRuntimeEvidence(runId, { page, page_size: 20 })
  const data = query.data
  const items = data?.items ?? []

  return (
    <section data-testid="runtime-evidence-panel" className={cn('rounded-xl border border-gray-200 bg-white p-4', query.isFetching && 'opacity-90')}>
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h3 className="text-sm font-medium text-gray-900">Evidence Inspector（默认仅元数据）</h3>
        <RuntimeQualityState availability={data?.availability} dataQuality={data?.data_quality} reasonCode={data?.reason_code} notRun={data?.not_run} />
      </div>
      <RuntimeQueryError query={query} />
      {query.isError && !data ? null : query.isLoading && !data ? (
        <p data-testid="runtime-evidence-loading" className="mt-3 text-sm text-gray-500">加载中…</p>
      ) : items.length === 0 ? (
        <p data-testid="runtime-evidence-empty" className="mt-3 text-sm text-gray-500">
          {data?.availability === 'unavailable' ? 'Evidence 查询当前不可用' : '暂无 Evidence 记录'}
        </p>
      ) : (
        <ul className="mt-3 flex flex-col gap-2">
          {items.map(evidence => <EvidenceRow key={evidence.evidence_id} evidence={evidence} runId={runId} />)}
        </ul>
      )}
      <RuntimeResourcePagination label="Evidence" page={page} meta={data?.page} loading={query.isFetching} onChange={setPage} />
    </section>
  )
}
