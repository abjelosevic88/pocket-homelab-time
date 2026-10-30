// Left column: the start/stop button, sections, this week by app — the Pocket Calendar sidebar.
import { Link, NavLink } from 'react-router-dom'
import { useApp } from '../lib/app-context'
import { useData } from '../lib/data'
import { useTimer, useTick } from '../lib/timer'
import { appMeta, elapsed, entryName, fmtDur, fmtHMS } from '../lib/time'
import { Icon } from './icons'

export function Sidebar({ onNavigate, onToggle }: { onNavigate: () => void; onToggle: () => void }) {
  const { status, settings } = useApp()
  const { running, busy } = useTimer()
  const { dashboard: d } = useData()
  useTick(!!running)
  const title = settings?.title || status?.title || 'Pocket Time'
  const nav: { to: string; label: string; icon: string; end?: boolean; count?: string }[] = [
    { to: '/', label: 'Timer', icon: 'timer', end: true, count: d ? fmtDur(d.today_s) : undefined },
    { to: '/entries', label: 'Entries', icon: 'list' },
    { to: '/reports', label: 'Reports', icon: 'chart' },
    { to: '/projects', label: 'Projects', icon: 'tag' },
  ]
  const apps = (d?.apps || []).filter(a => a.key !== 'time')
  return (
    <aside className="sidebar" aria-label="Time">
      <div className="sb-brand">
        <img src="/favicon.svg" alt="" className="sb-logo" />
        <span className="sb-title">{title !== 'Pocket Time' ? title : <>Pocket <b>Time</b></>}</span>
        {status?.version && <span className="sb-version" title={`Version ${status.version}`}>{/^v?\d+\.\d+/.test(status.version) ? 'v' + status.version.replace(/^v/, '').replace(/[-+].*$/, '') : status.version}</span>}
      </div>
      <button type="button" className={`btn primary create-btn ${running ? 'is-running' : ''}`} onClick={onToggle} disabled={busy} title="Start or stop (s)">
        {running ? <><Icon name="stop" size={15} strokeWidth={2.2} /><span className="grow ellipsis left">Stop</span><span className="tnum">{fmtHMS(elapsed(running))}</span></>
          : <><Icon name="play" size={15} strokeWidth={2.2} /><span className="grow left">Start timer</span></>}
      </button>
      {running && (
        <Link to="/" className="sb-running" onClick={onNavigate}>
          <span className="sb-running-dot" />
          <span className="grow ellipsis">{entryName(running)}</span>
          {running.source !== 'time' && running.source !== 'manual' && <Icon name={appMeta(running.source).icon} size={13} className="muted" />}
        </Link>
      )}

      <nav className="sb-nav" aria-label="Sections">
        {nav.map(n => (
          <NavLink key={n.to} to={n.to} end={n.end} onClick={onNavigate} className={({ isActive }) => `sb-nav-item ${isActive ? 'active' : ''}`}>
            <Icon name={n.icon} size={16} />{n.label}
            {!!n.count && <span className="sb-count tnum">{n.count}</span>}
          </NavLink>
        ))}
      </nav>

      {d && (
        <section className="sb-sec">
          <h4 className="sb-h"><span className="sb-h-btn">This week</span><span className="sb-h-n tnum">{fmtDur(d.week_s)}</span></h4>
          <ul className="sb-apps">
            {apps.length === 0 && <li className="muted small sb-hint">Time from Guitar, Projects, Learn and your other apps shows up here. <Link to="/settings/apps" onClick={onNavigate}>Connect them</Link>.</li>}
            {apps.map(a => {
              const m = appMeta(a.key)
              return (
                <li key={a.key}><Link to={`/entries?app=${a.key}`} onClick={onNavigate} className="sb-app">
                  <span className="app-ico sm" style={{ ['--app' as string]: m.color }}><Icon name={m.icon} size={12} /></span>
                  <span className="grow ellipsis">{m.name.replace('Pocket ', '')}</span>
                  <span className="tnum muted small">{fmtDur(a.seconds)}</span>
                </Link></li>
              )
            })}
          </ul>
        </section>
      )}

      <div className="sb-foot">
        <Link to="/settings" className="sb-foot-link" onClick={onNavigate}><Icon name="settings" size={15} />Settings</Link>
        <button type="button" className="sb-foot-link" onClick={() => { onNavigate(); window.dispatchEvent(new CustomEvent('ptm:shortcuts')) }}><Icon name="keyboard" size={15} />Shortcuts</button>
      </div>
    </aside>
  )
}
