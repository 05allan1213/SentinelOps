import { useState } from 'react'
import { createRoot } from 'react-dom/client'
import BaseCreateModal from '../../../src/pages/knowledge/components/BaseCreateModal'
import DocUploadModal from '../../../src/pages/knowledge/components/DocUploadModal'
import RebuildModal from '../../../src/pages/knowledge/components/RebuildModal'
import AddSubscriptionModal from '../../../src/pages/subscriptions/components/AddSubscriptionModal'
import GenerateReportModal from '../../../src/pages/reports/components/GenerateReportModal'
import '../../../src/assets/styles/index.css'

// Isolated interaction harness: no API fallback, fabricated business results, or product route.
export function Fixture() {
  const [open, setOpen] = useState('')
  const close = () => setOpen('')
  return <main>
    {['新建知识库', '上传文档', '重建索引', '添加订阅', 'AI 生成报告'].map(name => <button key={name} onClick={() => setOpen(name)}>{name}</button>)}
    {open === '新建知识库' && <BaseCreateModal onClose={close} onSuccess={close} />}
    {open === '上传文档' && <DocUploadModal baseID="" baseName="" onClose={close} onSuccess={close} />}
    {open === '重建索引' && <RebuildModal onClose={close} onSuccess={close} />}
    <AddSubscriptionModal isOpen={open === '添加订阅'} onClose={close} onSuccess={close} />
    <GenerateReportModal isOpen={open === 'AI 生成报告'} onClose={close} onSuccess={close} />
  </main>
}
createRoot(document.getElementById('root')!).render(<Fixture />)
