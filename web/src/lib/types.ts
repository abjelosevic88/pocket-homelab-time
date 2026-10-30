export interface User { id: number; email: string; name: string; role: 'admin' | 'member'; created_at: string }
export interface APIToken { id: number; user_id: number; name: string; prefix: string; last_used_at: string | null; created_at: string }

export type ConnMode = 'core' | 'direct' | 'off'
export type ConnVia = '' | 'core' | 'direct'

export interface Settings {
  core_configured: boolean
  core_registered?: boolean
  title: string; date_format: string; time_format: string; week_start: number
  daily_goal_min: number; weekly_goal_min: number; rounding: number; currency: string; default_billable: boolean
  switch_ask: boolean; long_timer_h: number; title_in_tab: boolean; collect_days: number
  notify_long: boolean; notify_weekly: boolean; weekly_day: number; weekly_hour: number
  notify_url: string; notify_format: string; notify_configured: boolean
  ai_provider: 'auto' | 'pocket' | 'openai'; ai_pocket_model: string; openai_api_key: string; openai_model: string; openai_base_url: string
  setup_complete: boolean; base_url: string
  // per sibling app: <slug>_mode, _via, _detail, _configured, _url, _token, _public_url, _collect, _open_url
  [k: string]: unknown
}

export interface Entry {
  id: number; title: string; note: string; project_id: number | null; tags: string; billable: boolean
  started_at: string; ended_at: string; date: string; duration_s: number; running: boolean
  source: string; app: string; app_ref: string; app_label: string; app_url: string
  collected: boolean; external_id: string
  deliver: boolean; delivered_at: string; deliver_error: string; remote_id: string
  created_at: string; updated_at: string
  project_name?: string; project_color?: string
}

export interface Project {
  id: number; name: string; color: string; client: string; billable: boolean; rate: number; goal_min: number
  archived: boolean; sort: number; created_at: string; updated_at: string
}

export interface Group { key: string; label: string; color?: string; app?: string; seconds: number; billable_s: number; amount: number; count: number; goal_min?: number }
export interface DayTotal { date: string; seconds: number; by: Record<string, number> }
export interface Report {
  from: string; to: string; group: string; seconds: number; billable_s: number; amount: number; currency: string; count: number
  days: DayTotal[]; groups: Group[]; rounding: number
}

export interface Dashboard {
  today: string; week_from: string; week_to: string; today_s: number; week_s: number
  daily_goal_min: number; weekly_goal_min: number
  days: DayTotal[]; projects: Group[]; apps: Group[]
  running: Entry | null; recent: Entry[]
}

export interface AppInfo {
  slug: string; name: string; item_name: string; reason: string
  mode: ConnMode; via: ConnVia; detail: string; connected: boolean; collect: boolean
  synced_at: string; sync_error: string; count: number; open_url: string
}

export interface Item { ref: string; label: string; detail?: string; url?: string }

/** What a timer or an entry is for in another app. */
export interface ForTarget { app: string; app_ref: string; app_label: string; app_url: string }
