import { useRef, useState, useCallback, useEffect } from 'react'
import { ArrowUp, Paperclip, Loader2, Lightbulb, Globe } from 'lucide-react'
import { cn } from '@/utils'
import DocUploadModal from '@/pages/knowledge/components/DocUploadModal'

interface ChatInputProps {
  value: string
  onChange: (value: string) => void
  onSend: () => void
  isLoading: boolean
  onFileUpload?: () => void
  deepThinking: boolean
  onDeepThinkingChange: (v: boolean) => void
  webSearch: boolean
  onWebSearchChange: (v: boolean) => void
}

export default function ChatInput({
  value,
  onChange,
  onSend,
  isLoading,
  onFileUpload,
  deepThinking,
  onDeepThinkingChange,
  webSearch,
  onWebSearchChange,
}: ChatInputProps) {
  const [isFocused, setIsFocused] = useState(false)
  const [uploadModalOpen, setUploadModalOpen] = useState(false)

  const textareaRef = useRef<HTMLTextAreaElement>(null)
  const isComposingRef = useRef(false)

  const adjustHeight = useCallback(() => {
    const el = textareaRef.current
    if (!el) return
    el.style.height = 'auto'
    el.style.height = `${Math.min(el.scrollHeight, 200)}px`
  }, [])

  useEffect(() => { adjustHeight() }, [value, adjustHeight])

  const hasContent = value.trim().length > 0

  const handleKeyDown = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === 'Enter' && !e.shiftKey && !isComposingRef.current) {
      e.preventDefault()
      if (hasContent && !isLoading) onSend()
    }
  }

  return (
    <div className="relative">
      <div
        className={cn(
          'relative rounded-2xl border bg-white transition-all duration-200',
          isFocused
            ? deepThinking
              ? 'border-[#A78BFA] shadow-[0_0_0_3px_rgba(167,139,250,0.25)]'
              : 'border-[#93C5FD] shadow-[0_0_0_3px_rgba(147,197,253,0.3)]'
            : deepThinking
              ? 'border-[#DDD6FE] shadow-sm hover:border-[#C4B5FD]'
              : 'border-[#E5E7EB] shadow-sm hover:border-[#D1D5DB]',
        )}
      >
        <textarea
          ref={textareaRef}
          value={value}
          onChange={(e) => { onChange(e.target.value); adjustHeight() }}
          onKeyDown={handleKeyDown}
          onCompositionStart={() => { isComposingRef.current = true }}
          onCompositionEnd={() => { isComposingRef.current = false }}
          onFocus={() => setIsFocused(true)}
          onBlur={() => setIsFocused(false)}
          placeholder={
            deepThinking
              ? '描述您的研判任务，深度思考模式将自动规划并协调多个 Agent…'
              : webSearch
              ? '输入问题，联网搜索将自动检索最新威胁情报与漏洞资讯…'
              : '输入消息…'
          }
          rows={1}
          disabled={isLoading}
          className="w-full resize-none bg-transparent px-4 pt-3.5 pb-14 text-base text-[#1F2937] placeholder-[#9CA3AF] focus:outline-none disabled:opacity-60"
          style={{ minHeight: 56 }}
        />

        {/* 底部工具栏 */}
        <div className="absolute bottom-0 left-0 right-0 flex items-center justify-between px-3 pb-2.5">

          {/* 左侧：深度思考 + 联网搜索 */}
          <div className="flex items-center gap-2">
            {/* 深度思考 pill */}
            <div className="relative group/deep">
              <button
                type="button"
                onClick={() => onDeepThinkingChange(!deepThinking)}
                className={cn(
                  'flex items-center gap-1.5 rounded-full px-3 py-1.5 text-xs font-medium transition-all duration-200 active:scale-95',
                  deepThinking
                    ? 'bg-gradient-to-r from-[#6366F1] to-[#8B5CF6] text-white shadow-[0_2px_10px_rgba(99,102,241,0.40)]'
                    : 'bg-[#F3F4F6] text-[#9CA3AF] hover:bg-[#E9EAEC] hover:text-[#6B7280]',
                )}
              >
                <Lightbulb className={cn('h-3.5 w-3.5', deepThinking ? 'text-yellow-200' : '')} />
                深度思考
              </button>
              <div className="pointer-events-none absolute bottom-full left-1/2 mb-2 -translate-x-1/2 whitespace-nowrap rounded-lg bg-[#1F2937] px-2.5 py-1.5 text-[11px] font-medium text-white opacity-0 shadow-md transition-opacity duration-150 group-hover/deep:opacity-100">
                深度推理模式，适合复杂安全研判
                <span className="absolute left-1/2 top-full -translate-x-1/2 border-4 border-transparent border-t-[#1F2937]" />
              </div>
            </div>

            {/* 联网搜索 pill */}
            <div className="relative group/web">
              <button
                type="button"
                onClick={() => onWebSearchChange(!webSearch)}
                className={cn(
                  'flex items-center gap-1.5 rounded-full px-3 py-1.5 text-xs font-medium transition-all duration-200 active:scale-95',
                  webSearch
                    ? 'bg-gradient-to-r from-[#6366F1] to-[#8B5CF6] text-white shadow-[0_2px_10px_rgba(99,102,241,0.40)]'
                    : 'bg-[#F3F4F6] text-[#9CA3AF] hover:bg-[#E9EAEC] hover:text-[#6B7280]',
                )}
              >
                <Globe className="h-3.5 w-3.5" />
                联网搜索
              </button>
              <div className="pointer-events-none absolute bottom-full left-1/2 mb-2 -translate-x-1/2 whitespace-nowrap rounded-lg bg-[#1F2937] px-2.5 py-1.5 text-[11px] font-medium text-white opacity-0 shadow-md transition-opacity duration-150 group-hover/web:opacity-100">
                按需搜索网页
                <span className="absolute left-1/2 top-full -translate-x-1/2 border-4 border-transparent border-t-[#1F2937]" />
              </div>
            </div>
          </div>

          {/* 右侧：上传 + 发送 */}
          <div className="flex items-center gap-1.5">
            {/* 上传附件 */}
            <div className="relative group/upload">
              <button
                type="button"
                onClick={() => setUploadModalOpen(true)}
                className="flex h-8 w-8 items-center justify-center rounded-full bg-[#F3F4F6] text-[#6B7280] transition-all duration-150 hover:bg-[#E5E7EB] hover:text-[#374151]"
              >
                <Paperclip className="h-4 w-4" />
              </button>
              <div className="pointer-events-none absolute bottom-full left-1/2 mb-2 -translate-x-1/2 whitespace-nowrap rounded-lg bg-[#1F2937] px-2.5 py-1.5 text-[11px] font-medium text-white opacity-0 shadow-md transition-opacity duration-150 group-hover/upload:opacity-100">
                上传文件到知识库
                <span className="absolute left-1/2 top-full -translate-x-1/2 border-4 border-transparent border-t-[#1F2937]" />
              </div>
            </div>

            {/* 发送 */}
            <div className="relative group/send">
              <button
                type="button"
                onClick={() => onSend()}
                disabled={!hasContent || isLoading}
                className={cn(
                  'flex h-9 w-9 items-center justify-center rounded-full transition-all duration-150',
                  isLoading
                    ? 'bg-[#6366F1]/70 text-white cursor-not-allowed'
                    : hasContent
                      ? 'bg-[#6366F1] text-white shadow-[0_2px_10px_rgba(99,102,241,0.45)] hover:bg-[#4F46E5] hover:shadow-[0_4px_14px_rgba(99,102,241,0.5)] active:scale-95'
                      : 'bg-[#F3F4F6] text-[#C4C9D4] cursor-default',
                )}
              >
                {isLoading ? (
                  <Loader2 className="h-4 w-4 animate-spin" />
                ) : (
                  <ArrowUp className="h-4 w-4" />
                )}
              </button>
              {!hasContent && !isLoading && (
                <div className="pointer-events-none absolute bottom-full left-1/2 mb-2 -translate-x-1/2 whitespace-nowrap rounded-lg bg-[#1F2937] px-2.5 py-1.5 text-[11px] font-medium text-white opacity-0 shadow-md transition-opacity duration-150 group-hover/send:opacity-100">
                  请输入您的问题
                  <span className="absolute left-1/2 top-full -translate-x-1/2 border-4 border-transparent border-t-[#1F2937]" />
                </div>
              )}
            </div>
          </div>
        </div>
      </div>

      <p className="mt-1.5 text-center text-[11px] text-[#9CA3AF]">
        Enter 发送 · Shift+Enter 换行
      </p>

      {uploadModalOpen && (
        <DocUploadModal
          baseID="default"
          baseName="默认知识库"
          onClose={() => setUploadModalOpen(false)}
          onSuccess={() => {
            setUploadModalOpen(false)
            onFileUpload?.()
          }}
        />
      )}
    </div>
  )
}
