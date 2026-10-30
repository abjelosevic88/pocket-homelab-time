// Where the time went: a period, grouped by project, app, item or tag.
import { useEffect, useMemo, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import { addDays, fmtDate, fmtRange, todayISO, weekday } from '../lib/format'
import { useOnChange } from '../lib/timer'
import { appMeta, fmtDur, fmtHM, fmtHours, fmtMoney, weekStartOf } from '../lib/time'
import type { Report } from '../lib/types'
import { Icon } from '../components/icons'
import { COLORS } from '../components/pickers'
import { Empty, Loading, PageHeader, Segmented, useToast } from '../components/ui'

type RangeId = 'week' | 'lastweek' | 'month' | 'lastmonth' | 'year' | 'custom'
type GroupId = 'project' | 'app' | 'item' | 'tag'

function rangeFor(id: RangeId, ws: number, cf: string, ct: string): [string, string] {
  const t = todayISO()
  const w = weekStartOf(t, ws)
  const m0 = t.slice(0, 8) + '01'
  const prevMonthEnd = addDays(m0, -1)
  switch (id) {
    case 'week': return [w, addDays(w, 6)]
    case 'lastweek': return [addDays(w, -7), addDays(w, -1)]
    case 'month': return [m0, addDays(addDays(m0, 32).slice(0, 8) + '01', -1)]
    case 'lastmonth': return [prevMonthEnd.slice(0, 8) + '01', prevMonthEnd]
    case 'year': return [t.slice(0, 4) + '-01-01', t.slice(0, 4) + '-12-31']
  }
  return [cf || addDays(t, -29), ct || t]
}

export default function Reports() {
  const { settings } = useApp()
  const [sp, setSp] = useSearchParams()
  const range = (sp.get('range') || 'week') as RangeId
  const group = (sp.get('group') || 'project') as GroupId
  const [cf, setCf] = useState(sp.get('from') || addDays(todayISO(), -29))
  const [ct, setCt] = useState(sp.get('to') || todayISO())
  const [from, to] = rangeFor(range, settings?.week_start || 1, cf, ct)
  const [rep, setRep] = useState<Report | null>(null)
  const [tick, setTick] = useState(0)
  useOnChange(() => setTick(n => n + 1))
  const set = (k: string, v: string) => { const n = new URLSearchParams(sp); n.set(k, v); setSp(n, { replace: true }) }
  useEffect(() => {
    let live = true
    api.get<Report>(`${V1}/report?from=${from}&to=${to}&group=${group}`).then(r => { if (live) setRep(r) }).catch(() => {})
    return () => { live = false }
  }, [from, to, group, tick])

  const colorOf = useMemo(() => {
    const map: Record<string, string> = {}
    rep?.groups.forEach((g, i) => { map[g.key] = g.color || (group === 'app' ? appMeta(g.key).color : COLORS[i % COLORS.length]) })
    return (k: string) => map[k] || 'var(--faint)'
  }, [rep, group])

  const days = rep?.days.length || 0
  const avg = rep && days ? rep.seconds / Math.max(1, rep.days.filter(d => d.date <= todayISO()).length) : 0
  return (
    <div className="stack loose">
      <PageHeader title="Reports" sub={<span className="tnum">{fmtRange(from, to)}</span>}
        actions={<a className="btn" href={`${V1}/report.csv?from=${from}&to=${to}`}><Icon name="download" size={15} />CSV</a>} />
      <div className="toolbar row wrap">
        <div className="scroll-x"><Segmented size="sm" label="Period" value={range} onChange={v => set('range', v)} options={[
          { value: 'week', label: 'This week' }, { value: 'lastweek', label: 'Last week' }, { value: 'month', label: 'This month' },
          { value: 'lastmonth', label: 'Last month' }, { value: 'year', label: 'This year' }, { value: 'custom', label: 'Custom' }]} /></div>
        {range === 'custom' && <div className="row">
          <input type="date" className="date-in" value={cf} max={ct} onChange={e => { setCf(e.target.value); set('from', e.target.value) }} aria-label="From" />
          <span className="muted">–</span>
          <input type="date" className="date-in" value={ct} min={cf} onChange={e => { setCt(e.target.value); set('to', e.target.value) }} aria-label="To" />
        </div>}
      </div>
      {!rep ? <Loading /> : <>
        <div className="stats-row">
          <div className="stat"><div className="stat-l">Total</div><div className="stat-v tnum">{fmtDur(rep.seconds)}</div><div className="stat-s small">{rep.count} entries</div></div>
          <div className="stat"><div className="stat-l">Per day</div><div className="stat-v tnum">{fmtDur(avg)}</div><div className="stat-s small">on average</div></div>
          <div className="stat"><div className="stat-l">Billable</div><div className="stat-v tnum">{fmtDur(rep.billable_s)}</div><div className="stat-s small">{rep.seconds ? Math.round((rep.billable_s / rep.seconds) * 100) : 0}% of the time</div></div>
          {rep.amount > 0 && <div className="stat"><div className="stat-l">Amount</div><div className="stat-v tnum">{fmtMoney(rep.amount, rep.currency)}</div><div className="stat-s small">at project rates</div></div>}
        </div>
        {rep.seconds === 0 ? <Empty icon="chart" title="Nothing tracked in this period">Pick another period above.</Empty> : <>
          <section className="card">
            <div className="card-head"><div className="grow"><h2>Over time</h2>{rep.rounding > 0 && <p className="muted small">Each entry rounded up to {rep.rounding} min.</p>}</div></div>
            <div className="card-body"><StackedDays rep={rep} colorOf={colorOf} /></div>
          </section>
          <section className="card">
            <div className="card-head">
              <div className="grow"><h2>Breakdown</h2></div>
              <div className="scroll-x"><Segmented size="sm" label="Group by" value={group} onChange={v => set('group', v)} options={[{ value: 'project', label: 'Project' }, { value: 'app', label: 'App' }, { value: 'item', label: 'What' }, { value: 'tag', label: 'Tag' }]} /></div>
            </div>
            <div className="card-body flush">
              <ul className="list breakdown">
                {rep.groups.map(g => (
                  <li key={g.key} className="list-row bd-row">
                    <span className="bd-swatch" style={{ background: colorOf(g.key) }} />
                    <span className="grow col">
                      <span className="bd-head"><span className="ellipsis bd-label">{group === 'app' && <Icon name={appMeta(g.key).icon} size={13} />}{g.label}</span><b className="tnum">{fmtDur(g.seconds)}</b></span>
                      <span className="bd-track"><span style={{ width: `${(g.seconds / rep.seconds) * 100}%`, background: colorOf(g.key) }} /></span>
                      <span className="muted small tnum">{Math.round((g.seconds / rep.seconds) * 100)}%{g.billable_s > 0 ? ` · ${fmtHours(g.billable_s)} billable` : ''}{g.amount > 0 ? ` · ${fmtMoney(g.amount, rep.currency)}` : ''}</span>
                    </span>
                  </li>
                ))}
              </ul>
            </div>
          </section>
          <Summary from={from} to={to} />
        </>}
      </>}
    </div>
  )
}

/** Bars for each day (or week, or month for long periods), stacked by group. */
function StackedDays({ rep, colorOf }: { rep: Report; colorOf: (k: string) => string }) {
  const n = rep.days.length
  const bucket = n <= 35 ? 'day' : n <= 120 ? 'week' : 'month'
  const cols: { key: string; label: string; title: string; by: Record<string, number>; total: number }[] = []
  for (const d of rep.days) {
    const key = bucket === 'day' ? d.date : bucket === 'week' ? weekStartOf(d.date, 1) : d.date.slice(0, 7)
    let c = cols[cols.length - 1]
    if (!c || c.key !== key) {
      const label = bucket === 'day' ? (n <= 7 ? weekday(d.date).slice(0, 3) : d.date.slice(8)) : bucket === 'week' ? fmtDate(key, false) : new Date(key + '-15').toLocaleString(undefined, { month: 'short' })
      c = { key, label, title: bucket === 'day' ? `${weekday(d.date, true)}, ${fmtDate(d.date)}` : bucket === 'week' ? `Week of ${fmtDate(key)}` : label, by: {}, total: 0 }
      cols.push(c)
    }
    for (const [k, v] of Object.entries(d.by)) c.by[k] = (c.by[k] || 0) + v
    c.total += d.seconds
  }
  const max = Math.max(...cols.map(c => c.total), 1)
  const order = rep.groups.map(g => g.key)
  return (
    <div className={`sbars ${cols.length > 16 ? 'dense' : ''}`} role="img" aria-label="Time over the period">
      {cols.map(c => (
        <div key={c.key} className="sbars-col" title={`${c.title}: ${fmtDur(c.total)}`}>
          <span className="sbars-v tnum">{c.total && cols.length <= 16 ? fmtHM(c.total) : ''}</span>
          <span className="sbars-track"><span className="sbars-stack" style={{ height: `${(c.total / max) * 100}%` }}>
            {order.filter(k => c.by[k]).map(k => <i key={k} style={{ flexGrow: c.by[k], background: colorOf(k) }} />)}
          </span></span>
          <span className="sbars-l">{c.label}</span>
        </div>
      ))}
    </div>
  )
}

function Summary({ from, to }: { from: string; to: string }) {
  const { toast } = useToast()
  const [text, setText] = useState('')
  const [busy, setBusy] = useState(false)
  useEffect(() => setText(''), [from, to])
  const run = async () => {
    setBusy(true)
    try {
      const r = await api.post<{ ok: boolean; text?: string; error?: string }>(`${V1}/ai/summary`, { from, to })
      if (r.ok) setText(r.text || ''); else toast(r.error || 'The AI did not answer', 'err')
    } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  return (
    <section className="card">
      <div className="card-head"><div className="grow"><h2>A look back</h2><p className="muted small">A few sentences on where this period’s time went, written by AI from your entries.</p></div>
        <button type="button" className="btn sm" onClick={run} disabled={busy}><Icon name="sparkles" size={14} />{busy ? 'Writing…' : text ? 'Write again' : 'Write it'}</button></div>
      {text && <div className="card-body"><div className="recap">{text.split(/\n+/).map((p, i) => <p key={i}>{p}</p>)}</div></div>}
    </section>
  )
}
