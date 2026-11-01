// Small helpers for durations, apps and dates.
import type { Entry, Settings } from './types'

/** The sibling apps Pocket Time brings together, and Pocket Time itself. */
export const APPS: Record<string, { name: string; icon: string; color: string; item: string }> = {
  time: { name: 'Pocket Time', icon: 'timer', color: 'var(--accent)', item: '' },
  projects: { name: 'Pocket Projects', icon: 'check-square', color: '#5e6ad2', item: 'issue' },
  guitar: { name: 'Pocket Guitar', icon: 'music', color: '#4338ca', item: 'song' },
  learn: { name: 'Pocket Learn', icon: 'brain', color: '#4263eb', item: 'deck' },
  still: { name: 'Pocket Still', icon: 'lotus', color: '#3f7f6b', item: 'practice' },
  invoicing: { name: 'Pocket Invoicing', icon: 'receipt', color: '#2563eb', item: 'client' },
}
export const APP_ORDER = ['projects', 'guitar', 'learn', 'still', 'invoicing']
export const appMeta = (slug: string) => APPS[slug || 'time'] || { name: 'Pocket ' + slug.charAt(0).toUpperCase() + slug.slice(1), icon: 'layers', color: 'var(--muted)', item: 'item' }

/** "1:02:03" or "12:03". */
export function fmtHMS(sec: number): string {
  sec = Math.max(0, Math.floor(sec))
  const h = Math.floor(sec / 3600), m = Math.floor(sec / 60) % 60, s = sec % 60
  const mm = String(m).padStart(2, '0'), ss = String(s).padStart(2, '0')
  return h > 0 ? `${h}:${mm}:${ss}` : `${m}:${ss}`
}

/** "2 h 05 min", "35 min", "0 min". */
export function fmtDur(sec: number): string {
  const m = Math.round(Math.max(0, sec) / 60)
  if (m < 60) return `${m} min`
  if (m % 60 === 0) return `${m / 60} h`
  return `${Math.floor(m / 60)} h ${String(m % 60).padStart(2, '0')} min`
}

/** "2:05" (hours:minutes), for tight columns. */
export function fmtHM(sec: number): string {
  const m = Math.round(Math.max(0, sec) / 60)
  return `${Math.floor(m / 60)}:${String(m % 60).padStart(2, '0')}`
}

/** "12.5 h" */
export const fmtHours = (sec: number) => `${(Math.round(sec / 360) / 10).toLocaleString()} h`

export function fmtMoney(v: number, currency: string): string {
  if (!currency) return v.toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 })
  try { return v.toLocaleString(undefined, { style: 'currency', currency }) } catch { return `${v.toFixed(2)} ${currency}` }
}

/** Seconds an entry has run until now (live for a running one). */
export function elapsed(e: Entry, now = Date.now()): number {
  if (!e.running) return e.duration_s
  return Math.max(0, Math.floor((now - new Date(e.started_at).getTime()) / 1000))
}

/** Where to open what an entry was for, in its app. */
export function openURL(e: { app: string; app_url: string }, settings: Settings | null): string {
  if (!e.app || !e.app_url) return ''
  if (/^https?:\/\//.test(e.app_url)) return e.app_url
  const base = String(settings?.[`${e.app}_open_url`] || '')
  return base ? base.replace(/\/$/, '') + e.app_url : ''
}

/** A "YYYY-MM-DDTHH:MM" for datetime-local inputs, in local time. */
export function localInput(iso: string | Date): string {
  const d = typeof iso === 'string' ? new Date(iso) : iso
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}T${p(d.getHours())}:${p(d.getMinutes())}`
}

/** The first day of the week holding date (week_start 1 = Monday, 7 = Sunday, 6 = Saturday). */
export function weekStartOf(date: string, ws: number): string {
  const d = new Date(date + 'T12:00:00')
  const first = ws === 7 ? 0 : ws === 6 ? 6 : 1
  const back = (d.getDay() - first + 7) % 7
  d.setDate(d.getDate() - back)
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}`
}

/** A short name for what an entry is. */
export const entryName = (e: Pick<Entry, 'title' | 'project_name' | 'app' | 'app_label'>) => e.title || e.app_label || e.project_name || (e.app ? appMeta(e.app).name : 'Untitled')

/** Where the entry came from, in words: "Started in Pocket Guitar", "Kept in Pocket Still". */
export function originText(e: Entry): string {
  if (e.collected) return `Logged in ${appMeta(e.app).name}`
  if (e.source && e.source !== 'time' && e.source !== 'manual' && e.source !== 'import') return `Started in ${appMeta(e.source).name}`
  if (e.source === 'manual') return 'Added by hand'
  if (e.source === 'import') return 'Imported'
  return ''
}
