// Entry rows grouped by day, and the add/edit dialog.
import React, { useState } from 'react'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import { fmtDate, fmtISOTime, weekday } from '../lib/format'
import { announceChange, useTimer, useTick } from '../lib/timer'
import { appMeta, elapsed, entryName, fmtDur, fmtHMS, localInput, openURL, originText } from '../lib/time'
import type { Entry, ForTarget } from '../lib/types'
import { Icon } from './icons'
import { Dot, ForChip, ForPicker, ProjectSelect } from './pickers'
import { Confirm, Field, Modal, Switch, useToast } from './ui'

export function DayGroups({ entries, onEdit, today }: { entries: Entry[]; onEdit: (e: Entry) => void; today?: string }) {
  const days: { date: string; list: Entry[]; total: number }[] = []
  for (const e of entries) {
    let d = days.find(x => x.date === e.date)
    if (!d) { d = { date: e.date, list: [], total: 0 }; days.push(d) }
    d.list.push(e)
    d.total += e.running ? 0 : e.duration_s
  }
  const label = (d: string) => d === today ? 'Today' : `${weekday(d, true)}, ${fmtDate(d, false)}`
  return (
    <div className="stack">
      {days.map(d => (
        <section key={d.date} className="card day-card">
          <header className="day-card-h"><span>{label(d.date)}</span><span className="tnum muted">{fmtDur(d.total + d.list.filter(e => e.running).reduce((s, e) => s + elapsed(e), 0))}</span></header>
          <ul className="list">
            {d.list.map(e => <li key={e.id}><EntryRow entry={e} onEdit={onEdit} /></li>)}
          </ul>
        </section>
      ))}
    </div>
  )
}

export function EntryRow({ entry: e, onEdit }: { entry: Entry; onEdit: (e: Entry) => void }) {
  const { settings } = useApp()
  const { continueEntry, running } = useTimer()
  useTick(e.running)
  const m = appMeta(e.app)
  const href = openURL(e, settings)
  const sent = e.deliver && e.app && !e.collected
  return (
    <div className={`entry-row ${e.running ? 'is-running' : ''} ${e.collected ? 'is-collected' : ''}`}>
      <button type="button" className="entry-main" onClick={() => onEdit(e)} aria-label={`Edit ${entryName(e)}`}>
        <span className="entry-title">
          <span className="ellipsis">{entryName(e)}</span>
          {e.billable && <Icon name="receipt" size={12} className="muted" title="Billable" />}
        </span>
        <span className="entry-sub">
          {e.project_name && <span className="entry-proj"><Dot color={e.project_color} />{e.project_name}</span>}
          {e.app && <span className="entry-app" style={{ ['--app' as string]: m.color }}><Icon name={m.icon} size={12} />{e.app_label && e.app_label !== e.title ? e.app_label : m.name.replace('Pocket ', '')}</span>}
          {sent && (e.delivered_at ? <span className="entry-sent ok" title={`Logged in ${m.name}`}><Icon name="check" size={11} /></span> : e.deliver_error ? <span className="entry-sent bad" title={e.deliver_error}><Icon name="alert" size={11} /></span> : null)}
          {e.collected && <span className="muted">{originText(e)}</span>}
        </span>
      </button>
      <span className="entry-when muted small tnum">{fmtISOTime(e.started_at)}{e.ended_at ? `–${fmtISOTime(e.ended_at)}` : ''}</span>
      <span className="entry-dur tnum">{e.running ? fmtHMS(elapsed(e)) : fmtDur(e.duration_s)}</span>
      <span className="entry-acts">
        {href && <a className="btn ghost icon-only sm" href={href} target="_blank" rel="noreferrer" title={`Open in ${m.name}`} aria-label={`Open in ${m.name}`}><Icon name="external" size={14} /></a>}
        {!e.running && !e.collected ? <button type="button" className="btn ghost icon-only sm" onClick={() => continueEntry(e)} disabled={!!running && running.id === e.id} title="Continue: start this again" aria-label="Continue"><Icon name="play" size={14} /></button>
          : <span className="btn-ph" aria-hidden="true" />}
      </span>
    </div>
  )
}

/** Add an entry by hand, or edit one. */
export function EntryModal({ entry, onClose, initialDate }: { entry?: Entry; onClose: () => void; initialDate?: string }) {
  const { settings } = useApp()
  const { toast } = useToast()
  const now = new Date()
  const defStart = initialDate ? new Date(initialDate + 'T09:00') : new Date(now.getTime() - 3600_000)
  const [f, setF] = useState(() => ({
    title: entry?.title || '', note: entry?.note || '', tags: entry?.tags || '', project_id: entry?.project_id ?? null,
    billable: entry?.billable ?? !!settings?.default_billable, touchedBill: false,
    start: localInput(entry?.started_at || defStart),
    end: localInput(entry?.ended_at || (entry ? new Date() : new Date(defStart.getTime() + 3600_000))),
    target: entry?.app ? { app: entry.app, app_ref: entry.app_ref, app_label: entry.app_label, app_url: entry.app_url } as ForTarget : null,
    deliver: entry ? entry.deliver : true,
  }))
  const [pick, setPick] = useState(false)
  const [busy, setBusy] = useState(false)
  const [del, setDel] = useState(false)
  const readOnly = !!entry?.collected
  const running = !!entry?.running
  const durS = Math.max(0, (new Date(f.end).getTime() - new Date(f.start).getTime()) / 1000)
  const m = f.target ? appMeta(f.target.app) : null

  const save = async (ev: React.FormEvent) => {
    ev.preventDefault()
    if (!running && durS <= 0) { toast('The end is before the start', 'err'); return }
    setBusy(true)
    const body: Record<string, unknown> = {
      title: f.title.trim(), note: f.note, tags: f.tags, project_id: f.project_id,
      app: f.target?.app || '', app_ref: f.target?.app_ref || '', app_label: f.target?.app_label || '', app_url: f.target?.app_url || '', deliver: !!f.target && f.deliver,
      started_at: new Date(f.start).toISOString(),
    }
    if (!running) body.ended_at = new Date(f.end).toISOString()
    if (f.touchedBill || entry) body.billable = f.billable
    try {
      if (entry) await api.put(`${V1}/entries/${entry.id}`, body)
      else await api.post(`${V1}/entries`, body)
      announceChange()
      toast(entry ? 'Saved' : 'Added')
      onClose()
    } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  const remove = async () => {
    try { await api.del(`${V1}/entries/${entry!.id}${entry?.remote_id ? '?everywhere=1' : ''}`); announceChange(); toast('Deleted'); onClose() } catch (e) { toast((e as Error).message, 'err') }
  }
  const resend = async () => {
    try { const r = await api.post<Entry>(`${V1}/entries/${entry!.id}/resend`); announceChange(); toast(r.deliver_error ? r.deliver_error : `Logged in ${appMeta(r.app).name}`, r.deliver_error ? 'err' : undefined) } catch (e) { toast((e as Error).message, 'err') }
  }
  const href = entry ? openURL(entry, settings) : ''

  if (readOnly && entry) {
    return (
      <Modal title={entryName(entry)} onClose={onClose} size="sm" footer={<><button type="button" className="btn" onClick={onClose}>Close</button>{href && <a className="btn primary" href={href} target="_blank" rel="noreferrer"><Icon name="external" size={15} />Open in {appMeta(entry.app).name}</a>}</>}>
        <div className="stack">
          <p className="muted">{originText(entry)}. Pocket Time shows it with the rest of your time; change or delete it in {appMeta(entry.app).name}.</p>
          <dl className="set-dl">
            <div><dt>When</dt><dd>{fmtDate(entry.date)} · {fmtISOTime(entry.started_at)}–{fmtISOTime(entry.ended_at)}</dd></div>
            <div><dt>How long</dt><dd>{fmtDur(entry.duration_s)}</dd></div>
            {entry.app_label && <div><dt>For</dt><dd>{entry.app_label}</dd></div>}
          </dl>
        </div>
      </Modal>
    )
  }
  return (
    <Modal title={entry ? (running ? 'The running timer' : 'Edit entry') : 'Add time'} onClose={onClose} size="sm">
      <form onSubmit={save} className="grid" style={{ gap: 14 }}>
        <Field label="What"><input value={f.title} onChange={e => setF({ ...f, title: e.target.value })} placeholder="What did you work on?" autoFocus={!entry} maxLength={300} /></Field>
        <Field group label="Project"><ProjectSelect value={f.project_id} onChange={id => setF({ ...f, project_id: id })} /></Field>
        <div className="two-col tight">
          <Field label="Start"><input type="datetime-local" value={f.start} onChange={e => setF({ ...f, start: e.target.value })} required /></Field>
          {!running && <Field label="End" help={durS > 0 ? fmtDur(durS) : undefined}><input type="datetime-local" value={f.end} onChange={e => setF({ ...f, end: e.target.value })} required /></Field>}
        </div>
        <Field group label="For an app" help={f.target ? undefined : 'A song in Pocket Guitar, an issue in Pocket Projects, a deck in Pocket Learn…'}>
          <div className="row wrap">
            {f.target ? <ForChip target={f.target} onClick={() => setPick(true)} onClear={() => setF({ ...f, target: null })} /> : <button type="button" className="btn sm" onClick={() => setPick(true)}><Icon name="layers" size={14} />Choose…</button>}
          </div>
        </Field>
        {f.target && m && <div className="check-line"><Switch size="sm" checked={f.deliver} onChange={v => setF({ ...f, deliver: v })} label={`Log it in ${m.name}`} /><span className="small">Log it in {m.name} too{entry?.delivered_at ? ' (done)' : ''}</span></div>}
        {entry?.deliver_error && <div className="callout warn small"><Icon name="alert" size={14} /><span>{appMeta(entry.app).name}: {entry.deliver_error} <button type="button" className="link-btn" onClick={resend}>Try again</button></span></div>}
        <div className="check-line"><Switch size="sm" checked={f.billable} onChange={v => setF({ ...f, billable: v, touchedBill: true })} label="Billable" /><span className="small">Billable</span></div>
        <Field label="Tags" help="Separated by commas."><input value={f.tags} onChange={e => setF({ ...f, tags: e.target.value })} placeholder="meeting, design" /></Field>
        <Field label="Note"><textarea rows={3} value={f.note} onChange={e => setF({ ...f, note: e.target.value })} /></Field>
        {entry && originText(entry) && <p className="muted small">{originText(entry)}.</p>}
        <div className="form-actions">
          {entry && !running && <button type="button" className="btn ghost danger-text" onClick={() => setDel(true)}><Icon name="trash" size={14} />Delete</button>}
          <span className="grow" />
          <button type="button" className="btn" onClick={onClose}>Cancel</button>
          <button className="btn primary" disabled={busy}>{entry ? 'Save' : 'Add'}</button>
        </div>
      </form>
      {pick && <ForPicker initialApp={f.target?.app} onClose={() => setPick(false)} onPick={t => setF(x => ({ ...x, target: t, title: x.title || t.app_label }))} />}
      {del && entry && <Confirm title="Delete this entry?" message={entry.remote_id ? `It is removed from ${appMeta(entry.app).name} too.` : 'This cannot be undone.'} confirmLabel="Delete" onCancel={() => setDel(false)} onConfirm={() => { setDel(false); remove() }} />}
    </Modal>
  )
}
