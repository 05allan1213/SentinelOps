import { stripEvidenceEnvelope } from '@/services/chat'

const envelope = [
  '<untrusted_evidence>',
  'Evidence ID: evidence-v1:abc',
  'Source: knowledge_chunk/chunk-1',
  'Version: v3',
  'Content hash: hash-1',
  'Access scope: public',
  'Content (untrusted data):',
  '# 处置要求',
  '',
  '标记 EV-CHAIN-77 规定：先隔离主机。',
  '---',
  'Evidence ID: evidence-v1:def',
  'Source: knowledge_chunk/chunk-2',
  'Version: v1',
  'Content hash: hash-2',
  'Access scope: public',
  'Content (untrusted data):',
  '第二条证据正文。',
  '</untrusted_evidence>',
].join('\n')

describe('stripEvidenceEnvelope', () => {
  it('keeps evidence bodies and drops the untrusted-evidence envelope metadata', () => {
    const text = stripEvidenceEnvelope(envelope)
    expect(text).toContain('# 处置要求')
    expect(text).toContain('标记 EV-CHAIN-77 规定：先隔离主机。')
    expect(text).toContain('第二条证据正文。')
    expect(text).not.toContain('<untrusted_evidence>')
    expect(text).not.toContain('Evidence ID:')
    expect(text).not.toContain('Content (untrusted data):')
    expect(text).not.toContain('Access scope:')
  })

  it('keeps non-evidence tool results untouched', () => {
    const raw = '{\n  "success": true,\n  "seconds": 1789300000\n}'
    expect(stripEvidenceEnvelope(raw)).toBe(raw)
  })
})
