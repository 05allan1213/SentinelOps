interface Props {
  query: { isError: boolean; data?: unknown; error: Error | null; refetch: () => unknown }
}

export default function RuntimeQueryError({ query }: Props) {
  if (!query.isError) return null
  return (
    <div role="alert" className="my-3 rounded-lg border border-red-200 bg-red-50 p-3 text-sm text-red-700">
      <p>查询失败{query.error?.message ? `：${query.error.message}` : ''}{query.data ? '；下方保留上次加载的数据。' : '；尚无法确认是否有记录。'}</p>
      <button type="button" onClick={() => void query.refetch()} className="mt-2 rounded border border-red-300 px-3 py-1">重试</button>
    </div>
  )
}
