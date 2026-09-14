import { useState, useRef, useEffect, useId, type ReactNode, type KeyboardEvent } from 'react'
import { ChevronDown, Check } from 'lucide-react'
import { cn } from '@/utils'

export interface SelectOption {
  value: string | number
  label: string
}

export interface CustomSelectProps {
  value: string | number
  onChange: (value: string) => void
  options: SelectOption[]
  className?: string
  placeholder?: string
  prefix?: ReactNode
  label?: string
  error?: string
  disabled?: boolean
  size?: 'standard' | 'compact'
  'aria-label'?: string
}

export default function CustomSelect({ value, onChange, options, className, prefix, placeholder = '请选择', label, error, disabled = false, size = 'standard', 'aria-label': ariaLabel }: CustomSelectProps) {
  const [open, setOpen] = useState(false)
  const [active, setActive] = useState(-1)
  const [wasDisabled, setWasDisabled] = useState(disabled)
  const ref = useRef<HTMLDivElement>(null)
  const triggerRef = useRef<HTMLButtonElement>(null)
  const search = useRef({ text: '', time: 0 })
  const id = useId()
  const listId = `${id}-options`
  const selectedIndex = options.findIndex(option => String(option.value) === String(value))
  const selected = options[selectedIndex]
  const activeIndex = Math.min(active, options.length - 1)
  const expanded = open && !disabled

  // A disabled transition cancels the interaction; re-enabling must not reopen it.
  if (wasDisabled !== disabled) {
    setWasDisabled(disabled)
    if (disabled) setOpen(false)
  }

  useEffect(() => {
    if (!expanded) return
    const handler = (event: MouseEvent) => {
      if (!ref.current?.contains(event.target as Node)) setOpen(false)
    }
    document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [expanded])

  useEffect(() => {
    if (expanded && activeIndex >= 0) {
      document.getElementById(`${listId}-${activeIndex}`)?.scrollIntoView?.({ block: 'nearest' })
    }
  }, [expanded, activeIndex, listId])

  const showOptions = (last = false) => {
    if (disabled) return
    search.current = { text: '', time: 0 }
    setActive(selectedIndex >= 0 ? selectedIndex : options.length ? (last ? options.length - 1 : 0) : -1)
    setOpen(true)
  }
  const choose = (index: number) => {
    if (disabled || !options[index]) return
    onChange(String(options[index].value))
    setOpen(false)
    triggerRef.current?.focus()
  }
  const onKeyDown = (event: KeyboardEvent<HTMLButtonElement>) => {
    if (disabled) return
    if (event.key === 'Tab') { setOpen(false); return }
    if (event.key === 'Escape' && expanded) {
      event.preventDefault()
      event.stopPropagation()
      setOpen(false)
      return
    }
    if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
      event.preventDefault()
      if (!expanded) showOptions(event.key === 'ArrowUp')
      else setActive(index => Math.max(0, Math.min(options.length - 1, index + (event.key === 'ArrowDown' ? 1 : -1))))
      return
    }
    if (expanded && (event.key === 'Home' || event.key === 'End')) {
      event.preventDefault()
      setActive(event.key === 'Home' ? 0 : options.length - 1)
      return
    }
    if (event.key === 'Enter' || event.key === ' ') {
      event.preventDefault()
      if (expanded) choose(activeIndex)
      else showOptions()
      return
    }
    if (event.key.length === 1 && !event.ctrlKey && !event.altKey && !event.metaKey && !event.nativeEvent.isComposing) {
      event.preventDefault()
      const now = Date.now()
      const text = now - search.current.time < 700 ? search.current.text + event.key : event.key
      search.current = { text, time: now }
      const match = options.findIndex(option => option.label.toLocaleLowerCase().startsWith(text.toLocaleLowerCase()))
      if (match >= 0) { setActive(match); setOpen(true) }
    }
  }

  return <div ref={ref} className={cn('relative min-w-0 max-w-full select-none', className)}
    onBlur={event => { if (!event.currentTarget.contains(event.relatedTarget)) setOpen(false) }}>
    {label && <label htmlFor={id} className="mb-1.5 block text-sm font-medium text-gray-700">{label}</label>}
    <button ref={triggerRef} id={id} type="button" role="combobox" aria-label={ariaLabel ?? (label ? undefined : selected?.label ?? placeholder)}
      aria-expanded={expanded} aria-haspopup="listbox" aria-controls={expanded ? listId : undefined}
      aria-activedescendant={expanded && activeIndex >= 0 ? `${listId}-${activeIndex}` : undefined}
      aria-invalid={Boolean(error) || undefined} aria-describedby={error ? `${id}-error` : undefined}
      disabled={disabled} onKeyDown={onKeyDown} onClick={() => { if (expanded) setOpen(false); else showOptions() }}
      className={cn('control inline-flex w-full items-center gap-2 text-left', size === 'compact' && 'control-compact')}>
      {prefix && <span aria-hidden="true" className="flex h-4 w-4 shrink-0 items-center text-gray-500 [&>svg]:h-4 [&>svg]:w-4">{prefix}</span>}
      <span className={cn('min-w-0 flex-1 truncate', !selected && 'text-gray-500')} title={selected?.label ?? placeholder}>{selected?.label ?? placeholder}</span>
      <ChevronDown aria-hidden="true" className={cn('h-3.5 w-3.5 shrink-0 transition-transform duration-[180ms] ease-out motion-reduce:transition-none motion-reduce:duration-0', expanded && 'rotate-180')} />
    </button>
    {expanded && <div id={listId} role="listbox" aria-label={label ?? ariaLabel ?? placeholder}
      className="absolute z-50 mt-1 max-h-60 w-full min-w-0 overflow-y-auto overscroll-contain rounded-lg border border-gray-300 bg-white py-1 shadow-md">
      {options.length === 0 && <div role="option" aria-disabled="true" aria-selected="false" className="px-3 py-2 text-sm text-gray-500">无可选项</div>}
      {options.map((option, index) => <div key={option.value} id={`${listId}-${index}`} role="option" aria-selected={index === selectedIndex}
        onMouseDown={event => event.preventDefault()} onClick={() => choose(index)} onMouseMove={() => setActive(index)}
        className={cn('flex cursor-pointer items-center justify-between gap-2 px-3 py-2 text-sm leading-[22px] text-gray-700 [overflow-wrap:anywhere]',
          index === activeIndex && 'bg-gray-100', index === selectedIndex && 'font-medium text-primary-700')}>
        <span className="min-w-0">{option.label}</span>
        {index === selectedIndex && <Check aria-hidden="true" className="h-4 w-4 shrink-0 text-primary-600" />}
      </div>)}
    </div>}
    {error && <p id={`${id}-error`} className="mt-1.5 text-sm leading-[22px] text-danger-700 [overflow-wrap:anywhere]">{error}</p>}
  </div>
}
