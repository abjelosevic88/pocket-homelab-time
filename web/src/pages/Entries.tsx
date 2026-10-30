// All your entries, a week at a time, with filters.
import { useCallback, useEffect, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import { useData } from '../lib/data'
import { addDays, fmtRange, todayISO } from '../lib/format'
import { useOnChange } from '../lib/timer'
import { APP_ORDER, APPS, fmtDur, weekStartOf } from '../lib/time'
import type { Entry } from '../lib/types'
import { DayGroups, EntryModal } from '../components/Entries'
import { Icon } from '../components/icons'
import { Select } from '../components/Select'
import { Empty, Loading, PageHeader, useDebounce } from '../components/ui'

export default function EntriesPage() {
  const { settings } = useApp()
  const { projects } = useData()
  const [sp, setSp] = useSearchParams()
  const ws = settings?.week_start || 1
  const from = sp.get('week') || weekStartOf(todayISO(), ws)
  const to = addDays(from, 6)
  const project = sp.get('project') || ''
  const app = sp.get('app') || ''
  const [q, setQ] = useState(sp.get('q') || '')
  const dq = useDebounce(q, 300)
  const [list, setList] = useState<Entry[] | null>(null)
  const [edit, setEdit] = useState<Entry | 'new' | null>(null)
  const set = (k: string, v: string) => { const n = new URLSearchParams(sp); if (v) n.set(k, v); else n.delete(k); setSp(n, { replace: true }) }
  const load = useCallback(() => {
    const p = new URLSearchParams({ from, to, limit: '2000' })
    if (project) p.set('project_id', project)
    if (app) p.set('app', app)
    if (dq) p.set('q', dq)
    api.get<{ entries: Entry[] }>(`${V1}/entries?${p}`).then(r => setList(r.entries)).catch(() => setList([]))
  }, [from, to, project, app, dq])
  useEffect(() => { load() }, [load])
  useOnChange(load)
  const total = (list || []).reduce((s, e) => s + e.duration_s, 0)
  const thisWeek = weekStartOf(todayISO(), ws)
  return (
    <div className="stack loose">
      <PageHeader title="Entries" sub={<span className="tnum">{fmtRange(from, to)} · {fmtDur(total)}</span>}
        actions={<button type="button" className="btn primary" onClick={() => setEdit('new')}><Icon name="plus" size={15} />Add time</button>} />
      <div className="toolbar row wrap">
        <div className="seg-nav">
          <button type="button" className="btn icon-only" aria-label="Previous week" onClick={() => set('week', addDays(from, -7))}><Icon name="chevron-left" size={16} /></button>
          <button type="button" className="btn" onClick={() => set('week', '')} disabled={from === thisWeek}>This week</button>
          <button type="button" className="btn icon-only" aria-label="Next week" onClick={() => set('week', addDays(from, 7))} disabled={from >= thisWeek}><Icon name="chevron-right" size={16} /></button>
        </div>
        <Select ariaLabel="Project" value={project} onChange={v => set('project', v)} placeholder="All projects" clearLabel="All projects"
          options={[{ value: 'none', label: 'No project' }, ...projects.map(p => ({ value: String(p.id), label: p.name }))]} />
        <Select ariaLabel="App" value={app} onChange={v => set('app', v)} placeholder="All apps" clearLabel="All apps"
          options={[{ value: 'time', label: 'Only Pocket Time' }, ...APP_ORDER.map(s => ({ value: s, label: APPS[s].name }))]} />
        <div className="search-box"><Icon name="search" size={15} /><input value={q} onChange={e => { setQ(e.target.value); set('q', e.target.value) }} placeholder="Search entries" aria-label="Search entries" /></div>
      </div>
      {list === null ? <Loading /> : list.length === 0 ? <Empty icon="clock" title="No time this week">Nothing matches here. Use the arrows to look at another week, or add time by hand.</Empty>
        : <DayGroups entries={list} onEdit={setEdit} today={todayISO()} />}
      {edit && <EntryModal entry={edit === 'new' ? undefined : edit} initialDate={edit === 'new' && from !== thisWeek ? from : undefined} onClose={() => setEdit(null)} />}
    </div>
  )
}
