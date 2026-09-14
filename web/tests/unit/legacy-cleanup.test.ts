import { existsSync, readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'

const source = (path: string) => readFileSync(fileURLToPath(new URL(path, import.meta.url)), 'utf8')
const stylesheet = source('../../src/assets/styles/index.css')

// Batch 9 removed every legacy definition whose replacement already owned the
// behaviour and whose caller census was empty. This guard keeps the removal
// honest: the entries below may only come back together with a real caller.
const removedLegacySelectors = [
  '.modal-overlay',
  '.modal {',
  '.modal-header',
  '.modal-title',
  '.modal-body',
  '.modal-footer',
  '.glass-panel',
  '.glass-card',
  '.premium-glow',
  '.stat-card',
  '.stat-value',
  '.stat-label',
  '.stat-change',
  '.card-header',
  '.btn-lg',
  '.btn-sm',
  '.btn-text',
  '.btn-link',
  '.btn-danger',
  '.input-lg',
  '.input-error',
  '.select {',
  '.textarea {',
  '.tag-primary',
  '.status-active',
  '.status-paused',
  '.status-disabled',
  '.descriptions {',
  '.descriptions-item',
  '.descriptions-content',
  '.breadcrumb',
  '.tabs {',
  '.tab {',
  '.alert-success',
  '.dropdown',
  '.tooltip {',
  '.progress {',
  '.progress-bar',
  '.switch-handle',
  '.switch.active',
  '.loading {',
  '.chat-container',
  '.chat-messages',
  '.welcome-greeting',
  '.message-content',
  '.message-avatar',
  '.message-input',
  '.chat-markdown',
  '.think-markdown',
  '.mode-selector',
  '.send-btn-circle',
  '.input-wrapper',
  '.view-panel',
  '.text-balance',
  '.scrollbar-none',
  '.cyber-skeleton',
  '.cyber-glow-card',
  '--cyber-',
]

const removedKeyframes = [
  'cyber-breathe',
  'cyber-ring',
  'cyber-cursor',
  'cyber-shimmer',
  'cyber-scanline',
  'cyber-dash',
  'cyber-skeleton',
  'cyber-rotate',
  'cyber-drawer-up',
  'cyber-drawer-down',
  'cyber-pop-in',
  'cyber-glow-pulse',
]

const removedComponents = [
  '../../src/components/AgentPipelineModal.tsx',
  '../../src/components/Glossary.tsx',
  '../../src/components/common/RiskRadar.tsx',
  '../../src/components/common/DataBlock.tsx',
  '../../src/pages/dashboard/components/SecurityFunnel.tsx',
  '../../src/pages/knowledge/components/DocList.tsx',
  '../../src/pages/cost-monitor/index.tsx',
  '../../src/pages/event-analysis/constants/colors.ts',
  '../../src/pages/event-analysis/components/ActionSandbox.tsx',
  '../../src/pages/event-analysis/components/AgentReasoningCard.tsx',
  '../../src/pages/event-analysis/components/AgentStatusBar.tsx',
  '../../src/pages/event-analysis/components/AgentTraceGraph.tsx',
  '../../src/pages/event-analysis/components/AnalysisCenter.tsx',
  '../../src/pages/event-analysis/components/EvidenceLab.tsx',
  '../../src/pages/event-analysis/components/GlobalInsightHeader.tsx',
  '../../src/pages/event-analysis/components/IntelligenceSidebar.tsx',
  '../../src/pages/event-analysis/components/InvestigationMap.tsx',
  '../../src/pages/event-analysis/components/ReasoningCanvas.tsx',
  '../../src/pages/event-analysis/components/ReasoningStream.tsx',
  '../../src/pages/event-analysis/components/RiskScoreGauge.tsx',
  '../../src/pages/event-analysis/components/SummaryPanel.tsx',
  '../../src/pages/event-analysis/components/TimelineNav.tsx',
]

it('has no removed legacy selectors or keyframes left in the stylesheet', () => {
  const stale = [
    ...removedLegacySelectors.filter((selector) => stylesheet.includes(selector)),
    ...removedKeyframes.filter((name) => stylesheet.includes(`@keyframes ${name}`)),
  ]
  expect(stale).toEqual([])
})

it('keeps the two cyber animations that still have live callers', () => {
  expect(stylesheet).toContain('@keyframes cyber-scan')
  expect(stylesheet).toContain('@keyframes cyber-slide-in')
  expect(source('../../src/pages/event-analysis/components/StartButton.tsx')).toContain('cyber-scan')
  expect(source('../../src/pages/event-analysis/components/ResultPanel.tsx')).toContain('cyber-slide-in')
})

it('keeps the chat classes that still have live callers', () => {
  for (const kept of ['.chat-welcome-anim', '@keyframes chat-fade-in', '@keyframes chat-dot-pulse', '.chat-sidebar-scroll', '.pagination-item', '.descriptions-label', '.empty-text', '.table-container']) {
    expect(stylesheet).toContain(kept)
  }
})

it('removed every component whose caller census was empty', () => {
  const present = removedComponents.filter((path) => existsSync(fileURLToPath(new URL(path, import.meta.url))))
  expect(present).toEqual([])
})
