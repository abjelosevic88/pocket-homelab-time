// What most pages need: your projects, the connected apps, this week's numbers.
import React, { createContext, useCallback, useContext, useEffect, useState } from 'react'
import { api, V1 } from './api'
import type { AppInfo, Dashboard, Project } from './types'
import { useOnChange } from './timer'

interface DataState {
  projects: Project[]
  apps: AppInfo[]
  dashboard: Dashboard | null
  reload: () => void
  reloadApps: () => void
}
const Ctx = createContext<DataState>({ projects: [], apps: [], dashboard: null, reload: () => {}, reloadApps: () => {} })
export const useData = () => useContext(Ctx)

export function DataProvider({ children }: { children: React.ReactNode }) {
  const [projects, setProjects] = useState<Project[]>([])
  const [apps, setApps] = useState<AppInfo[]>([])
  const [dashboard, setDashboard] = useState<Dashboard | null>(null)
  const reload = useCallback(() => {
    api.get<Project[]>(`${V1}/projects?archived=1`).then(setProjects).catch(() => {})
    api.get<Dashboard>(`${V1}/dashboard`).then(setDashboard).catch(() => {})
  }, [])
  const reloadApps = useCallback(() => { api.get<AppInfo[]>(`${V1}/apps`).then(setApps).catch(() => {}) }, [])
  useEffect(() => { reload(); reloadApps() }, [reload, reloadApps])
  useOnChange(reload)
  return <Ctx.Provider value={{ projects, apps, dashboard, reload, reloadApps }}>{children}</Ctx.Provider>
}
