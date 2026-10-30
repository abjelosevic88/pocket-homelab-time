// Home: the timer, today and this week, and the last few days of entries.
import { useState } from 'react'
import { Link } from 'react-router-dom'
import { useApp } from '../lib/app-context'
import { useData } from '../lib/data'
import { weekday } from '../lib/format'
import { useTimer, useTick } from '../lib/timer'
import { appMeta, elapsed, fmtDur, fmtHM } from '../lib/time'
import type { Entry } from '../lib/types'
import { TimerBar } from '../components/TimerBar'
import { DayGroups, EntryModal } from '../components/Entries'
import { Ring } from '../components/Chart'
import { Dot } from '../components/pickers'
import { Icon } from '../components/icons'
import { Empty, Loading, useIsMobile } from '../components/ui'

export default function Timer() {
  const { dashboard: d } = useData()
  const { running } = useTimer()
  const { settings } = useApp()
  const [edit, setEdit] = useState<Entry | null>(null)
  const ring = useIsMobile() ? 48 : 64
  useTick(!!running)
  // the totals include the running timer as of the last load; keep it live
  const live = running && d?.running && d.running.id === running.id ? elapsed(running) - d.running.duration_s : 0
  const todayS = (d?.today_s || 0) + (running?.date === d?.today ? live : 0)
  const weekS = (d?.week_s || 0) + live
  const max = Math.max(...(d?.days || []).map(x => x.seconds), (d?.daily_goal_min || 0) * 60, 3600)
  const connected = Object.keys(settings || {}).some(k => k.endsWith('_configured') && k !== 'notify_configured' && settings?.[k] === true)
  return (
    <div className="stack loose">
      <TimerBar />
      {!d ? <Loading /> : <>
        <div className="kpis">
          <div className="card kpi">
            <Ring pct={d.daily_goal_min ? (todayS / 60 / d.daily_goal_min) * 100 : 0} size={ring} stroke={6}><span className="ring-num"><b className="tnum">{fmtHM(todayS)}</b></span></Ring>
            <div><div className="kpi-l">Today</div><div className="kpi-v tnum">{fmtDur(todayS)}</div>{d.daily_goal_min > 0 && <div className="muted small">of {fmtDur(d.daily_goal_min * 60)}</div>}</div>
          </div>
          <div className="card kpi">
            <Ring pct={d.weekly_goal_min ? (weekS / 60 / d.weekly_goal_min) * 100 : 0} size={ring} stroke={6}><span className="ring-num"><b className="tnum">{Math.round(weekS / 3600)}h</b></span></Ring>
            <div><div className="kpi-l">This week</div><div className="kpi-v tnum">{fmtDur(weekS)}</div>{d.weekly_goal_min > 0 && <div className="muted small">of {fmtDur(d.weekly_goal_min * 60)}</div>}</div>
          </div>
          <div className="card kpi kpi-week">
            <div className="week-bars" role="img" aria-label="This week by day">
              {d.days.map(x => {
                const s = x.seconds + (running && x.date === running.date ? live : 0)
                return (
                  <div key={x.date} className={`wb ${x.date === d.today ? 'today' : ''}`} title={`${weekday(x.date, true)}: ${fmtDur(s)}`}>
                    <span className="wb-track">{d.daily_goal_min > 0 && <i className="wb-goal" style={{ bottom: `${(d.daily_goal_min * 60 / max) * 100}%` }} />}<span className="wb-bar" style={{ height: `${(s / max) * 100}%` }} /></span>
                    <span className="wb-l">{weekday(x.date).slice(0, 2)}</span>
                  </div>
                )
              })}
            </div>
          </div>
        </div>
        {(d.projects.length > 0 || d.apps.some(a => a.key !== 'time')) && (
          <div className="two-col">
            <section className="card">
              <div className="card-head"><div className="grow"><h2>Projects this week</h2></div><Link className="btn ghost sm" to="/reports">Reports</Link></div>
              <div className="card-body">
                <ul className="mini-list">
                  {d.projects.slice(0, 6).map(p => (
                    <li key={p.key}><Dot color={p.color} /><span className="grow ellipsis">{p.label}</span>
                      {p.goal_min ? <span className="muted small tnum">{Math.round((p.seconds / 60 / p.goal_min) * 100)}%</span> : null}
                      <span className="tnum">{fmtDur(p.seconds)}</span></li>
                  ))}
                </ul>
              </div>
            </section>
            <section className="card">
              <div className="card-head"><div className="grow"><h2>From your apps</h2></div><Link className="btn ghost sm" to="/settings/apps">Apps</Link></div>
              <div className="card-body">
                <ul className="mini-list">
                  {d.apps.map(a => {
                    const m = appMeta(a.key)
                    return <li key={a.key}><span className="app-ico sm" style={{ ['--app' as string]: m.color }}><Icon name={m.icon} size={12} /></span><span className="grow ellipsis">{a.label}</span><span className="tnum">{fmtDur(a.seconds)}</span></li>
                  })}
                </ul>
              </div>
            </section>
          </div>
        )}
        {d.recent.length === 0 ? (
          <Empty icon="timer" title="Nothing tracked yet">
            Type what you are doing above and press start. Timers you start in Pocket Guitar, Projects or Learn show up here too, and you can stop them from either place.
            {!connected && <> <Link to="/settings/apps">Connect your apps</Link> to bring their time in.</>}
          </Empty>
        ) : <DayGroups entries={d.recent} onEdit={setEdit} today={d.today} />}
      </>}
      {edit && <EntryModal entry={edit} onClose={() => setEdit(null)} />}
    </div>
  )
}
