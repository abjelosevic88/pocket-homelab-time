// The timer: what you are doing, for which project and app item, start/stop.
// The same timer the other Pocket apps start and stop.
import { useEffect, useState } from 'react'
import { useApp } from '../lib/app-context'
import { useTimer, useTick } from '../lib/timer'
import { appMeta, elapsed, entryName, fmtHMS, localInput, openURL, originText } from '../lib/time'
import type { Entry, ForTarget } from '../lib/types'
import { Icon } from './icons'
import { ForChip, ForPicker, ProjectSelect } from './pickers'
import { Confirm, Modal, Field } from './ui'
import { api, V1 } from '../lib/api'

const empty = { title: '', project_id: null as number | null, billable: false, target: null as ForTarget | null, touchedBill: false }

export function TimerBar() {
  const { settings } = useApp()
  const { running, start, stop, discard, update, busy } = useTimer()
  const [f, setF] = useState(empty)
  const [pick, setPick] = useState(false)
  const [suggest, setSuggest] = useState<Entry[]>([])
  const [focus, setFocus] = useState(false)
  const [editStart, setEditStart] = useState(false)
  const [confirmDiscard, setConfirmDiscard] = useState(false)
  useTick(!!running)
  useEffect(() => { if (!running) setF(empty) }, [running?.id]) // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => { api.get<Entry[]>(`${V1}/suggestions`).then(setSuggest).catch(() => {}) }, [running?.id])

  const go = async () => {
    if (running) { await stop(); return }
    const t = f.target
    await start({ title: f.title.trim(), project_id: f.project_id, ...(f.touchedBill ? { billable: f.billable } : {}), ...(t ? { app: t.app, app_ref: t.app_ref, app_label: t.app_label, app_url: t.app_url } : {}) })
  }
  const fromSuggestion = (e: Entry) => {
    setFocus(false)
    start({ title: e.title, project_id: e.project_id, billable: e.billable, ...(e.app ? { app: e.app, app_ref: e.app_ref, app_label: e.app_label, app_url: e.app_url } : {}) })
  }
  const q = f.title.trim().toLowerCase()
  const shown = focus && !running ? suggest.filter(s => !q || s.title.toLowerCase().includes(q)).slice(0, 6) : []

  if (running) {
    const m = appMeta(running.app)
    const href = openURL(running, settings)
    const origin = originText(running)
    return (
      <div className="timerbar running" aria-live="off">
        <div className="tb-main">
          <input className="tb-title" value={f.title !== '' ? f.title : running.title} aria-label="What are you working on?" placeholder="What are you working on?"
            onChange={e => setF({ ...f, title: e.target.value })} onBlur={() => { if (f.title !== '' && f.title !== running.title) update({ title: f.title.trim() }) }}
            onKeyDown={e => { if (e.key === 'Enter') (e.target as HTMLInputElement).blur() }} />
          <div className="tb-meta">
            <ProjectSelect className="tb-project" value={running.project_id} onChange={id => update({ project_id: id })} />
            {running.app && <ForChip target={running} onClick={() => href && window.open(href, '_blank', 'noopener')} />}
            {origin && <span className="tb-origin muted small"><Icon name={m.icon} size={12} />{origin}</span>}
          </div>
        </div>
        <button type="button" className="tb-clock tnum" onClick={() => setEditStart(true)} title="Change when it started">{fmtHMS(elapsed(running))}</button>
        <button type="button" className="tb-btn stop" onClick={go} disabled={busy} aria-label="Stop the timer" title="Stop (s)"><Icon name="stop" size={20} strokeWidth={2.4} /></button>
        <button type="button" className="btn ghost icon-only tb-discard" onClick={() => setConfirmDiscard(true)} aria-label="Discard" title="Throw this timer away"><Icon name="trash" size={16} /></button>
        {editStart && <StartEdit entry={running} onClose={() => setEditStart(false)} onSave={iso => { update({ started_at: iso }); setEditStart(false) }} />}
        {confirmDiscard && <Confirm title="Throw this timer away?" message={`“${entryName(running)}” is not saved anywhere.`} confirmLabel="Discard" onCancel={() => setConfirmDiscard(false)} onConfirm={() => { setConfirmDiscard(false); discard() }} />}
      </div>
    )
  }
  return (
    <div className="timerbar">
      <div className="tb-main">
        <div className="tb-input-wrap">
          <input className="tb-title" value={f.title} onChange={e => setF({ ...f, title: e.target.value })} placeholder="What are you working on?" aria-label="What are you working on?"
            onFocus={() => setFocus(true)} onBlur={() => setTimeout(() => setFocus(false), 150)} onKeyDown={e => { if (e.key === 'Enter') { e.preventDefault(); go() } }} />
          {shown.length > 0 && (
            <ul className="tb-suggest" role="listbox" aria-label="Recent">
              {shown.map(s => (
                <li key={s.id}><button type="button" onMouseDown={e => e.preventDefault()} onClick={() => fromSuggestion(s)}>
                  <Icon name="play" size={12} /><span className="grow ellipsis">{entryName(s)}</span>
                  {s.project_name && <span className="muted small ellipsis">{s.project_name}</span>}
                  {s.app && <Icon name={appMeta(s.app).icon} size={13} className="muted" />}
                </button></li>
              ))}
            </ul>
          )}
        </div>
        <div className="tb-meta">
          <ProjectSelect className="tb-project" value={f.project_id} onChange={id => setF({ ...f, project_id: id })} />
          {f.target ? <ForChip target={f.target} onClick={() => setPick(true)} onClear={() => setF({ ...f, target: null })} />
            : <button type="button" className="btn sm ghost tb-for" onClick={() => setPick(true)}><Icon name="layers" size={14} />For an app…</button>}
          <button type="button" className={`btn sm ghost tb-bill ${f.billable ? 'on' : ''}`} aria-pressed={f.billable} onClick={() => setF({ ...f, billable: !f.billable, touchedBill: true })} title="Billable"><Icon name="receipt" size={14} />Billable</button>
        </div>
      </div>
      <span className="tb-clock tnum muted">0:00</span>
      <button type="button" className="tb-btn" onClick={go} disabled={busy} aria-label="Start the timer" title="Start (s)"><Icon name="play" size={20} strokeWidth={2.4} /></button>
      <span className="tb-discard" aria-hidden="true" />
      {pick && <ForPicker initialApp={f.target?.app} onClose={() => setPick(false)} onPick={t => setF(x => ({ ...x, target: t, title: x.title || '' }))} />}
    </div>
  )
}

function StartEdit({ entry, onClose, onSave }: { entry: Entry; onClose: () => void; onSave: (iso: string) => void }) {
  const [v, setV] = useState(localInput(entry.started_at))
  return (
    <Modal title="When did it start?" onClose={onClose} size="sm" footer={<><button type="button" className="btn" onClick={onClose}>Cancel</button><button type="button" className="btn primary" onClick={() => onSave(new Date(v).toISOString())}>Save</button></>}>
      <Field label="Started"><input type="datetime-local" value={v} max={localInput(new Date())} onChange={e => setV(e.target.value)} /></Field>
    </Modal>
  )
}
