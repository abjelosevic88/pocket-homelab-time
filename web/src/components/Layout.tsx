// The app shell: sidebar, top bar, search palette, shortcuts — the Pocket Calendar layout.
import { createContext, useCallback, useContext, useEffect, useRef, useState } from 'react'
import { Outlet, useLocation, useNavigate, useSearchParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import { DataProvider, useData } from '../lib/data'
import { TimerProvider, useTimer } from '../lib/timer'
import type { Entry } from '../lib/types'
import { Icon } from './icons'
import { Sidebar } from './Sidebar'
import { CommandPalette, type PaletteCommand } from './CommandPalette'
import { ShortcutsSheet } from './ShortcutsSheet'
import { EntryModal } from './Entries'
import { Avatar, type MenuItem, MenuButton, modalOpen, useIsMobile, useMediaQuery } from './ui'
import { AskButton } from './AskButton'

const SIDEBAR_KEY = 'ptm-sidebar'

/** The page's own name for the top bar breadcrumb. */
const CrumbCtx = createContext<(t: string) => void>(() => {})
export function useCrumb(title: string | undefined) {
  const set = useContext(CrumbCtx)
  useEffect(() => { set(title || ''); return () => set('') }, [title, set])
}

export default function Layout() {
  return <TimerProvider><DataProvider><Shell /></DataProvider></TimerProvider>
}

function Shell() {
  const navigate = useNavigate()
  const loc = useLocation()
  const [sp, setSp] = useSearchParams()
  const mobile = useIsMobile()
  const narrow = useMediaQuery('(max-width: 1099px)')
  const drawer = mobile || narrow
  const timer = useTimer()
  const { dashboard } = useData()
  const [sidebarOpen, setSidebarOpen] = useState(() => !drawer && localStorage.getItem(SIDEBAR_KEY) !== '0')
  useEffect(() => { setSidebarOpen(!drawer && localStorage.getItem(SIDEBAR_KEY) !== '0') }, [drawer])
  const drawerOpenedAt = useRef(0)
  const toggleSidebar = () => setSidebarOpen(o => { drawerOpenedAt.current = performance.now(); const n = !o; if (!drawer) localStorage.setItem(SIDEBAR_KEY, n ? '1' : '0'); return n })
  const closeDrawer = () => { if (drawer) setSidebarOpen(false) }
  useEffect(() => { document.querySelector('.content')?.scrollTo({ top: 0 }) }, [loc.pathname])

  const [crumb, setCrumbRaw] = useState('')
  const setCrumb = useCallback((t: string) => setCrumbRaw(t), [])
  const [palette, setPalette] = useState(false)
  const [shortcuts, setShortcuts] = useState(false)
  const [adding, setAdding] = useState(false)
  const [recent, setRecent] = useState<Entry[]>([])
  useEffect(() => { if (palette) api.get<Entry[]>(`${V1}/suggestions`).then(setRecent).catch(() => {}) }, [palette])

  // /?add=1 (the PWA shortcut) opens "add time"; /?start=1 starts a timer
  useEffect(() => {
    if (sp.get('add')) { setAdding(true); setSp(new URLSearchParams(), { replace: true }) }
    if (sp.get('start')) { setSp(new URLSearchParams(), { replace: true }); if (!timer.running) timer.start({}) }
  }, [timer.ready]) // eslint-disable-line react-hooks/exhaustive-deps

  /** Start or stop: stop the running timer, or start one (on the Timer page, focus what it is). */
  const toggle = () => {
    closeDrawer()
    if (timer.running) { timer.stop(); return }
    const input = document.querySelector<HTMLInputElement>('.timerbar .tb-title')
    if (input && input.value.trim() === '' && loc.pathname === '/') { input.focus(); return }
    timer.start(input && loc.pathname === '/' ? { title: input.value.trim() } : {})
  }
  const toggleRef = useRef(toggle)
  toggleRef.current = toggle

  useEffect(() => {
    const open = () => setShortcuts(true)
    window.addEventListener('ptm:shortcuts', open)
    return () => window.removeEventListener('ptm:shortcuts', open)
  }, [])
  useEffect(() => {
    const h = (e: KeyboardEvent) => {
      const t = e.target as HTMLElement
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') { e.preventDefault(); setPalette(p => !p); return }
      if (e.metaKey || e.ctrlKey || e.altKey) return
      if (t.closest('input, textarea, select, [contenteditable="true"]') || modalOpen() || document.querySelector('.menu, .popover, .sel-pop')) return
      const go = (to: string) => { e.preventDefault(); navigate(to) }
      switch (e.key) {
        case '/': e.preventDefault(); setPalette(true); break
        case '?': e.preventDefault(); setShortcuts(true); break
        case 's': e.preventDefault(); toggleRef.current(); break
        case 'n': e.preventDefault(); setAdding(true); break
        case 't': case 'h': go('/'); break
        case 'e': go('/entries'); break
        case 'r': go('/reports'); break
        case 'p': go('/projects'); break
        case ',': go('/settings'); break
        case '[': e.preventDefault(); toggleSidebar(); break
      }
    }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  }) // eslint-disable-line react-hooks/exhaustive-deps

  const commands: PaletteCommand[] = [
    timer.running ? { id: 'stop', label: 'Stop the timer', icon: 'stop', hint: 'S', keywords: 'end finish', run: () => timer.stop() }
      : { id: 'start', label: 'Start a timer', icon: 'play', hint: 'S', keywords: 'begin track', run: () => timer.start({}) },
    { id: 'add', label: 'Add time by hand', icon: 'plus', hint: 'N', keywords: 'log manual entry', run: () => setAdding(true) },
    { id: 'timer', label: 'Timer', icon: 'timer', hint: 'T', run: () => navigate('/') },
    { id: 'entries', label: 'Entries', icon: 'list', hint: 'E', keywords: 'history log week', run: () => navigate('/entries') },
    { id: 'reports', label: 'Reports', icon: 'chart', hint: 'R', keywords: 'stats summary billable csv', run: () => navigate('/reports') },
    { id: 'projects', label: 'Projects', icon: 'tag', hint: 'P', keywords: 'clients rates', run: () => navigate('/projects') },
    { id: 'settings', label: 'Settings', icon: 'settings', hint: ',', run: () => navigate('/settings') },
    { id: 'apps', label: 'Settings: connected apps', icon: 'layers', keywords: 'guitar projects learn still invoicing core integrations', run: () => navigate('/settings/apps') },
    { id: 'shortcuts', label: 'Keyboard shortcuts', icon: 'keyboard', hint: '?', run: () => setShortcuts(true) },
  ]

  return (
    <CrumbCtx.Provider value={setCrumb}>
      <div className={`app ${sidebarOpen ? 'with-sidebar' : 'no-sidebar'} ${mobile ? 'is-mobile' : ''} ${drawer ? 'drawer-mode' : ''}`}>
        {drawer && sidebarOpen && <div className="drawer-backdrop" onClick={e => { if (e.timeStamp - drawerOpenedAt.current > 400) setSidebarOpen(false) }} />}
        <div className={`sidebar-wrap ${sidebarOpen ? 'open' : ''}`}>
          <Sidebar onNavigate={closeDrawer} onToggle={toggle} />
        </div>
        <div className="main">
          <TopBar onMenu={toggleSidebar} onPalette={() => setPalette(true)} mobile={mobile} compact={narrow} sidebarOpen={sidebarOpen} crumb={crumb} />
          <main className="content"><div className="content-inner"><Outlet /></div></main>
        </div>
        {mobile && loc.pathname !== '/' && <button type="button" className={`fab ${timer.running ? 'is-running' : ''}`} aria-label={timer.running ? 'Stop the timer' : 'Start a timer'} title={timer.running ? 'Stop' : 'Start'} disabled={timer.busy} onClick={toggle}>
          <Icon name={timer.running ? 'stop' : 'play'} size={24} strokeWidth={2.2} />
        </button>}
        {palette && <CommandPalette onClose={() => setPalette(false)} commands={commands} recent={recent.length ? recent : dashboard?.recent.slice(0, 8) || []} onStart={e => timer.continueEntry(e)} />}
        {shortcuts && <ShortcutsSheet onClose={() => setShortcuts(false)} />}
        {adding && <EntryModal onClose={() => setAdding(false)} />}
      </div>
    </CrumbCtx.Provider>
  )
}

const SECTION: Record<string, [string, string]> = { '': ['Timer', '/'], entries: ['Entries', '/entries'], reports: ['Reports', '/reports'], projects: ['Projects', '/projects'] }

function TopBar({ onMenu, onPalette, mobile, compact, sidebarOpen, crumb }: { onMenu: () => void; onPalette: () => void; mobile: boolean; compact: boolean; sidebarOpen: boolean; crumb: string }) {
  const { user, themeMode, setThemeMode, status, setUser } = useApp()
  const navigate = useNavigate()
  const loc = useLocation()
  const [section, sectionTo] = SECTION[loc.pathname.split('/')[1] || ''] || ['', '/']
  const signOut = async () => { await api.post(`${V1}/auth/logout`).catch(() => {}); setUser(null); navigate('/login') }
  const avatarMenu = (): MenuItem[] => [
    { heading: user?.name || user?.email || '' },
    { label: 'Profile', icon: 'user', onSelect: () => navigate('/settings/profile') },
    { label: 'Settings', icon: 'settings', onSelect: () => navigate('/settings'), hint: ',' },
    { label: 'Keyboard shortcuts', icon: 'keyboard', onSelect: () => window.dispatchEvent(new CustomEvent('ptm:shortcuts')), hint: '?' },
    'sep',
    { heading: 'Appearance' },
    { label: 'System', checked: themeMode === 'system', onSelect: () => setThemeMode('system') },
    { label: 'Light', checked: themeMode === 'light', onSelect: () => setThemeMode('light') },
    { label: 'Dark', checked: themeMode === 'dark', onSelect: () => setThemeMode('dark') },
    ...(status?.auth_mode === 'local' ? ['sep' as const, { label: 'Sign out', icon: 'log-out', onSelect: signOut }] : []),
  ]
  return (
    <header className="topbar">
      <button type="button" className="btn ghost icon-only" onClick={onMenu} aria-label={sidebarOpen ? 'Hide the sidebar' : 'Show the sidebar'} title="Menu ([)"><Icon name={mobile || compact ? 'menu' : 'sidebar'} size={18} /></button>
      <h1 className="range-title">
        {crumb ? <><button type="button" className="crumb" onClick={() => navigate(sectionTo)}>{section}</button><Icon name="chevron-right" size={16} className="crumb-sep" /><span className="ellipsis">{crumb}</span></> : section}
      </h1>
      <span className="spacer" />
      <button type="button" className="btn ghost icon-only" onClick={onPalette} aria-label="Search" title="Search (⌘K or /)"><Icon name="search" size={18} /></button>
      <AskButton />
      <MenuButton label="Account" className="avatar-btn" align="right" width={220} items={avatarMenu}>
        <Avatar name={user?.name} email={user?.email} size={30} />
      </MenuButton>
    </header>
  )
}
