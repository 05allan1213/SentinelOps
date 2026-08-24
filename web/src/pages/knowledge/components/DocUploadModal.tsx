import { useState, useRef } from 'react'
import { createPortal } from 'react-dom'
import { X, Upload, Loader2, CheckCircle2, XCircle, Plus } from 'lucide-react'
import { cn } from '@/utils'
import { knowledgeService } from '@/services/knowledge'
import toast from 'react-hot-toast'

const ACCEPT = '.pdf,.md,.docx,.go,.py,.java'
const ALLOWED_EXTS = ['pdf', 'md', 'docx', 'go', 'py', 'java']
const MAX_FILE_SIZE = 50 * 1024 * 1024  // 50 MB
const MAX_FILES = 3  // 最多上传3个文件

type UploadStatus = 'pending' | 'uploading' | 'success' | 'error'

interface FileEntry {
  file: File
  status: UploadStatus
  error?: string
}

interface Props {
  baseID: string
  baseName: string
  onClose: () => void
  onSuccess: () => void
}

function formatSize(b: number) {
  if (b < 1024) return `${b} B`
  if (b < 1024 * 1024) return `${(b / 1024).toFixed(1)} KB`
  return `${(b / 1024 / 1024).toFixed(1)} MB`
}

export default function DocUploadModal({ baseID, baseName, onClose, onSuccess }: Props) {
  const [files, setFiles] = useState<FileEntry[]>([])
  const [uploading, setUploading] = useState(false)
  const [dragging, setDragging] = useState(false)
  const inputRef = useRef<HTMLInputElement>(null)

  const addFiles = (incoming: File[]) => {
    if (files.length >= MAX_FILES) {
      toast.error(`最多只能上传 ${MAX_FILES} 个文件`)
      return
    }
    const newEntries: FileEntry[] = []
    for (const f of incoming) {
      if (files.length + newEntries.length >= MAX_FILES) {
        toast.error(`最多只能上传 ${MAX_FILES} 个文件`)
        break
      }
      const ext = f.name.split('.').pop()?.toLowerCase() ?? ''
      if (!ALLOWED_EXTS.includes(ext)) {
        toast.error(`${f.name}：不支持的格式`)
        continue
      }
      if (f.size > MAX_FILE_SIZE) {
        toast.error(`${f.name}：超过 50MB 大小限制`)
        continue
      }
      // 避免重复添加同名文件
      if (files.some(e => e.file.name === f.name && e.file.size === f.size)) continue
      newEntries.push({ file: f, status: 'pending' })
    }
    if (newEntries.length > 0) setFiles(prev => [...prev, ...newEntries])
  }

  const handleDrop = (e: React.DragEvent) => {
    e.preventDefault()
    setDragging(false)
    addFiles(Array.from(e.dataTransfer.files))
  }

  const handleInputChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    if (e.target.files) addFiles(Array.from(e.target.files))
    e.target.value = ''  // 允许重复选择同一文件
  }

  const removeFile = (index: number) => {
    setFiles(prev => prev.filter((_, i) => i !== index))
  }

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    const pending = files.filter(f => f.status === 'pending' || f.status === 'error')
    if (pending.length === 0) { toast.error('请选择文件'); return }

    setUploading(true)
    let successCount = 0

    for (let i = 0; i < files.length; i++) {
      const entry = files[i]
      if (entry.status !== 'pending' && entry.status !== 'error') continue

      // 标记为上传中
      setFiles(prev => prev.map((f, idx) => idx === i ? { ...f, status: 'uploading' } : f))

      try {
        await knowledgeService.uploadDoc(baseID, entry.file)
        setFiles(prev => prev.map((f, idx) => idx === i ? { ...f, status: 'success' } : f))
        successCount++
      } catch (err: unknown) {
        const msg = err instanceof Error ? err.message : '上传失败'
        setFiles(prev => prev.map((f, idx) => idx === i ? { ...f, status: 'error', error: msg } : f))
      }
    }

    setUploading(false)

    if (successCount > 0) {
      toast.success(`${successCount} 个文档已提交，正在异步索引…`)
      // 仍有失败的，留在弹窗供用户重试；全部成功则关闭
      const hasErrors = files.some(f => f.status === 'error')
      if (!hasErrors) {
        onSuccess()
      }
    }
  }

  const pendingCount = files.filter(f => f.status === 'pending' || f.status === 'error').length
  const allDone = files.length > 0 && files.every(f => f.status === 'success')

  return createPortal(
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50">
      <div className="bg-white rounded-xl shadow-xl w-full max-w-lg mx-4 flex flex-col max-h-[85vh]">
        <div className="flex items-center justify-between px-6 py-4 border-b border-gray-100 flex-shrink-0">
          <div>
            <h3 className="text-base font-semibold text-gray-900">上传文档</h3>
            <p className="text-xs text-gray-500 mt-0.5">上传到：{baseName}</p>
          </div>
          <button onClick={onClose} className="p-1 rounded-lg hover:bg-gray-100 transition-colors">
            <X className="w-4 h-4 text-gray-500" />
          </button>
        </div>

        <form onSubmit={handleSubmit} className="flex flex-col flex-1 overflow-hidden">
          <div className="flex-1 overflow-y-auto px-6 py-5 space-y-4">
            {/* 拖拽上传区 */}
            <div
              className={cn(
                'relative border-2 border-dashed rounded-xl p-5 text-center transition-colors cursor-pointer',
                dragging ? 'border-indigo-400 bg-indigo-50' : 'border-gray-200 hover:border-indigo-300',
              )}
              onDragOver={e => { e.preventDefault(); setDragging(true) }}
              onDragLeave={() => setDragging(false)}
              onDrop={handleDrop}
              onClick={() => inputRef.current?.click()}
            >
              <input
                ref={inputRef}
                type="file"
                accept={ACCEPT}
                multiple
                className="hidden"
                onChange={handleInputChange}
              />
              <div className="space-y-2">
                <Upload className="w-7 h-7 mx-auto text-gray-300" />
                <p className="text-sm text-gray-500">
                  拖拽文件到此处，或 <span className="text-indigo-600 font-medium">点击选择</span>
                </p>
                <p className="text-xs text-gray-400">支持 PDF、Docx、Markdown、Go、Python、Java，单文件最大 50MB，最多 3 个</p>
              </div>
            </div>

            {/* 文件列表 */}
            {files.length > 0 && (
              <div className="space-y-1.5">
                <div className="flex items-center justify-between">
                  <p className="text-xs font-medium text-gray-600">已选文件 ({files.length})</p>
                  {!uploading && (
                    <button
                      type="button"
                      onClick={() => inputRef.current?.click()}
                      className="text-xs text-indigo-600 hover:underline flex items-center gap-1"
                    >
                      <Plus className="w-3 h-3" />
                      继续添加
                    </button>
                  )}
                </div>
                <div className="max-h-36 overflow-y-auto space-y-1 rounded-lg border border-gray-100 p-1">
                  {files.map((entry, i) => (
                    <div
                      key={`${entry.file.name}-${i}`}
                      className={cn(
                        'flex items-center gap-2 px-2.5 py-1.5 rounded-lg text-xs',
                        entry.status === 'success' ? 'bg-emerald-50' :
                        entry.status === 'error'   ? 'bg-red-50' :
                        entry.status === 'uploading' ? 'bg-blue-50' : 'bg-gray-50',
                      )}
                    >
                      <div className="flex items-center gap-2 flex-1 min-w-0">
                        {entry.status === 'success' && <CheckCircle2 className="w-3.5 h-3.5 text-emerald-500 flex-shrink-0" />}
                        {entry.status === 'error'   && <XCircle      className="w-3.5 h-3.5 text-red-500 flex-shrink-0" />}
                        {entry.status === 'uploading' && <Loader2    className="w-3.5 h-3.5 text-blue-500 animate-spin flex-shrink-0" />}
                        {entry.status === 'pending'  && <div className="w-3.5 h-3.5 rounded-full border-2 border-gray-300 flex-shrink-0" />}

                        <span className="flex-1 truncate text-gray-700 min-w-0" title={entry.file.name}>{entry.file.name}</span>
                      </div>
                      <span className="text-gray-400 flex-shrink-0">{formatSize(entry.file.size)}</span>
                      {entry.status === 'error' && entry.error && (
                        <span className="text-red-500 truncate max-w-[100px] flex-shrink-0" title={entry.error}>{entry.error}</span>
                      )}
                      {!uploading && entry.status !== 'uploading' && (
                        <button
                          type="button"
                          onClick={() => removeFile(i)}
                          className="text-gray-400 hover:text-red-500 flex-shrink-0"
                        >
                          <X className="w-3 h-3" />
                        </button>
                      )}
                    </div>
                  ))}
                </div>
              </div>
            )}
          </div>

          <div className="flex justify-end gap-3 px-6 py-4 border-t border-gray-100 flex-shrink-0">
            <button
              type="button"
              onClick={onClose}
              className="px-4 py-2 text-sm font-medium text-gray-600 bg-gray-100 rounded-lg hover:bg-gray-200 transition-colors"
            >
              {allDone ? '关闭' : '取消'}
            </button>
            {!allDone && (
              <div className="relative group/submit">
                <button
                  type="submit"
                  disabled={uploading || pendingCount === 0}
                  className={cn(
                    'flex items-center gap-2 px-4 py-2 text-sm font-medium text-white rounded-lg transition-colors',
                    uploading || pendingCount === 0 ? 'bg-indigo-400 cursor-default' : 'bg-indigo-600 hover:bg-indigo-700',
                  )}
                >
                  {uploading && <Loader2 className="w-3.5 h-3.5 animate-spin" />}
                  {uploading ? `上传中…` : `上传${pendingCount > 1 ? `${pendingCount} 个文件` : '并索引'}`}
                </button>
                {!uploading && pendingCount === 0 && (
                  <div className="pointer-events-none absolute bottom-full left-1/2 mb-2 -translate-x-1/2 whitespace-nowrap rounded-lg bg-[#1F2937] px-2.5 py-1.5 text-[11px] font-medium text-white opacity-0 shadow-md transition-opacity duration-150 group-hover/submit:opacity-100">
                    请先选择要上传的文件
                    <span className="absolute left-1/2 top-full -translate-x-1/2 border-4 border-transparent border-t-[#1F2937]" />
                  </div>
                )}
              </div>
            )}
          </div>
        </form>
      </div>
    </div>
  , document.body)
}
