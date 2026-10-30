// The one running timer, shared by every page (and by the other Pocket apps,
// which start and stop the same timer through Pocket Core). Polled while the
// page is visible, so a timer started in Pocket Guitar shows up here.
import React, { createContext, useCallback, useContext, useEffect, useRef, useState } from 'react'
import { api, ApiError, V1 } from './api'
import { useApp } from './app-context'
import type { Entry } from './types'
import { elapsed, entryName, fmtHMS, appMeta } from './time'
import { Confirm, useToast } from '../components/ui'

export type StartInput = Partial<Pick<Entry, 'title' | 'note' | 'tags' | 'project_id' | 'billable' | 'app' | 'app_ref' | 'app_label' | 'app_url' | 'deliver'>> & { started_at?: string }

interface TimerState {
  running: Entry | null
  ready: boolean
  busy: boolean
  start: (input: StartInput) => Promise<Entry | null>
  continueEntry: (e: Entry) => Promise<Entry | null>
  stop: () => Promise<Entry | null>
  discard: () => Promise<void>
  update: (patch: StartInput) => Promise<void>
  refresh: () => void
}
const Ctx = createContext<TimerState>(null!)
export const useTimer = () => useContext(Ctx)

/** Something changed (a timer, an entry): pages reload their lists. */
export const announceChange = () => window.dispatchEvent(new CustomEvent('ptm:changed'))
export function useOnChange(cb: () => void) {
  const ref = useRef(cb)
  ref.current = cb
  useEffect(() => {
    const h = () => ref.current()
    window.addEventListener('ptm:changed', h)
    return () => window.removeEventListener('ptm:changed', h)
  }, [])
}

/** Re-renders every second while on. */
export function useTick(on: boolean) {
  const [, set] = useState(0)
  useEffect(() => {
    if (!on) return
    const t = setInterval(() => set(n => n + 1), 1000)
    return () => clearInterval(t)
  }, [on])
}

type Pending = { running: Entry; run: () => Promise<Entry | null>; next: string; resolve: (e: Entry | null) => void }

export function TimerProvider({ children }: { children: React.ReactNode }) {
  const { settings } = useApp()
  const { toast } = useToast()
  const [running, setRunning] = useState<Entry | null>(null)
  const [ready, setReady] = useState(false)
  const [busy, setBusy] = useState(false)
  const [pending, setPending] = useState<Pending | null>(null)
  const last = useRef<string>('')

  const refresh = useCallback(() => {
    api.get<{ running: Entry | null }>(`${V1}/timer`).then(r => {
      const key = r.running ? `${r.running.id}|${r.running.updated_at}` : ''
      if (key !== last.current) {
        // started or stopped somewhere else: the lists follow
        if (last.current !== '' || key !== '') announceChange()
        last.current = key
        setRunning(r.running)
      }
      setReady(true)
    }).catch(() => setReady(true))
  }, [])

  useEffect(() => {
    refresh()
    const t = setInterval(() => { if (document.visibilityState === 'visible') refresh() }, 15000)
    const vis = () => { if (document.visibilityState === 'visible') refresh() }
    document.addEventListener('visibilitychange', vis)
    window.addEventListener('focus', vis)
    return () => { clearInterval(t); document.removeEventListener('visibilitychange', vis); window.removeEventListener('focus', vis) }
  }, [refresh])

  const apply = (r: { running: Entry | null; stopped?: Entry | null }) => {
    last.current = r.running ? `${r.running.id}|${r.running.updated_at}` : ''
    setRunning(r.running)
    announceChange()
  }

  // Start (or continue): a 409 means another timer runs; ask before switching
  // when the person wants to be asked, else switch.
  const withSwitch = useCallback(async (call: (sw: boolean) => Promise<{ running: Entry | null; stopped?: Entry | null }>, next: string): Promise<Entry | null> => {
    setBusy(true)
    try {
      const r = await call(false)
      apply(r)
      return r.running
    } catch (e) {
      if (e instanceof ApiError && e.status === 409 && e.data.running) {
        const cur = e.data.running as Entry
        const go = async () => { const r = await call(true); apply(r); return r.running }
        if (settings?.switch_ask === false) return await go()
        return await new Promise<Entry | null>(resolve => setPending({ running: cur, run: go, next, resolve }))
      }
      toast((e as Error).message, 'err')
      return null
    } finally { setBusy(false) }
  }, [settings?.switch_ask, toast])

  const start = useCallback((input: StartInput) => withSwitch(sw => api.post(`${V1}/timer/start`, { ...input, switch: sw }), entryName({ title: input.title || '', app: input.app || '', app_label: input.app_label || '' })), [withSwitch])
  const continueEntry = useCallback((e: Entry) => withSwitch(sw => api.post(`${V1}/entries/${e.id}/continue`, { switch: sw }), entryName(e)), [withSwitch])

  const stop = useCallback(async () => {
    setBusy(true)
    try {
      const r = await api.post<{ running: null; stopped: Entry | null }>(`${V1}/timer/stop`, running ? { only_if: running.id } : {})
      apply(r)
      if (r.stopped && r.stopped.deliver && r.stopped.app) toast(`Saved, and logged in ${appMeta(r.stopped.app).name}`)
      return r.stopped
    } catch (e) {
      if (e instanceof ApiError && e.status === 409) { refresh(); toast('Another timer was started meanwhile; it is still running.', 'err') } else toast((e as Error).message, 'err')
      return null
    } finally { setBusy(false) }
  }, [running, refresh, toast])

  const discard = useCallback(async () => {
    setBusy(true)
    try { apply(await api.post(`${V1}/timer/discard`)) } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }, [toast])

  const update = useCallback(async (patch: StartInput) => {
    try { apply(await api.put(`${V1}/timer`, patch)) } catch (e) { toast((e as Error).message, 'err') }
  }, [toast])

  // the running time in the browser tab
  const showInTab = settings?.title_in_tab !== false
  useTick(!!running && showInTab)
  useEffect(() => {
    const base = settings?.title && settings.title !== 'Pocket Time' ? settings.title : 'Pocket Time'
    document.title = running && showInTab ? `${fmtHMS(elapsed(running))} · ${entryName(running)}` : base
  })

  const close = (v: Entry | null) => { pending?.resolve(v); setPending(null) }
  return (
    <Ctx.Provider value={{ running, ready, busy, start, continueEntry, stop, discard, update, refresh }}>
      {children}
      {pending && <Confirm danger={false} confirmLabel="Stop it and start" title="Switch timers?"
        message={<>“{entryName(pending.running)}” is running{pending.running.source !== 'time' && pending.running.source !== 'manual' ? <> (started in {appMeta(pending.running.source).name})</> : null}, for {fmtHMS(elapsed(pending.running))}. Stop it and start “{pending.next}”? You can turn this question off in Settings.</>}
        onCancel={() => close(null)} onConfirm={async () => { const p = pending; setPending(null); try { p.resolve(await p.run()) } catch (e) { toast((e as Error).message, 'err'); p.resolve(null) } }} />}
    </Ctx.Provider>
  )
}
