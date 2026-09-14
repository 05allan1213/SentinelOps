import { useState } from 'react'
import { createRoot } from 'react-dom/client'
import { Filter } from 'lucide-react'
import CustomSelect from '../../../src/components/common/CustomSelect'
import '../../../src/assets/styles/index.css'

// Test-only form values and option labels; no API, domain records or route registration.
export default function ControlsFixture() {
  const [mode, setMode] = useState('')
  const [disabled, setDisabled] = useState(false)
  const [time, setTime] = useState('')
  const [checked, setChecked] = useState(false)
  const options = [{ value: '', label: 'All' }, { value: 1, label: 'Alpha' }, { value: 'b', label: 'Beta' }]
  return <main className="mx-auto max-w-[1440px] space-y-5 p-8">
    <h1 className="text-2xl font-semibold leading-8">Shared control contract</h1>
    <form onSubmit={event => event.preventDefault()} className="grid grid-cols-[repeat(auto-fit,minmax(min(100%,260px),1fr))] items-start gap-5">
      <label className="grid min-w-0 gap-1.5 text-sm">Standard input<input className="control w-full" placeholder="Search" /></label>
      <label className="grid min-w-0 gap-1.5 text-sm">Compact input<input className="control control-compact w-full" placeholder="Search compact" /></label>
      <div className="grid min-w-0 gap-1.5 text-sm"><label htmlFor="native-select">Native select</label><select id="native-select" className="control w-full">{options.map(o => <option key={o.value} value={o.value}>{o.label}</option>)}</select></div>
      <div className="grid min-w-0 gap-1.5 text-sm"><label htmlFor="compact-native-select">Compact native select</label><select id="compact-native-select" className="control control-compact w-full">{options.map(o => <option key={o.value} value={o.value}>{o.label}</option>)}</select></div>
      <CustomSelect label="Custom select" value={mode} onChange={setMode} options={options} prefix={<Filter />} disabled={disabled} />
      <CustomSelect label="Compact custom select" size="compact" value={mode} onChange={setMode} options={options} />
      <CustomSelect label="Missing selection" placeholder="Choose an option" value="missing" onChange={setMode} options={options} error="Selection required" />
      <CustomSelect label="Long label" value="long" onChange={() => {}} options={[{ value: 'long', label: 'LongOptionLabel'.repeat(12) }]} />
      <label className="grid min-w-0 gap-1.5 text-sm">Local date time<input type="datetime-local" className="control" value={time} onChange={event => setTime(event.target.value)} /></label>
      <label className="grid min-w-0 gap-1.5 text-sm">Compact local date time<input type="datetime-local" className="control control-compact" /></label>
      <label className="grid min-w-0 gap-1.5 text-sm">Disabled input<input className="control" disabled placeholder="Unavailable" /></label>
      <div className="grid min-w-0 gap-1.5 text-sm"><label htmlFor="disabled-select">Disabled select</label><select id="disabled-select" className="control" disabled><option>Unavailable</option></select></div>
      <label className="grid min-w-0 gap-1.5 text-sm">Disabled date time<input className="control" type="datetime-local" disabled /></label>
      <label className="grid min-w-0 gap-1.5 text-sm">Invalid input<input className="control" aria-invalid="true" aria-describedby="input-error" /><span id="input-error" className="text-danger-700">Input required</span></label>
      <label className="flex min-h-9 items-center gap-2 text-sm"><input type="checkbox" className="control-checkbox" checked={checked} onChange={event => setChecked(event.target.checked)} />Checkbox</label>
      <label className="flex min-h-8 items-center gap-2 text-sm"><input type="checkbox" className="control-checkbox" disabled />Disabled checkbox</label>
    </form>
    <button type="button" className="control" onClick={() => setDisabled(v => !v)}>Toggle disabled</button>
    <output className="block" data-testid="control-values">{JSON.stringify({ mode, time, checked })}</output>
  </main>
}
createRoot(document.getElementById('root')!).render(<ControlsFixture />)
