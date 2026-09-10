export type RuntimeDetailTab = 'overview' | 'timeline' | 'attempts' | 'effects' | 'evidence' | 'context' | 'trace'

export const RUNTIME_DETAIL_TABS: { id: RuntimeDetailTab; label: string }[] = [
  { id: 'overview', label: '概览' },
  { id: 'timeline', label: '时间线' },
  { id: 'attempts', label: 'Attempts' },
  { id: 'effects', label: 'Effects' },
  { id: 'evidence', label: 'Evidence' },
  { id: 'context', label: 'Context' },
  { id: 'trace', label: 'Trace' },
]

export const isRuntimeDetailTab = (value: string | null): value is RuntimeDetailTab =>
  !!value && RUNTIME_DETAIL_TABS.some(tab => tab.id === value)
