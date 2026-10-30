// Your projects: colour, client, billable rate, a weekly target.
import React, { useState } from 'react'
import { Link } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import { useData } from '../lib/data'
import { announceChange } from '../lib/timer'
import { fmtDur, fmtMoney } from '../lib/time'
import type { Project } from '../lib/types'
import { Icon } from '../components/icons'
import { COLORS, Dot } from '../components/pickers'
import { Confirm, Empty, Field, Modal, PageHeader, Switch, useToast } from '../components/ui'

export default function Projects() {
  const { projects, dashboard } = useData()
  const { settings } = useApp()
  const [edit, setEdit] = useState<Project | 'new' | null>(null)
  const [showArchived, setShowArchived] = useState(false)
  const week = new Map((dashboard?.projects || []).map(g => [g.key, g.seconds]))
  const list = projects.filter(p => showArchived || !p.archived)
  const archived = projects.filter(p => p.archived).length
  return (
    <div className="stack loose">
      <PageHeader title="Projects" sub="Group your time: client work, a side project, a hobby."
        actions={<button type="button" className="btn primary" onClick={() => setEdit('new')}><Icon name="plus" size={15} />New project</button>} />
      {list.length === 0 ? <Empty icon="tag" title="No projects yet">Projects are optional. Add one for each client or thing you want totals for; a timer can be put in a project when it starts.</Empty> : (
        <section className="card">
          <div className="card-body flush">
            <ul className="list">
              {list.map(p => {
                const s = week.get(String(p.id)) || 0
                return (
                  <li key={p.id}>
                    <div className={`list-row proj-row ${p.archived ? 'off' : ''}`}>
                      <Dot color={p.color} size={12} />
                      <button type="button" className="grow col proj-main" onClick={() => setEdit(p)}>
                        <span className="ellipsis"><b>{p.name}</b>{p.archived && <span className="badge">archived</span>}</span>
                        <span className="muted small ellipsis">{[p.client, p.billable ? (p.rate ? `${fmtMoney(p.rate, settings?.currency || '')}/h` : 'billable') : '', p.goal_min ? `${fmtDur(p.goal_min * 60)} a week` : ''].filter(Boolean).join(' · ') || 'No details'}</span>
                      </button>
                      {p.goal_min > 0 && <span className="proj-goal"><span className="progress"><span style={{ width: `${Math.min(100, (s / 60 / p.goal_min) * 100)}%`, background: p.color || undefined }} /></span></span>}
                      <span className="tnum proj-week" title="This week">{fmtDur(s)}</span>
                      <Link className="btn ghost icon-only sm" to={`/entries?project=${p.id}`} title="Entries" aria-label={`Entries of ${p.name}`}><Icon name="list" size={14} /></Link>
                    </div>
                  </li>
                )
              })}
            </ul>
          </div>
        </section>
      )}
      {archived > 0 && <div><button type="button" className="link-btn small" onClick={() => setShowArchived(!showArchived)}>{showArchived ? 'Hide' : 'Show'} {archived} archived</button></div>}
      {edit && <ProjectModal project={edit === 'new' ? undefined : edit} onClose={() => setEdit(null)} />}
    </div>
  )
}

function ProjectModal({ project, onClose }: { project?: Project; onClose: () => void }) {
  const { projects, reload } = useData()
  const { settings } = useApp()
  const { toast } = useToast()
  const [f, setF] = useState({ name: project?.name || '', color: project?.color || COLORS[projects.length % COLORS.length], client: project?.client || '', billable: project?.billable ?? !!settings?.default_billable,
    rate: String(project?.rate || ''), goal_h: project?.goal_min ? String(project.goal_min / 60) : '', archived: !!project?.archived })
  const [busy, setBusy] = useState(false)
  const [del, setDel] = useState(false)
  const save = async (e: React.FormEvent) => {
    e.preventDefault(); setBusy(true)
    const body = { name: f.name, color: f.color, client: f.client, billable: f.billable, rate: Number(f.rate) || 0, goal_min: Math.round((Number(f.goal_h) || 0) * 60), archived: f.archived }
    try { if (project) await api.put(`${V1}/projects/${project.id}`, body); else await api.post(`${V1}/projects`, body); reload(); announceChange(); onClose() } catch (err) { toast((err as Error).message, 'err') } finally { setBusy(false) }
  }
  const remove = async () => { try { await api.del(`${V1}/projects/${project!.id}`); reload(); announceChange(); onClose() } catch (err) { toast((err as Error).message, 'err') } }
  return (
    <Modal title={project ? 'Edit project' : 'New project'} onClose={onClose} size="sm">
      <form onSubmit={save} className="grid" style={{ gap: 14 }}>
        <Field label="Name"><input value={f.name} onChange={e => setF({ ...f, name: e.target.value })} required autoFocus maxLength={120} /></Field>
        <Field group label="Colour"><div className="row wrap">{COLORS.map(c => <button key={c} type="button" className={`swatch ${f.color === c ? 'active' : ''}`} style={{ background: c }} aria-label={c} onClick={() => setF({ ...f, color: c })} />)}</div></Field>
        <Field label="Client" help="Optional."><input value={f.client} onChange={e => setF({ ...f, client: e.target.value })} maxLength={120} /></Field>
        <div className="check-line"><Switch size="sm" checked={f.billable} onChange={v => setF({ ...f, billable: v })} label="Billable" /><span className="small">New time in this project is billable</span></div>
        <div className="two-col tight">
          <Field label={`Rate per hour${settings?.currency ? ` (${settings.currency})` : ''}`} help="For the amount in Reports."><input type="number" min={0} step="0.01" value={f.rate} onChange={e => setF({ ...f, rate: e.target.value })} /></Field>
          <Field label="Hours a week" help="A target; optional."><input type="number" min={0} max={168} step="0.5" value={f.goal_h} onChange={e => setF({ ...f, goal_h: e.target.value })} /></Field>
        </div>
        {project && <div className="check-line"><Switch size="sm" checked={f.archived} onChange={v => setF({ ...f, archived: v })} label="Archived" /><span className="small">Archived (hidden from the timer)</span></div>}
        <div className="form-actions">
          {project && <button type="button" className="btn ghost danger-text" onClick={() => setDel(true)}><Icon name="trash" size={14} />Delete</button>}
          <span className="grow" />
          <button type="button" className="btn" onClick={onClose}>Cancel</button>
          <button className="btn primary" disabled={busy}>{project ? 'Save' : 'Create'}</button>
        </div>
      </form>
      {del && <Confirm title={`Delete ${project?.name}?`} message="Its entries stay, without a project." confirmLabel="Delete" onCancel={() => setDel(false)} onConfirm={() => { setDel(false); remove() }} />}
    </Modal>
  )
}
