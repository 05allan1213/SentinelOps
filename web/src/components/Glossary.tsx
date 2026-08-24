import { useState } from 'react'

const terms: Record<string, string> = {
  'CVE': 'Common Vulnerabilities and Exposures，通用漏洞披露标识符',
  'CVSS': 'Common Vulnerability Scoring System，通用漏洞评分系统(0-10分)',
  'IOC': 'Indicator of Compromise，入侵指标',
  'TTPs': 'Tactics, Techniques and Procedures，攻击战术、技术和程序',
  'APT': 'Advanced Persistent Threat，高级持续性威胁',
  'SIEM': 'Security Information and Event Management，安全信息和事件管理',
  'SOC': 'Security Operations Center，安全运营中心',
  'EDR': 'Endpoint Detection and Response，端点检测与响应',
  'XDR': 'Extended Detection and Response，扩展检测与响应',
  'MITRE ATT&CK': '网络攻击战术和技术知识库框架',
}

export default function Glossary({ text }: { text: string }) {
  const [tooltip, setTooltip] = useState<{ term: string; x: number; y: number } | null>(null)

  const parts = text.split(new RegExp(`(${Object.keys(terms).join('|')})`, 'g'))

  return (
    <span className="relative">
      {parts.map((part, i) =>
        terms[part] ? (
          <span
            key={i}
            className="border-b border-dashed border-primary-400 cursor-help text-primary-300"
            onMouseEnter={(e) => setTooltip({ term: part, x: e.clientX, y: e.clientY })}
            onMouseLeave={() => setTooltip(null)}
          >
            {part}
          </span>
        ) : (
          <span key={i}>{part}</span>
        )
      )}
      {tooltip && (
        <div
          className="fixed z-50 px-3 py-2 bg-gray-800 border border-gray-600 rounded shadow-lg text-xs max-w-xs"
          style={{ left: tooltip.x + 10, top: tooltip.y + 10 }}
        >
          <div className="font-medium text-white">{tooltip.term}</div>
          <div className="text-gray-400 mt-1">{terms[tooltip.term]}</div>
        </div>
      )}
    </span>
  )
}
