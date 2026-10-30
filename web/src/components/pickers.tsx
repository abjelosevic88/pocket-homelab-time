// Choosing a project, and what in another Pocket app the time is for.
import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useData } from '../lib/data'
import { APPS, appMeta } from '../lib/time'
import type { ForTarget, Item, Project } from '../lib/types'
import { Icon } from './icons'
import { Select } from './Select'
import { Empty, Loading, Modal, useDebounce, useToast } from './ui'

export const COLORS = ['#2f6fdf', '#0f9d77', '#b0571d', '#c2255c', '#7048e8', '#e8590c', '#2b8a3e', '#0b7285', '#8a8f98', '#b8231f']

export function Dot({ color, size = 9 }: { color?: string; size?: number }) {
  return <i className="pdot" style={{ width: size, height: size, background: color || 'var(--faint)' }} aria-hidden="true" />
}

/** A project picker that can create a project on the spot. */
export function ProjectSelect({ value, onChange, className = '' }: { value: number | null; onChange: (id: number | null) => void; className?: string }) {
  const { projects, reload } = useData()
  const { toast } = useToast()
  const live = projects.filter(p => !p.archived || p.id === value)
  const create = async (name: string) => {
    try {
      const p = await api.post<Project>(`${V1}/projects`, { name, color: COLORS[projects.length % COLORS.length] })
      reload()
      return String(p.id)
    } catch (e) { toast((e as Error).message, 'err'); return '' }
  }
  return (
    <Select className={className} ariaLabel="Project" value={value ? String(value) : ''} placeholder="No project" clearLabel="No project"
      onChange={v => onChange(v ? Number(v) : null)} onCreate={create} createLabel="New project"
      options={live.map(p => ({ value: String(p.id), label: p.name, hint: p.client || undefined }))} />
  )
}

/** The "for" chip: what in which app the time is for. */
export function ForChip({ target, onClear, onClick }: { target: ForTarget; onClear?: () => void; onClick?: () => void }) {
  const m = appMeta(target.app)
  return (
    <span className="for-chip" style={{ ['--app' as string]: m.color }}>
      <button type="button" className="for-chip-main" onClick={onClick} title={`For ${target.app_label || m.name} in ${m.name}`}>
        <Icon name={m.icon} size={13} /><span className="ellipsis">{target.app_label || m.name}</span>
      </button>
      {onClear && <button type="button" className="for-chip-x" aria-label="Not for an app" onClick={onClear}><Icon name="x" size={12} /></button>}
    </span>
  )
}

/** Choose an app, then what in it (a song, an issue, a deck). */
export function ForPicker({ onPick, onClose, initialApp }: { onPick: (t: ForTarget) => void; onClose: () => void; initialApp?: string }) {
  const { apps, reloadApps } = useData()
  const connected = apps.filter(a => a.connected)
  const [app, setApp] = useState<string>(initialApp || connected[0]?.slug || '')
  const [q, setQ] = useState('')
  const dq = useDebounce(q, 250)
  const [items, setItems] = useState<Item[] | null>(null)
  const [err, setErr] = useState('')
  useEffect(() => { reloadApps() }, [reloadApps])
  useEffect(() => { if (!app && connected[0]) setApp(connected[0].slug) }, [connected, app])
  useEffect(() => {
    if (!app) return
    let live = true
    setItems(null); setErr('')
    api.get<{ items: Item[]; error?: string }>(`${V1}/apps/${app}/items${dq ? `?q=${encodeURIComponent(dq)}` : ''}`)
      .then(r => { if (live) { setItems(r.items); setErr(r.error || '') } })
      .catch(e => { if (live) { setItems([]); setErr((e as Error).message) } })
    return () => { live = false }
  }, [app, dq])
  const cur = apps.find(a => a.slug === app)
  const what = cur ? (cur.item_name || APPS[cur.slug]?.item || 'item') : 'item'
  return (
    <Modal title="What is this time for?" onClose={onClose} size="lg" className="for-modal">
      {apps.length > 0 && connected.length === 0 ? (
        <Empty icon="layers" title="No apps connected yet">
          Connect Pocket Guitar, Projects, Learn and the others in <Link to="/settings/apps" onClick={onClose}>Settings → Connected apps</Link>, then pick a song, an issue or a deck here. The time is logged there when the timer stops.
        </Empty>
      ) : (
        <div className="for-body">
          <div className="for-apps" role="tablist" aria-label="Apps">
            {apps.map(a => {
              const m = appMeta(a.slug)
              return (
                <button key={a.slug} type="button" role="tab" aria-selected={a.slug === app} disabled={!a.connected}
                  className={`for-app ${a.slug === app ? 'active' : ''}`} style={{ ['--app' as string]: m.color }} onClick={() => { setApp(a.slug); setQ('') }}
                  title={a.connected ? a.reason : `${a.name}: ${a.detail || 'not connected'}`}>
                  <span className="for-app-ico"><Icon name={m.icon} size={15} /></span>
                  <span className="ellipsis">{a.name.replace('Pocket ', '')}</span>
                </button>
              )
            })}
          </div>
          <div className="for-list">
            <div className="search-box"><Icon name="search" size={15} /><input autoFocus value={q} onChange={e => setQ(e.target.value)} placeholder={`Find a ${what}…`} aria-label={`Find a ${what}`} /></div>
            {err && <div className="callout warn small"><Icon name="alert" size={14} /><span>{err}</span></div>}
            {items === null ? <Loading /> : items.length === 0 && !err ? <p className="muted small pad-sm">Nothing found.</p> : (
              <ul className="list for-items">
                {items.map(it => (
                  <li key={it.ref || '_'}>
                    <button type="button" className="list-row for-item" onClick={() => { onPick({ app, app_ref: it.ref, app_label: it.label, app_url: it.url || '' }); onClose() }}>
                      <span className="grow ellipsis"><b>{it.label}</b>{it.detail && <span className="muted small"> · {it.detail}</span>}</span>
                      <Icon name="chevron-right" size={15} className="muted" />
                    </button>
                  </li>
                ))}
              </ul>
            )}
            {cur && <p className="muted small">When the timer stops, the time is logged in {cur.name} too.</p>}
          </div>
        </div>
      )}
    </Modal>
  )
}
