import { useState } from 'react'
import { createRoot } from 'react-dom/client'
import { RefreshCw } from 'lucide-react'
import PageHeader from '../../../src/components/common/PageHeader'
import Card from '../../../src/components/common/Card'
import Button from '../../../src/components/common/Button'
import Alert from '../../../src/components/common/Alert'
import StatePanel from '../../../src/components/common/StatePanel'
import Pagination from '../../../src/components/common/Pagination'
import PaginationBar from '../../../src/components/common/PaginationBar'
import { Dialog, DialogTrigger, DialogContent, DialogHeader, DialogBody, DialogFooter, DialogTitle, DialogDescription, DialogClose } from '../../../src/components/ui/dialog'
import '../../../src/assets/styles/index.css'

// Isolated interaction fixture; no product routes, business records or API responses.
export default function FoundationFixture() {
  const [page, setPage] = useState(1)
  const [size, setSize] = useState(20)
  const [loading, setLoading] = useState(false)
  return <main className="mx-auto max-w-[1440px] space-y-5 p-8">
    <PageHeader title="Foundation contract" subtitle="Primitive interaction fixture" icon={RefreshCw} actions={<Button variant="primary" onClick={() => setLoading(v => !v)}>切换加载</Button>} />
    <div className="flex flex-wrap gap-2">
      <Button variant="primary" loading={loading}>标准按钮</Button>
      <Button size="compact">紧凑按钮</Button>
      <Button variant="danger">危险操作</Button>
      <Button variant="ghost">次要操作</Button>
      <Button variant="icon" aria-label="刷新" icon={<RefreshCw />} />
      <Button disabled>不可用</Button>
    </div>
    <Card testId="foundation-card" header="Card header" footer="Card footer">Card body</Card>
    <Alert tone="warning">unknown / not_observed</Alert>
    <StatePanel kind="not-run" title="未执行" description="not_run" technicalDetail="reason_code=not_observed" />
    <PaginationBar data-testid="pagination-bar"><Pagination page={page} pageSize={size} total={size * 5} totalPages={5} onPageChange={setPage} onPageSizeChange={setSize} isFetching={loading} /></PaginationBar>
    <Dialog>
      <DialogTrigger asChild><Button>打开对话框</Button></DialogTrigger>
      <DialogContent>
        <DialogHeader><DialogTitle className="text-lg font-semibold">Foundation dialog</DialogTitle><DialogDescription>Dialog contract</DialogDescription></DialogHeader>
        <DialogBody data-testid="dialog-body">
          <Button>首个操作</Button>
          {Array.from({ length: 40 }, (_, i) => <p key={i}>Overflow contract {i}</p>)}
        </DialogBody>
        <DialogFooter><DialogClose asChild><Button>关闭</Button></DialogClose></DialogFooter>
      </DialogContent>
    </Dialog>
  </main>
}
createRoot(document.getElementById('root')!).render(<FoundationFixture />)
