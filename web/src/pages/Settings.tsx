import { AskButton } from '../components/AskButton'
import React, { useEffect, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { APIToken, AppInfo, ConnMode, ConnVia, Settings as SettingsT, User } from '../lib/types'
import { DATE_FORMATS, TIME_FORMATS, ago, fmtBytes, fmtDateTime, type DateFormat, type TimeFormat } from '../lib/format'
import { APPS, appMeta } from '../lib/time'
import { Avatar, Confirm, CoreHint, Field, Loading, Modal, Segmented, SettingRow, Switch, Tabs, useAsync, useIsMobile, useToast } from '../components/ui'
import { Icon } from '../components/icons'
import { Select } from '../components/Select'
import { ShortcutsSheet } from '../components/ShortcutsSheet'
import { PocketModelSelect } from '../components/PocketModelSelect'

type TabId = 'general' | 'notifications' | 'apps' | 'ai' | 'profile' | 'people' | 'tokens' | 'data' | 'about'

const TABS: { id: TabId; label: string; icon: string; sub: string }[] = [
  { id: 'general', label: 'Timer & reports', icon: 'timer', sub: 'Your daily and weekly targets, how the timer behaves, rounding and billing in reports, and dates.' },
  { id: 'notifications', label: 'Notifications', icon: 'bell', sub: 'A reminder when a timer has been running for hours, and a weekly report.' },
  { id: 'apps', label: 'Connected apps', icon: 'layers', sub: 'Pocket Projects, Guitar, Learn, Still and Invoicing: their time shows up here, and time you track here is logged there.' },
  { id: 'ai', label: 'AI', icon: 'sparkles', sub: 'Who writes the look back in Reports and answers questions: Pocket AI through Core, or your own OpenAI key.' },
  { id: 'profile', label: 'Profile', icon: 'user', sub: 'Your name, email and password.' },
  { id: 'people', label: 'People', icon: 'users', sub: 'Everyone who has an account on this box.' },
  { id: 'tokens', label: 'API & shortcuts', icon: 'key', sub: 'Personal tokens for Homepage widgets, Siri Shortcuts and scripts.' },
  { id: 'data', label: 'Backup & data', icon: 'database', sub: 'Download the database, export your time, or import it from Toggl, Clockify, Kimai or Harvest.' },
  { id: 'about', label: 'About', icon: 'info', sub: 'Version, numbers and the server.' },
]

export default function Settings() {
  const { tab: raw } = useParams()
  const navigate = useNavigate()
  const mobile = useIsMobile()
  const tab: TabId = TABS.find(t => t.id === raw)?.id || 'general'
  const current = TABS.find(t => t.id === tab)!
  const go = (id: TabId) => navigate(`/settings/${id}`, { replace: true })
  useEffect(() => { document.querySelector('.set-content')?.scrollTo({ top: 0 }) }, [tab])
  useEffect(() => {
    const h = (e: KeyboardEvent) => { if (e.key === 'Escape' && !document.querySelector('.modal-backdrop, .menu, .sel-pop')) navigate('/') }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  }, [navigate])
  return (
    <div className="set-page">
      <header className="set-header">
        <button type="button" className="btn ghost icon-only" aria-label="Back" title="Back (esc)" onClick={() => navigate('/')}><Icon name="chevron-left" size={18} /></button>
        <img src="/favicon.svg" alt="" width={24} height={24} className="set-logo" />
        <h1>Settings</h1>
        <span style={{ flex: 1 }} />
        <AskButton />
      </header>
      {mobile && <div className="set-tabs-mobile"><Tabs tabs={TABS.map(t => ({ id: t.id, label: t.label, icon: t.icon }))} value={tab} onChange={go} /></div>}
      <div className="set-body">
        {!mobile && (
          <nav className="set-nav" aria-label="Settings sections">
            {TABS.map(t => (
              <button key={t.id} type="button" className={`set-nav-item ${t.id === tab ? 'active' : ''}`} aria-current={t.id === tab ? 'page' : undefined} onClick={() => go(t.id)}>
                <Icon name={t.icon} size={16} />{t.label}
              </button>
            ))}
          </nav>
        )}
        <main className="set-content">
          <div className="set-inner" key={tab}>
            <div className="set-title"><h2>{current.label}</h2><p>{current.sub}</p></div>
            {tab === 'general' && <General />}
            {tab === 'notifications' && <Notifications />}
            {tab === 'apps' && <Integrations />}
            {tab === 'ai' && <AITab />}
            {tab === 'profile' && <Profile />}
            {tab === 'people' && <Users />}
            {tab === 'tokens' && <Tokens />}
            {tab === 'data' && <Data />}
            {tab === 'about' && <About />}
          </div>
        </main>
      </div>
    </div>
  )
}

function Section({ title, sub, icon, iconColor, actions, children }: { title: React.ReactNode; sub?: React.ReactNode; icon?: string; iconColor?: string; actions?: React.ReactNode; children?: React.ReactNode }) {
  return (
    <section className="set-section">
      <div className="set-section-head">
        {icon && <span className="set-app-ico" style={iconColor ? { background: iconColor } : undefined}><Icon name={icon} size={17} /></span>}
        <div className="grow"><h3>{title}</h3>{sub && <p>{sub}</p>}</div>
        {actions && <div className="row">{actions}</div>}
      </div>
      {children && <div className="set-section-body">{children}</div>}
    </section>
  )
}

function useSettingsForm() {
  const { settings, setSettings, user } = useApp()
  const { toast } = useToast()
  const [f, setF] = useState<SettingsT | null>(settings)
  const [busy, setBusy] = useState(false)
  useEffect(() => { if (!f && settings) setF(settings) }, [settings]) // eslint-disable-line react-hooks/exhaustive-deps
  const save = async (patch?: Record<string, unknown>) => {
    if (!f) return
    setBusy(true)
    try { const s = await api.put<SettingsT>(`${V1}/settings`, { ...f, ...patch }); setSettings(s); setF(s); toast('Saved') } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  return { f, setF, save, busy, isAdmin: user?.role === 'admin' }
}

function useTester() {
  const [test, setTest] = useState<Record<string, string>>({})
  const run = async (which: string, url: string, key: string, extra: Record<string, string> = {}) => {
    setTest(t => ({ ...t, [which]: 'testing…' }))
    try { const r = await api.post<{ ok: boolean; message?: string; error?: string }>(`${V1}/integrations/test`, { which, url, key, ...extra }); setTest(t => ({ ...t, [which]: r.ok ? `✓ ${r.message}` : `✕ ${r.error}` })) } catch (e) { setTest(t => ({ ...t, [which]: `✕ ${(e as Error).message}` })) }
  }
  const status = (which: string) => <span className={`small ${test[which]?.startsWith('✓') ? 'ok' : test[which]?.startsWith('✕') ? 'bad' : 'muted'}`}>{test[which]}</span>
  return { run, status }
}

const DAYS: [number, string][] = [[1, 'Monday'], [2, 'Tuesday'], [3, 'Wednesday'], [4, 'Thursday'], [5, 'Friday'], [6, 'Saturday'], [7, 'Sunday']]
const HOURS = Array.from({ length: 24 }, (_, h) => ({ value: String(h), label: `${String(h).padStart(2, '0')}:00` }))

const hours = (min: number) => (min ? String(Math.round((min / 60) * 10) / 10) : '')
const toMin = (h: string) => Math.round((Number(h) || 0) * 60)

function General() {
  const { f, setF, save, busy, isAdmin } = useSettingsForm()
  if (!f) return <Loading />
  return (
    <div className="stack loose">
      <Section title="Targets" icon="target" sub="Optional. They fill the rings on the Timer page.">
        <SettingRow title="Hours a day" help="Empty = no daily target."><input type="number" min={0} max={24} step="0.5" value={hours(f.daily_goal_min)} onChange={e => setF({ ...f, daily_goal_min: toMin(e.target.value) })} className="set-time" /></SettingRow>
        <SettingRow title="Hours a week" help="Empty = no weekly target."><input type="number" min={0} max={168} step="0.5" value={hours(f.weekly_goal_min)} onChange={e => setF({ ...f, weekly_goal_min: toMin(e.target.value) })} className="set-time" /></SettingRow>
      </Section>
      <Section title="The timer" icon="timer" sub="One timer runs at a time, shared with your other Pocket apps.">
        <SettingRow title="Ask before switching" help="When a timer runs and you start another (here or in another app’s page here), ask first. Off: the running one stops and the new one starts."><Switch checked={f.switch_ask} onChange={v => setF({ ...f, switch_ask: v })} label="Ask before switching" /></SettingRow>
        <SettingRow title="Running time in the browser tab"><Switch checked={f.title_in_tab} onChange={v => setF({ ...f, title_in_tab: v })} label="Show in tab" /></SettingRow>
      </Section>
      <Section title="Reports and billing" icon="receipt">
        <SettingRow title="Round each entry up to" help="Only in reports and exports; the entries keep their exact length.">
          <Select ariaLabel="Rounding" value={String(f.rounding)} onChange={v => setF({ ...f, rounding: Number(v) })} options={[{ value: '0', label: 'Exact' }, { value: '1', label: '1 minute' }, { value: '5', label: '5 minutes' }, { value: '6', label: '6 minutes (0.1 h)' }, { value: '10', label: '10 minutes' }, { value: '15', label: '15 minutes' }, { value: '30', label: '30 minutes' }, { value: '60', label: '1 hour' }]} />
        </SettingRow>
        <SettingRow title="Currency" help="For project rates, e.g. EUR, BAM, USD."><input value={f.currency} onChange={e => setF({ ...f, currency: e.target.value.toUpperCase().slice(0, 3) })} className="set-time" placeholder="EUR" maxLength={3} /></SettingRow>
        <SettingRow title="Billable by default" help="For time without a project (a project has its own setting)."><Switch checked={f.default_billable} onChange={v => setF({ ...f, default_billable: v })} label="Billable by default" /></SettingRow>
      </Section>
      <Section title="Dates" icon="calendar">
        <SettingRow title="Week starts on"><Segmented size="sm" value={f.week_start} onChange={v => setF({ ...f, week_start: v })} options={[{ value: 1, label: 'Monday' }, { value: 6, label: 'Saturday' }, { value: 7, label: 'Sunday' }]} /></SettingRow>
        <SettingRow title="Date format"><Select ariaLabel="Date format" value={f.date_format} onChange={v => setF({ ...f, date_format: v as DateFormat })} options={DATE_FORMATS.map(d => ({ value: d.id, label: d.label }))} /></SettingRow>
        <SettingRow title="Time format"><Select ariaLabel="Time format" value={f.time_format} onChange={v => setF({ ...f, time_format: v as TimeFormat })} options={TIME_FORMATS.map(d => ({ value: d.id, label: d.label }))} /></SettingRow>
        {isAdmin && <SettingRow title="App title" help="Shown in the sidebar for everyone on this box."><input value={f.title} onChange={e => setF({ ...f, title: e.target.value })} /></SettingRow>}
      </Section>
      <div className="row"><button type="button" className="btn primary" onClick={() => save()} disabled={busy}>Save changes</button></div>
    </div>
  )
}

function Notifications() {
  const { f, setF, save, busy } = useSettingsForm()
  const { toast } = useToast()
  const { run, status } = useTester()
  if (!f) return <Loading />
  const weekly = async () => { try { const r = await api.post<{ ok: boolean; error?: string }>(`${V1}/notify/weekly`); if (r.ok) toast('Weekly report sent'); else toast(r.error || 'failed', 'err') } catch (e) { toast((e as Error).message, 'err') } }
  return (
    <div className="stack loose">
      <Section title="Push" icon="bell" actions={<span className={`badge ${f.notify_configured ? 'ok' : ''}`}>{f.notify_configured ? 'on' : 'not set'}</span>}
        sub={<>Where notifications go: an Apprise gateway (<code>…/notify/&lt;key&gt;</code>), an ntfy topic URL, or any webhook that takes JSON.</>}>
        <SettingRow title="URL" stack><input value={f.notify_url} onChange={e => setF({ ...f, notify_url: e.target.value })} placeholder="http://apprise:8000/notify/time" /></SettingRow>
        <SettingRow title="Format"><Segmented value={f.notify_format} onChange={v => setF({ ...f, notify_format: v })} options={[{ value: 'apprise', label: 'Apprise' }, { value: 'ntfy', label: 'ntfy' }, { value: 'json', label: 'JSON' }]} size="sm" /></SettingRow>
      </Section>
      <Section title="What to send" icon="clock">
        <SettingRow title="A timer is still running" help="Once per timer, after this many hours — in case you forgot to stop it (also for timers started in another app).">
          <div className="row"><Switch checked={f.notify_long} onChange={v => setF({ ...f, notify_long: v })} label="Still running" />{f.notify_long && <><input type="number" min={1} max={48} value={f.long_timer_h} onChange={e => setF({ ...f, long_timer_h: Number(e.target.value) })} className="set-time" aria-label="Hours" /><span className="muted small">hours</span></>}</div>
        </SettingRow>
        <SettingRow title="Weekly report" help="Last week’s total, by project and by app, and the billable time.">
          <div className="row wrap"><Switch checked={f.notify_weekly} onChange={v => setF({ ...f, notify_weekly: v })} label="Weekly report" />{f.notify_weekly && <><Select ariaLabel="Weekly day" value={String(f.weekly_day)} onChange={v => setF({ ...f, weekly_day: Number(v) })} options={DAYS.map(([d, l]) => ({ value: String(d), label: l }))} /><Select ariaLabel="Weekly hour" value={String(f.weekly_hour)} onChange={v => setF({ ...f, weekly_hour: Number(v) })} options={HOURS} /></>}</div>
        </SettingRow>
      </Section>
      <div className="row wrap">
        <button type="button" className="btn primary" onClick={() => save()} disabled={busy}>Save changes</button>
        <button type="button" className="btn" onClick={() => run('notify', f.notify_url, '', { format: f.notify_format })} disabled={!f.notify_url}><Icon name="mail" size={15} />Send a test</button>
        <button type="button" className="btn" onClick={weekly} disabled={!f.notify_url}><Icon name="chart" size={15} />Send the weekly report now</button>
        {status('notify')}
      </div>
    </div>
  )
}

const DIRECT_HELP: Record<string, [string, string]> = {
  projects: ['http://host.docker.internal:8196', 'Pocket Projects → Settings → API tokens'],
  guitar: ['http://host.docker.internal:8214', 'Pocket Guitar → Settings → API & shortcuts'],
  learn: ['http://host.docker.internal:8218', 'Pocket Learn → Settings → API & shortcuts'],
  still: ['http://host.docker.internal:8206', 'Pocket Still → Settings → API & shortcuts'],
  invoicing: ['http://host.docker.internal:8168', 'Pocket Invoicing → Settings → API tokens'],
}

function Integrations() {
  const { f, setF, save, busy } = useSettingsForm()
  const { run, status } = useTester()
  const { toast } = useToast()
  const { data: apps, reload, setData } = useAsync(() => api.get<AppInfo[]>(`${V1}/apps`), [])
  const [syncing, setSyncing] = useState(false)
  if (!f) return <Loading />
  const coreOn = f.core_configured && f.core_registered !== false
  const eff = (m: ConnMode): ConnMode => !coreOn && m === 'core' ? 'off' : m
  const sync = async () => { setSyncing(true); try { setData(await api.post<AppInfo[]>(`${V1}/apps/sync`)); toast('Brought in the latest time') } catch (e) { toast((e as Error).message, 'err') } finally { setSyncing(false) } }
  const str = (k: string) => String(f[k] ?? '')
  return (
    <div className="stack loose">
      <div className="callout info"><Icon name="info" size={15} /><div>
        Pocket Time is the one place for your timers. <b>Start a timer in Pocket Guitar, Projects or Learn and it runs here too</b> — stop it in either place. Time those apps keep themselves (a practice logged in Guitar, time on an issue) is shown here, read-only; time you track here for a song or an issue is logged in that app when the timer stops.
        <br /><br />These connections are yours. Through <b>Pocket Core</b> Time acts as you in your own Pocket apps with nothing to paste; <b>Direct</b> uses a URL and an API token you enter.{!coreOn && <CoreHint />}
      </div></div>
      {(apps || []).map(a => {
        const m = appMeta(a.slug)
        const mode = f[`${a.slug}_mode`] as ConnMode
        const help = DIRECT_HELP[a.slug] || ['', '']
        return (
          <Section key={a.slug} title={a.name} icon={m.icon} iconColor={m.color} actions={connBadge(eff(mode), f[`${a.slug}_via`] as ConnVia)} sub={a.reason}>
            <Connection app={a.name} mode={mode} via={str(`${a.slug}_via`)} detail={str(`${a.slug}_detail`)} core={coreOn} hasToken={!!f[`${a.slug}_token`]}
              onMode={async md => { await save({ [`${a.slug}_mode`]: md }); reload() }} onDisconnect={() => save({ [`${a.slug}_url`]: '', [`${a.slug}_token`]: '' })}
              test={<><button type="button" className="btn sm" onClick={() => run(a.slug, str(`${a.slug}_url`), str(`${a.slug}_token`))}>Test connection</button>{status(a.slug)}</>}
              direct={<div className="two-col tight">
                <Field label="URL" help="Inside Docker use http://host.docker.internal:PORT or the service name."><input value={str(`${a.slug}_url`)} onChange={e => setF({ ...f, [`${a.slug}_url`]: e.target.value })} placeholder={help[0]} /></Field>
                <Field label="API token" help={help[1]}><input value={str(`${a.slug}_token`)} onChange={e => setF({ ...f, [`${a.slug}_token`]: e.target.value })} autoComplete="off" /></Field>
              </div>} />
            {eff(mode) !== 'off' && <div className="mt">
              <SettingRow title={`Show ${a.name.replace('Pocket ', '')}’s own time here`} help={a.synced_at ? (a.sync_error ? <span className="bad">{a.sync_error}</span> : `${a.count} entries from the last ${f.collect_days} days · checked ${ago(a.synced_at)}`) : 'Checked every 15 minutes.'}>
                <Switch checked={!!f[`${a.slug}_collect`]} onChange={async v => { await save({ [`${a.slug}_collect`]: v }); reload() }} label="Collect" />
              </SettingRow>
            </div>}
          </Section>
        )
      })}
      <Section title="Bringing time in" icon="refresh">
        <SettingRow title="How far back" help="Days of the apps’ own time shown here."><input type="number" min={7} max={730} value={f.collect_days} onChange={e => setF({ ...f, collect_days: Number(e.target.value) })} className="set-time" /></SettingRow>
        <div className="row wrap"><button type="button" className="btn" onClick={sync} disabled={syncing}><Icon name="refresh" size={15} />{syncing ? 'Checking…' : 'Check now'}</button></div>
      </Section>
      <Section title="Apps that use your timer" icon="layers" sub="Time offers your timer to the rest of the box through Pocket Core. Switch these on in Pocket Core → Links for each app that should start and stop it (Guitar, Projects, Learn…). Pocket Day can read your tracked time for the journal.">
        <dl className="set-dl">
          <div><dt>time:read</dt><dd className="muted">See the running timer and your tracked time by day and project — never your notes.</dd></div>
          <div><dt>time:write</dt><dd className="muted">Start and stop your one timer, and log time done there; only what that app logged itself can be changed by it.</dd></div>
        </dl>
      </Section>
      <div className="row"><button type="button" className="btn primary" onClick={() => save()} disabled={busy}>Save changes</button></div>
    </div>
  )
}

function AITab() {
  const { f, setF, save, busy } = useSettingsForm()
  const { run, status } = useTester()
  const { data: ai } = useAsync(() => api.get<{ ready: boolean; provider: string; detail: string }>(`${V1}/ai/status`), [])
  if (!f) return <Loading />
  const coreOn = f.core_configured && f.core_registered !== false
  const shown = !coreOn && f.ai_provider === 'pocket' ? 'openai' : (f.ai_provider || 'auto')
  return (
    <div className="stack loose">
      <Section title="What the AI does" icon="sparkles" sub="Writes a short reflection on your practice in Statistics and answers questions, from your own log (what, when, how long, how you felt, your notes). It is not a therapist, and nothing is sent unless you press the button.">
        {ai && (ai.ready ? <p className="muted small row" style={{ gap: 6 }}><Icon name="sparkles" size={13} />{ai.provider === 'pocket' ? 'Pocket AI' : 'Your OpenAI key'} is ready.</p>
          : <div className="callout info"><Icon name="info" size={15} /><div>No AI connected{ai.detail ? ` (${ai.detail})` : ''}.{!coreOn && <CoreHint />}</div></div>)}
      </Section>
      <Section title="Provider" icon="sparkles">
        <div className="grid" style={{ gap: 12 }}>
          <Field group label="Who writes" help="Automatic prefers your enabled Pocket AI connection; without one it uses your OpenAI settings.">
            <Select ariaLabel="AI provider" value={shown} onChange={v => setF({ ...f, ai_provider: v as SettingsT['ai_provider'] })} options={[{ value: 'auto', label: coreOn ? 'Automatic — prefer Pocket AI' : 'Automatic' }, { value: 'pocket', label: 'Pocket AI', disabled: !coreOn, hint: coreOn ? undefined : 'needs Pocket Core' }, { value: 'openai', label: 'OpenAI / compatible API' }]} />
          </Field>
          {!coreOn && <CoreHint />}
          {coreOn && shown !== 'openai' && <PocketModelSelect value={f.ai_pocket_model || ''} onChange={v => setF({ ...f, ai_pocket_model: v })} />}
          {(shown === 'openai' || (!coreOn && shown === 'auto')) && <>
            <Field label="OpenAI API key" help="platform.openai.com → API keys. Stored on the server; shown masked."><input value={f.openai_api_key} onChange={e => setF({ ...f, openai_api_key: e.target.value })} autoComplete="off" placeholder="sk-…" /></Field>
            <div className="two-col tight">
              <Field label="Model" help="Default gpt-4o-mini."><input value={f.openai_model} onChange={e => setF({ ...f, openai_model: e.target.value })} placeholder="gpt-4o-mini" /></Field>
              <Field label="Base URL (optional)" help="Ollama, LM Studio, OpenRouter…"><input value={f.openai_base_url} onChange={e => setF({ ...f, openai_base_url: e.target.value })} placeholder="https://api.openai.com/v1" /></Field>
            </div>
          </>}
        </div>
      </Section>
      <div className="row wrap">
        <button type="button" className="btn primary" onClick={() => save()} disabled={busy}>Save changes</button>
        {(shown === 'openai' || (!coreOn && shown === 'auto')) && <><button type="button" className="btn" onClick={() => run('openai', f.openai_base_url, f.openai_api_key)} disabled={!f.openai_api_key}>Test the OpenAI key</button>{status('openai')}</>}
      </div>
    </div>
  )
}

function Connection({ app, mode: saved, via, detail, core, onMode, onDisconnect, hasToken, test, direct }: {
  app: string; mode: ConnMode; via: string; detail: string; core: boolean; onMode: (m: ConnMode) => void; onDisconnect: () => void
  hasToken: boolean; test: React.ReactNode; direct: React.ReactNode
}) {
  const [advanced, setAdvanced] = useState(saved === 'direct')
  const mode: ConnMode = !core && saved === 'core' ? 'off' : saved
  const ok = via !== '' && mode !== 'off'
  const showDirect = !core || mode === 'direct' || advanced
  const line = mode === 'off' ? `Off: Pocket Time does not talk to ${app}.`
    : mode === 'core' ? (ok ? `Connected through Pocket Core, as you${detail ? ` (${detail})` : ''}. Nothing to fill in; switch it off for yourself in Pocket Core → Settings → Apps acting for me.` : `Not connected: ${detail}.`)
    : (ok ? `Direct connection with your own ${app} URL and token; use Test connection to check it.` : `Not connected: ${detail || 'add the URL and a token'}.`)
  return (
    <div className="conn-block">
      <div className="conn-mode">
        <Segmented size="sm" value={mode} onChange={m => onMode(m as ConnMode)} options={[
          { value: 'core', label: 'Pocket Core', title: core ? 'Through Pocket Core: per person, nothing to paste' : 'Needs Pocket Core: there is none on this box', disabled: !core },
          ...(showDirect ? [{ value: 'direct', label: 'Direct', title: 'Your own URL and API token' }] : []),
          { value: 'off', label: 'Off' },
        ]} />
        {!showDirect && <button type="button" className="conn-advanced" onClick={() => setAdvanced(true)}>Advanced: connect directly with a URL and key</button>}
      </div>
      <div className={`conn-status ${mode === 'off' ? '' : ok ? 'ok' : 'bad'}`}><span className="dot" />{line}</div>
      {mode === 'direct' && <>
        {direct}
        <div className="row wrap">{test}{hasToken && <button type="button" className="btn sm ghost danger-text" onClick={onDisconnect} title="Remove the saved URL and token">Disconnect</button>}</div>
      </>}
      {mode === 'core' && <div className="row wrap">{test}</div>}
    </div>
  )
}

const connBadge = (mode: ConnMode, via: string) => mode === 'off' ? <span className="badge">off</span>
  : via ? <span className="badge ok">{via === 'core' ? 'Pocket Core' : 'direct'}</span>
  : <span className="badge warn">not connected</span>

function Users() {
  const { user, users, reloadMeta, status } = useApp()
  const { toast } = useToast()
  const [edit, setEdit] = useState<Partial<User> | null>(null)
  const [del, setDel] = useState<User | null>(null)
  const isAdmin = user?.role === 'admin'
  const run = async (fn: () => Promise<unknown>, msg: string) => { try { await fn(); await reloadMeta(); toast(msg) } catch (e) { toast((e as Error).message, 'err') } }
  return (
    <Section title="People on this box" actions={isAdmin && <button type="button" className="btn primary sm" onClick={() => setEdit({ role: 'member' })}><Icon name="plus" size={14} />Add person</button>}
      sub="Everyone signs in with their own account and has their own timer, entries, projects and connections. Admins manage accounts; they do not see anyone else’s time.">
      <div className="set-people">
        {users.map(u => (
          <div key={u.id} className="set-person">
            <Avatar name={u.name} email={u.email} size={34} />
            <div className="grow">
              <div className="set-person-name">{u.name || <span className="muted">—</span>}{u.id === user?.id && <span className="badge">you</span>}{u.role === 'admin' && <span className="badge accent">admin</span>}</div>
              <div className="muted small ellipsis">{u.email}</div>
            </div>
            {isAdmin && <span className="row" style={{ justifyContent: 'flex-end' }}><button type="button" className="btn ghost sm" onClick={() => setEdit(u)}>Edit</button>{u.id !== user?.id && <button type="button" className="btn ghost sm danger-text" onClick={() => setDel(u)}>Remove</button>}</span>}
          </div>
        ))}
      </div>
      {status?.auth_mode !== 'local' && <p className="muted small mt">Auth mode is <code>{status?.auth_mode}</code>: users are created automatically on first visit; passwords are not used here.</p>}
      {edit && <UserModal user={edit} local={status?.auth_mode === 'local'} onClose={() => setEdit(null)} onSaved={() => { setEdit(null); reloadMeta() }} />}
      {del && <Confirm confirmLabel="Remove" title={`Remove ${del.name || del.email}?`} message="Their account is deleted together with all their tracked time." onCancel={() => setDel(null)} onConfirm={() => { const d = del; setDel(null); run(() => api.del(`${V1}/users/${d.id}`), 'User removed') }} />}
    </Section>
  )
}

function UserModal({ user, local, onClose, onSaved }: { user: Partial<User>; local: boolean; onClose: () => void; onSaved: () => void }) {
  const { toast } = useToast()
  const [f, setF] = useState({ name: user.name || '', email: user.email || '', password: '', role: user.role || 'member' })
  const [busy, setBusy] = useState(false)
  const save = async (e: React.FormEvent) => {
    e.preventDefault(); setBusy(true)
    try { if (user.id) await api.put(`${V1}/users/${user.id}`, f); else await api.post(`${V1}/users`, f); toast(user.id ? 'User saved' : 'User added'); onSaved() } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  return (
    <Modal title={user.id ? 'Edit person' : 'Add a person'} onClose={onClose} size="sm">
      <form onSubmit={save} className="grid" style={{ gap: 14 }}>
        <Field label="Name"><input value={f.name} onChange={e => setF({ ...f, name: e.target.value })} autoFocus required maxLength={60} /></Field>
        <Field label="Email" help="Used to sign in."><input type="email" value={f.email} onChange={e => setF({ ...f, email: e.target.value })} required /></Field>
        {local && <Field label={user.id ? 'New password (leave empty to keep)' : 'Password'} help="At least 8 characters."><input type="password" value={f.password} onChange={e => setF({ ...f, password: e.target.value })} minLength={user.id ? 0 : 8} required={!user.id} autoComplete="new-password" /></Field>}
        <Field group label="Role"><Segmented value={f.role} onChange={v => setF({ ...f, role: v })} options={[{ value: 'member', label: 'Member' }, { value: 'admin', label: 'Admin' }]} /></Field>
        <div className="form-actions"><button type="button" className="btn" onClick={onClose}>Cancel</button><button className="btn primary" disabled={busy}>{user.id ? 'Save' : 'Add person'}</button></div>
      </form>
    </Modal>
  )
}

function Profile() {
  const { user, setUser, status } = useApp()
  const { toast } = useToast()
  const [f, setF] = useState({ name: user?.name || '', email: user?.email || '', current_password: '', new_password: '' })
  const [busy, setBusy] = useState(false)
  const save = async (e: React.FormEvent) => {
    e.preventDefault(); setBusy(true)
    try { setUser(await api.put(`${V1}/auth/me`, f)); setF(x => ({ ...x, current_password: '', new_password: '' })); toast('Profile saved') } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  return (
    <form onSubmit={save} className="stack loose">
      <div className="set-profile">
        <Avatar name={user?.name} email={user?.email} size={56} />
        <div className="grow"><h3>{user?.name || user?.email}</h3><p className="muted small">{user?.email} · {user?.role}</p></div>
      </div>
      <Section title="You">
        <div className="stack">
          <Field label="Name"><input value={f.name} onChange={e => setF({ ...f, name: e.target.value })} /></Field>
          <Field label="Email"><input type="email" value={f.email} onChange={e => setF({ ...f, email: e.target.value })} /></Field>
        </div>
      </Section>
      {status?.auth_mode === 'local' && <Section title="Password" sub="Leave empty to keep the current one."><div className="stack">
        <Field label="Current password"><input type="password" value={f.current_password} onChange={e => setF({ ...f, current_password: e.target.value })} autoComplete="current-password" /></Field>
        <Field label="New password" help="At least 8 characters."><input type="password" value={f.new_password} onChange={e => setF({ ...f, new_password: e.target.value })} autoComplete="new-password" /></Field>
      </div></Section>}
      <div className="row"><button className="btn primary" disabled={busy}>Save changes</button></div>
    </form>
  )
}

function Tokens() {
  const { toast } = useToast()
  const { settings } = useApp()
  const { data, reload } = useAsync(() => api.get<APIToken[]>(`${V1}/tokens`), [])
  const [name, setName] = useState('')
  const [fresh, setFresh] = useState('')
  const [del, setDel] = useState<APIToken | null>(null)
  const create = async (e: React.FormEvent) => {
    e.preventDefault()
    try { const r = await api.post<{ token: string }>(`${V1}/tokens`, { name: name || 'token' }); setFresh(r.token); setName(''); reload() } catch (e) { toast((e as Error).message, 'err') }
  }
  const base = settings?.base_url || window.location.origin
  return (
    <div className="stack loose">
      <Section title="Personal tokens" icon="key" sub={<>Use as <code>Authorization: Bearer &lt;token&gt;</code>. A token acts as you: it can read and change everything, feed a Homepage widget (<code>GET /api/v1/dashboard</code>) or start the timer from a Shortcut. See <code>docs/api.md</code>.</>}>
        <form onSubmit={create} className="row mb"><input value={name} onChange={e => setName(e.target.value)} placeholder="Token name (e.g. Homepage)" /><button className="btn primary"><Icon name="plus" size={15} />Create</button></form>
        {fresh && <div className="set-fresh mb"><div className="row"><Icon name="check-circle" size={16} /><b>Copy it now — it is shown once.</b></div><pre className="mono">{fresh}</pre></div>}
        {data && data.length > 0 ? (
          <table className="table compact">
            <thead><tr><th>Name</th><th>Prefix</th><th>Last used</th><th>Created</th><th /></tr></thead>
            <tbody>{data.map(t => <tr key={t.id}><td>{t.name}</td><td className="mono">{t.prefix}…</td><td className="muted">{t.last_used_at ? fmtDateTime(t.last_used_at) : 'never'}</td><td className="muted">{fmtDateTime(t.created_at)}</td><td className="right"><button type="button" className="btn ghost sm danger-text" onClick={() => setDel(t)}>Revoke</button></td></tr>)}</tbody>
          </table>
        ) : <p className="muted">No tokens yet.</p>}
        {del && <Confirm confirmLabel="Revoke" title={`Revoke “${del.name}”?`} message="Anything using this token stops working immediately." onCancel={() => setDel(null)} onConfirm={async () => { await api.del(`${V1}/tokens/${del.id}`); setDel(null); reload() }} />}
      </Section>
      <Section title="Endpoints" icon="code" sub={<>At <code>{base}/api/v1</code>.</>}>
        <dl className="set-dl">
          <div><dt><code>POST /timer/start</code></dt><dd className="muted"><code>{'{"title":"Writing","project":"Book"}'}</code> — start the timer (a Siri Shortcut, a desk button). Add <code>"switch":true</code> to stop a running one.</dd></div>
          <div><dt><code>POST /timer/stop</code></dt><dd className="muted">Stop whatever runs, wherever it was started.</dd></div>
          <div><dt><code>GET /timer</code></dt><dd className="muted">The running timer, or <code>null</code>.</dd></div>
          <div><dt><code>GET /dashboard</code></dt><dd className="muted">Today, this week, by project and app — for Homepage.</dd></div>
          <div><dt><code>GET /report?from=&to=&group=project</code></dt><dd className="muted">A period’s totals; <code>/report.csv</code> for a spreadsheet.</dd></div>
          <div><dt><code>GET /day/YYYY-MM-DD</code></dt><dd className="muted">One day’s time — what Pocket Day reads.</dd></div>
        </dl>
      </Section>
    </div>
  )
}

function Data() {
  const { toast } = useToast()
  const { user } = useApp()
  const [busy, setBusy] = useState('')
  const changed = () => window.dispatchEvent(new CustomEvent('ptm:changed'))
  const said = (r: Record<string, number>) => 'Imported: ' + (Object.entries(r).filter(([, v]) => v > 0).map(([k, v]) => `${v} ${k}`).join(', ') || 'nothing new')
  const importJSON = async (file: File) => {
    setBusy('json')
    try { toast(said(await api.post<Record<string, number>>(`${V1}/import`, JSON.parse(await file.text())))); changed() } catch (e) { toast((e as Error).message, 'err') } finally { setBusy('') }
  }
  const importCSV = async (file: File) => {
    setBusy('csv')
    try {
      const res = await fetch(`${V1}/import/csv`, { method: 'POST', credentials: 'same-origin', headers: { 'Content-Type': 'text/csv' }, body: await file.text() })
      const r = await res.json()
      if (!res.ok) throw new Error(r.error || res.statusText)
      toast(said(r)); changed()
    } catch (e) { toast((e as Error).message, 'err') } finally { setBusy('') }
  }
  return (
    <div className="stack loose">
      {user?.role === 'admin' && <Section title="Database backup" icon="database" sub="The whole box, every account — admins only.">
        <p className="muted">A consistent copy of the SQLite database (<code>VACUUM INTO</code>). Restore by stopping the app and replacing <code>pocket-time.db</code> in the data directory.</p>
        <a className="btn primary" href={`${V1}/backup`}><Icon name="download" size={15} />Download database</a>
      </Section>}
      <Section title="Export & import" icon="archive" sub="Your projects and entries as one JSON file, or every entry as a CSV. Time collected from other apps stays in those apps and is not in the JSON. Importing the same file twice adds nothing.">
        <div className="row wrap"><a className="btn" href={`${V1}/export.json`}><Icon name="download" size={15} />Export JSON</a><a className="btn" href={`${V1}/export.csv`}><Icon name="download" size={15} />Entries CSV</a><label className="btn"><Icon name="upload" size={15} />{busy === 'json' ? 'Importing…' : 'Import JSON…'}<input type="file" accept="application/json,.json" hidden onChange={e => e.target.files?.[0] && importJSON(e.target.files[0])} /></label></div>
      </Section>
      <Section title="Coming from another time tracker" icon="upload" sub="A CSV export from Toggl Track (Reports → Detailed → CSV), Clockify (Reports → Detailed → Export CSV), Kimai, Harvest or a spreadsheet with a date, a start and an end or a duration. Projects are matched by name and created when missing.">
        <label className="btn"><Icon name="upload" size={15} />{busy === 'csv' ? 'Importing…' : 'Import a CSV…'}<input type="file" accept="text/csv,.csv" hidden onChange={e => e.target.files?.[0] && importCSV(e.target.files[0])} /></label>
      </Section>
    </div>
  )
}

function About() {
  const { data, error } = useAsync(() => api.get<Record<string, unknown>>(`${V1}/settings/system`), [])
  const [keys, setKeys] = useState(false)
  if (error) return <div className="callout danger"><Icon name="alert" size={15} /><span>{error}</span></div>
  if (!data) return <Loading />
  const counts = (data.counts || {}) as Record<string, number>
  const core = (data.core || {}) as { configured?: boolean; registered?: boolean; url?: string; error?: string }
  const tiles: [string, number | undefined, string][] = [['Entries', counts.entries, 'clock'], ['Projects', counts.projects, 'tag'], ['Tokens', counts.api_tokens, 'key']]
  const rows: [string, React.ReactNode][] = [
    ['Version', <code key="v">{String(data.version)}</code>],
    ['Uptime', String(data.uptime)],
    ['Runtime', `${String(data.go)} · ${String(data.os)}/${String(data.arch)}`],
    ['Time zone', String(data.timezone)],
    ['Sign-in', String(data.auth_mode)],
    ['Pocket Core', core.configured ? (core.registered ? <span key="c" className="ok">connected · {core.url}</span> : <span key="c" className="bad">{core.error || 'not registered yet'}</span>) : <span key="c" className="muted">not configured</span>],
    ['Database', <span key="db"><code>{String(data.db_path)}</code> · {fmtBytes(Number(data.db_bytes))}</span>],
    ['People', `${counts.users ?? 0} accounts on this box`],
  ]
  return (
    <div className="stack loose">
      <div className="set-about-hero">
        <img src="/favicon.svg" alt="" width={48} height={48} />
        <div>
          <h3>Pocket Time</h3>
          <p className="muted small">One place for all your timers: start a timer here or in another Pocket app and it is the same timer everywhere; see all your tracked time together, by project and by app, and look back at your week. One binary, one SQLite file, one volume.</p>
        </div>
      </div>
      <div className="set-stats">
        {tiles.map(([label, n, icon]) => (
          <div key={label} className="set-stat"><Icon name={icon} size={16} /><span className="set-stat-n tnum">{(n ?? 0).toLocaleString()}</span><span className="set-stat-l">{label}</span></div>
        ))}
      </div>
      <Section title="This server">
        <dl className="set-dl">
          {rows.map(([k, v]) => <div key={k}><dt>{k}</dt><dd>{v}</dd></div>)}
        </dl>
      </Section>
      <div className="row wrap">
        <button type="button" className="btn" onClick={() => setKeys(true)}><Icon name="keyboard" size={15} />Keyboard shortcuts</button>
        <Link className="btn" to="/reports"><Icon name="chart" size={15} />Reports</Link>
        <a className="btn" href="https://github.com/abjelosevic88/pocket-homelab-time" target="_blank" rel="noreferrer"><Icon name="external" size={15} />Project on GitHub</a>
      </div>
      <p className="muted small">Open source (MIT). Part of the Pocket apps: Pocket Core connects them — Projects, Guitar, Learn, Day, Calendar and more.</p>
      {keys && <ShortcutsSheet onClose={() => setKeys(false)} />}
    </div>
  )
}
