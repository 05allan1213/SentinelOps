import { useState, useEffect } from 'react'
import { Plug, Send, CheckCircle2, XCircle, Copy, ChevronDown, ChevronUp, Eye, EyeOff, RefreshCw, Key, History } from 'lucide-react'
import { ingestService, cacheIngestKey, type IngestResult } from '@/services/ingest'
import { settingsService } from '@/services/settings'
import { eventService } from '@/services/event'
import { formatDate } from '@/utils'
import toast from 'react-hot-toast'
import { cn } from '@/utils'
import type { SecurityEvent } from '@/types'

type FormatTab = 'push' | 'webhook' | 'cef' | 'leef'

const formatTabs: { key: FormatTab; label: string; desc: string }[] = [
  { key: 'push',    label: 'API Push',  desc: '标准化 REST 推送，新系统对接推荐' },
  { key: 'webhook', label: 'Webhook',   desc: '通用 JSON，兼容 Splunk / Elastic' },
  { key: 'cef',     label: 'CEF',       desc: 'ArcSight Common Event Format' },
  { key: 'leef',    label: 'LEEF',      desc: 'IBM QRadar Log Event Extended Format' },
]

const examples: Record<FormatTab, string> = {
  push: JSON.stringify({
    title: 'SQL 注入攻击检测',
    content: '检测到来自 192.168.1.100 的 SQL 注入尝试，目标 /api/login',
    severity: 'high',
    source: 'WAF-Prod',
    cve_id: 'CVE-2024-1234',
    extra_fields: { src_ip: '192.168.1.100', dst_ip: '10.0.0.1' },
  }, null, 2),
  webhook: JSON.stringify({
    title: 'Brute Force Login Attempt',
    severity: 'high',
    source: 'Splunk-SIEM',
    description: '检测到暴力破解登录尝试，来源 IP: 203.0.113.5',
    src_ip: '203.0.113.5',
    timestamp: Math.floor(Date.now() / 1000),
  }, null, 2),
  cef: `CEF:0|Palo Alto Networks|PAN-OS|10.1|threat/vulnerability|SQL Injection Attempt|7|src=192.168.1.100 dst=10.0.0.5 msg=SQL injection detected in HTTP request`,
  leef: `LEEF:2.0|IBM|QRadar|7.4|SQL_INJECTION\tsrc=192.168.1.100\tdst=10.0.0.5\tsev=high\tmsg=SQL injection attempt detected`,
}

const severityConfig: Record<string, { label: string; cls: string }> = {
  critical: { label: '严重', cls: 'text-red-600 bg-red-50 border-red-200' },
  high:     { label: '高危', cls: 'text-orange-600 bg-orange-50 border-orange-200' },
  medium:   { label: '中危', cls: 'text-yellow-600 bg-yellow-50 border-yellow-200' },
  low:      { label: '低危', cls: 'text-blue-600 bg-blue-50 border-blue-200' },
  info:     { label: '信息', cls: 'text-gray-500 bg-gray-50 border-gray-200' },
}

const channelLabel: Record<string, string> = {
  webhook: 'Webhook', cef: 'CEF', leef: 'LEEF', api_push: 'API Push',
}

export default function IngestPage() {
  const [activeTab, setActiveTab] = useState<FormatTab>('push')
  const [payload, setPayload] = useState(examples.push)
  const [sourceName, setSourceName] = useState('')
  const [loading, setLoading] = useState(false)
  const [result, setResult] = useState<IngestResult | null>(null)
  const [error, setError] = useState('')
  const [showDocs, setShowDocs] = useState(false)

  // API Key 状态
  const [apiKey, setApiKey] = useState('')
  const [keyVisible, setKeyVisible] = useState(false)
  const [keyLoading, setKeyLoading] = useState(false)
  const [resetConfirm, setResetConfirm] = useState(false)

  // 接入历史状态
  const [history, setHistory] = useState<SecurityEvent[]>([])
  const [histLoading, setHistLoading] = useState(false)

  useEffect(() => {
    // 加载 API Key
    settingsService.getIngestKey().then(k => { setApiKey(k); cacheIngestKey(k) }).catch(() => {})
    // 加载接入历史
    loadHistory()
  }, [])

  const loadHistory = async () => {
    setHistLoading(true)
    try {
      const res = await eventService.list({ size: 10, order_by: 'created_at', order_dir: 'desc' })
      // 只保留外部接入渠道的事件
      const ingestTypes = new Set(['webhook', 'cef', 'leef', 'api_push'])
      setHistory(res.list.filter(e => e.event_type && ingestTypes.has(e.event_type)))
    } catch {
      // 静默失败
    } finally {
      setHistLoading(false)
    }
  }

  const handleResetKey = async () => {
    if (!resetConfirm) { setResetConfirm(true); return }
    setKeyLoading(true)
    try {
      const newKey = await settingsService.resetIngestKey()
      setApiKey(newKey)
      cacheIngestKey(newKey)
      setKeyVisible(true)
      toast.success('API Key 已重置')
    } catch {
      toast.error('重置失败')
    } finally {
      setKeyLoading(false)
      setResetConfirm(false)
    }
  }

  const copyKey = () => {
    navigator.clipboard.writeText(apiKey)
    toast.success('已复制')
  }

  const switchTab = (tab: FormatTab) => {
    setActiveTab(tab)
    setPayload(examples[tab])
    setResult(null)
    setError('')
  }

  const handleSubmit = async () => {
    setLoading(true)
    setResult(null)
    setError('')
    try {
      let res: IngestResult
      if (activeTab === 'push') {
        res = await ingestService.push(JSON.parse(payload))
      } else if (activeTab === 'webhook') {
        res = await ingestService.webhook(JSON.parse(payload), sourceName || undefined)
      } else if (activeTab === 'cef') {
        res = await ingestService.cef(payload, sourceName || undefined)
      } else {
        res = await ingestService.leef(payload, sourceName || undefined)
      }
      setResult(res)
      toast.success(res.is_new ? '告警已接入' : '重复告警，已去重')
    } catch (e: unknown) {
      const msg = e instanceof Error ? e.message : '接入失败'
      setError(msg)
      toast.error(msg)
    } finally {
      setLoading(false)
    }
  }

  const copyEndpoint = (path: string) => {
    navigator.clipboard.writeText(`POST /api/ingest/v1/${path}`)
    toast.success('已复制')
  }

  return (
    <div className="flex flex-col gap-5 pb-8">
      {/* 页头 */}
      <div className="flex items-center justify-between flex-shrink-0">
        <div className="flex items-center gap-3">
          <div className="p-2 rounded-lg bg-indigo-50">
            <Plug className="w-5 h-5 text-indigo-600" />
          </div>
          <div>
            <h1 className="text-2xl font-semibold text-gray-900 tracking-tight">多源告警接入</h1>
            <p className="text-sm text-gray-500 mt-0.5">支持 Webhook / CEF / LEEF / API Push，统一归一化后写入事件库</p>
          </div>
        </div>
      </div>

      {/* API Key 管理 */}
      <div className="bg-white rounded-xl border border-slate-200 shadow-sm p-4 flex-shrink-0">
        <div className="flex items-center gap-2 mb-3">
          <Key className="w-4 h-4 text-indigo-500" />
          <span className="text-sm font-semibold text-gray-800">接入 API Key</span>
          <span className="text-xs text-gray-400 ml-1">— 所有接入端点需在请求头携带 <code className="font-mono bg-slate-100 px-1 rounded">X-API-Key</code></span>
        </div>
        <div className="flex items-center gap-2">
          <div className="flex-1 flex items-center gap-2 rounded-lg border border-slate-200 bg-slate-50 px-3 py-2">
            <code className="flex-1 text-sm font-mono text-gray-700 truncate">
              {apiKey ? (keyVisible ? apiKey : apiKey.slice(0, 10) + '••••••••••••••••') : '加载中...'}
            </code>
            <button onClick={() => setKeyVisible(v => !v)} className="text-gray-400 hover:text-gray-600 transition-colors" title={keyVisible ? '隐藏' : '显示'}>
              {keyVisible ? <EyeOff className="w-4 h-4" /> : <Eye className="w-4 h-4" />}
            </button>
            <button onClick={copyKey} className="text-gray-400 hover:text-indigo-500 transition-colors" title="复制">
              <Copy className="w-4 h-4" />
            </button>
          </div>
          {resetConfirm ? (
            <div className="flex items-center gap-1.5">
              <span className="text-xs text-red-600">确认重置？</span>
              <button onClick={handleResetKey} disabled={keyLoading} className="px-3 py-1.5 rounded-lg bg-red-500 text-white text-xs font-medium hover:bg-red-600 transition-colors">
                {keyLoading ? '重置中...' : '确认'}
              </button>
              <button onClick={() => setResetConfirm(false)} className="px-3 py-1.5 rounded-lg border border-slate-200 text-xs text-gray-600 hover:bg-slate-50 transition-colors">
                取消
              </button>
            </div>
          ) : (
            <button onClick={handleResetKey} className="flex items-center gap-1.5 px-3 py-2 rounded-lg border border-slate-200 text-sm text-gray-600 hover:bg-slate-50 transition-colors">
              <RefreshCw className="w-3.5 h-3.5" />
              重置
            </button>
          )}
        </div>
      </div>

      {/* 格式选择卡片 */}
      <div className="grid grid-cols-2 sm:grid-cols-4 gap-3 flex-shrink-0">
        {formatTabs.map(t => (
          <button
            key={t.key}
            onClick={() => switchTab(t.key)}
            className={cn(
              'rounded-xl border p-3 text-left transition-all',
              activeTab === t.key
                ? 'border-indigo-400 bg-indigo-50 ring-1 ring-indigo-300'
                : 'border-slate-200 bg-white hover:border-indigo-200 hover:bg-indigo-50/50'
            )}
          >
            <div className="flex items-center justify-between mb-1">
              <span className={cn('text-sm font-semibold', activeTab === t.key ? 'text-indigo-700' : 'text-gray-800')}>
                {t.label}
              </span>
              <button
                onClick={e => { e.stopPropagation(); copyEndpoint(t.key) }}
                className="text-gray-400 hover:text-indigo-500 transition-colors"
                title="复制端点"
              >
                <Copy className="w-3.5 h-3.5" />
              </button>
            </div>
            <p className="text-xs text-gray-500 leading-tight">{t.desc}</p>
          </button>
        ))}
      </div>

      {/* 测试面板 */}
      <div className="bg-white rounded-xl border border-slate-200 shadow-sm overflow-hidden flex-shrink-0">
        <div className="flex items-center justify-between px-4 py-3 border-b border-slate-100 bg-slate-50">
          <span className="text-sm font-medium text-gray-700">
            测试接入 —{' '}
            <code className="text-indigo-600 bg-indigo-50 px-1.5 py-0.5 rounded text-xs font-mono">
              POST /api/ingest/v1/{activeTab}
            </code>
          </span>
          <button
            onClick={() => setPayload(examples[activeTab])}
            className="text-xs text-gray-400 hover:text-indigo-600 transition-colors"
          >
            重置示例
          </button>
        </div>

        <div className="p-4 space-y-3">
          {activeTab !== 'push' && (
            <div>
              <label className="text-xs font-medium text-gray-600 mb-1 block">
                来源系统名称 <span className="text-gray-400 font-normal">（可选，覆盖 payload 中的 source 字段）</span>
              </label>
              <input
                value={sourceName}
                onChange={e => setSourceName(e.target.value)}
                placeholder="如：Splunk-Prod"
                className="input w-64"
              />
            </div>
          )}

          <div>
            <label className="text-xs font-medium text-gray-600 mb-1 block">
              {activeTab === 'cef' || activeTab === 'leef' ? '原始告警行（纯文本）' : 'JSON Payload'}
            </label>
            <textarea
              value={payload}
              onChange={e => setPayload(e.target.value)}
              rows={activeTab === 'cef' || activeTab === 'leef' ? 3 : 10}
              className="w-full rounded-lg border border-slate-200 bg-slate-900 px-3 py-2.5 text-sm text-emerald-400 font-mono focus:outline-none focus:ring-2 focus:ring-indigo-300 focus:border-indigo-400 resize-none"
            />
          </div>

          <button
            onClick={handleSubmit}
            disabled={loading || !payload.trim()}
            className="btn-primary"
          >
            {loading ? (
              <span className="w-4 h-4 border-2 border-white/40 border-t-white rounded-full animate-spin" />
            ) : (
              <Send className="w-4 h-4" />
            )}
            {loading ? '接入中...' : '发送告警'}
          </button>
        </div>

        {(result || error) && (
          <div className={cn(
            'mx-4 mb-4 rounded-lg p-3 flex items-start gap-3 border',
            result ? 'bg-emerald-50 border-emerald-200' : 'bg-red-50 border-red-200'
          )}>
            {result
              ? <CheckCircle2 className="w-4 h-4 text-emerald-500 mt-0.5 shrink-0" />
              : <XCircle className="w-4 h-4 text-red-500 mt-0.5 shrink-0" />
            }
            <div className="text-sm space-y-1">
              {result ? (
                <>
                  <p className={result.is_new ? 'text-emerald-700 font-medium' : 'text-amber-700 font-medium'}>
                    {result.is_new ? '新告警已入库' : '重复告警，已去重'}
                  </p>
                  <p className="text-gray-500 font-mono text-xs">ID: {result.id}</p>
                  <p className="text-gray-400 font-mono text-xs">DedupKey: {result.dedup_key}</p>
                </>
              ) : (
                <p className="text-red-600">{error}</p>
              )}
            </div>
          </div>
        )}
      </div>

      {/* 接入历史 */}
      <div className="bg-white rounded-xl border border-slate-200 shadow-sm overflow-hidden flex-shrink-0">
        <div className="flex items-center justify-between px-4 py-3 border-b border-slate-100 bg-slate-50">
          <div className="flex items-center gap-2">
            <History className="w-4 h-4 text-indigo-500" />
            <span className="text-sm font-semibold text-gray-800">接入历史</span>
            <span className="text-xs text-gray-400">最近 10 条外部接入事件</span>
          </div>
          <button onClick={loadHistory} disabled={histLoading} className="text-gray-400 hover:text-indigo-500 transition-colors">
            <RefreshCw className={cn('w-4 h-4', histLoading && 'animate-spin')} />
          </button>
        </div>
        {histLoading ? (
          <div className="py-8 flex justify-center">
            <RefreshCw className="w-5 h-5 animate-spin text-gray-300" />
          </div>
        ) : history.length === 0 ? (
          <div className="py-8 text-center text-sm text-gray-400">暂无接入记录</div>
        ) : (
          <table className="w-full text-sm">
            <thead>
              <tr className="text-xs text-gray-500 border-b border-slate-100">
                <th className="text-left px-4 py-2 font-medium">标题</th>
                <th className="text-left px-4 py-2 font-medium">渠道</th>
                <th className="text-left px-4 py-2 font-medium">级别</th>
                <th className="text-left px-4 py-2 font-medium">时间</th>
              </tr>
            </thead>
            <tbody>
              {history.map(e => {
                const sev = severityConfig[e.severity] || severityConfig.info
                return (
                  <tr key={e.id} className="border-b border-slate-50 hover:bg-slate-50/60 transition-colors">
                    <td className="px-4 py-2.5 max-w-xs truncate text-gray-800" title={e.title}>{e.title}</td>
                    <td className="px-4 py-2.5">
                      <span className="inline-flex items-center h-5 px-1.5 rounded text-[10px] font-semibold bg-indigo-100 text-indigo-700">
                        {channelLabel[e.event_type || ''] || e.event_type}
                      </span>
                    </td>
                    <td className="px-4 py-2.5">
                      <span className={cn('inline-flex items-center h-5 px-1.5 rounded text-[10px] font-semibold border', sev.cls)}>
                        {sev.label}
                      </span>
                    </td>
                    <td className="px-4 py-2.5 text-gray-400 text-xs whitespace-nowrap">
                      {formatDate(e.created_at, 'YYYY-MM-DD HH:mm')}
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        )}
      </div>

      {/* 接入文档折叠面板 */}
      <div className="bg-white rounded-xl border border-slate-200 shadow-sm overflow-hidden">
        <button
          onClick={() => setShowDocs(v => !v)}
          className="w-full flex items-center justify-between px-4 py-3 text-sm text-gray-700 hover:bg-slate-50 transition-colors"
        >
          <span className="font-medium">接入文档</span>
          {showDocs ? <ChevronUp className="w-4 h-4 text-gray-400" /> : <ChevronDown className="w-4 h-4 text-gray-400" />}
        </button>
        {showDocs && (
          <div className="px-4 pb-4 space-y-5 border-t border-slate-100">
            <div className="pt-4">
              <DocSection
                title="API Push（推荐）"
                endpoint="POST /api/ingest/v1/push"
                fields={[
                  { name: 'title',        type: 'string',                        required: true,  desc: '告警标题' },
                  { name: 'content',      type: 'string',                        required: false, desc: '详细描述' },
                  { name: 'severity',     type: 'critical|high|medium|low|info', required: false, desc: '严重程度，默认 medium' },
                  { name: 'source',       type: 'string',                        required: true,  desc: '来源系统名称' },
                  { name: 'cve_id',       type: 'string',                        required: false, desc: 'CVE 编号' },
                  { name: 'extra_fields', type: 'object',                        required: false, desc: '扩展字段（src_ip/dst_ip/host 等）' },
                ]}
              />
            </div>
            <DocSection
              title="Webhook（Splunk / Elastic / 自定义）"
              endpoint="POST /api/ingest/v1/webhook?source_name=xxx"
              fields={[
                { name: 'title / name / alert_name', type: 'string',        required: true,  desc: '告警标题（按优先级取第一个非空字段）' },
                { name: 'severity / priority / level', type: 'string',      required: false, desc: '严重程度' },
                { name: 'description / details / body', type: 'string',     required: false, desc: '详细描述' },
                { name: 'source / vendor / product', type: 'string',        required: false, desc: '来源系统' },
                { name: 'timestamp / occurred_at',   type: 'number|string', required: false, desc: '告警时间（Unix 秒或 RFC3339）' },
              ]}
            />
            <div>
              <p className="text-sm font-semibold text-gray-800 mb-1">CEF（ArcSight）</p>
              <p className="text-xs text-gray-500 mb-2">POST /api/ingest/v1/cef?source_name=xxx — Content-Type: text/plain</p>
              <code className="block bg-slate-900 rounded-lg p-3 text-xs text-emerald-400 font-mono">
                CEF:0|Vendor|Product|Version|SignatureID|Name|Severity|Extension
              </code>
            </div>
            <div>
              <p className="text-sm font-semibold text-gray-800 mb-1">LEEF（IBM QRadar）</p>
              <p className="text-xs text-gray-500 mb-2">POST /api/ingest/v1/leef?source_name=xxx — Content-Type: text/plain</p>
              <code className="block bg-slate-900 rounded-lg p-3 text-xs text-emerald-400 font-mono">
                LEEF:2.0|Vendor|Product|Version|EventID{'\t'}key=value{'\t'}...
              </code>
            </div>
            <div className="rounded-lg bg-indigo-50 border border-indigo-200 p-3 text-xs text-indigo-700">
              所有端点统一返回：<code className="font-mono bg-indigo-100 px-1 rounded">{`{ "id": "...", "is_new": true, "dedup_key": "..." }`}</code>
              <br />
              重复告警（相同 title + source + content 前200字）自动去重，is_new=false。
            </div>
          </div>
        )}
      </div>
    </div>
  )
}

function DocSection({ title, endpoint, fields }: {
  title: string
  endpoint: string
  fields: { name: string; type: string; required: boolean; desc: string }[]
}) {
  return (
    <div>
      <p className="text-sm font-semibold text-gray-800 mb-1">{title}</p>
      <p className="text-xs text-gray-500 mb-2">{endpoint}</p>
      <table className="w-full text-xs border-collapse">
        <thead>
          <tr className="text-gray-500 border-b border-slate-200">
            <th className="text-left py-1.5 pr-3 font-medium">字段</th>
            <th className="text-left py-1.5 pr-3 font-medium">类型</th>
            <th className="text-left py-1.5 pr-3 font-medium">必填</th>
            <th className="text-left py-1.5 font-medium">说明</th>
          </tr>
        </thead>
        <tbody>
          {fields.map(f => (
            <tr key={f.name} className="border-b border-slate-100">
              <td className="py-1.5 pr-3 font-mono text-indigo-600">{f.name}</td>
              <td className="py-1.5 pr-3 text-gray-500">{f.type}</td>
              <td className="py-1.5 pr-3">{f.required ? <span className="text-red-500">是</span> : <span className="text-gray-400">否</span>}</td>
              <td className="py-1.5 text-gray-600">{f.desc}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}
