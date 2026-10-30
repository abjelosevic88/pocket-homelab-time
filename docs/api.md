# API

JSON over HTTP under `/api/v1`. Authenticate with the session cookie, a personal token (`Authorization: Bearer ptm_…`, Settings → API tokens), a Pocket Core token, or a Pocket Core link token (`pcx_…`, only the routes of its scope). Everything is per person: other people's ids answer 404.

## The timer

One timer per person, shared with the other Pocket apps (see [worklog.md](worklog.md)).

| | |
|---|---|
| `GET /timer` | `{running: Entry \| null, now}` |
| `POST /timer/start` | Start: `{title, project_id \| project, billable, tags, note, app, app_ref, app_label, app_url, deliver, started_at, switch}` — all optional. While another timer runs: `409 {error, running}`, unless `switch: true` (then the running one stops at the new start and comes back as `stopped`) |
| `POST /timer/stop` | `{ended_at?, note?, only_if?}`; `only_if` = the entry you think is running (another one → 409) |
| `PUT /timer` | Change the running timer (any subset; `started_at` too) |
| `POST /timer/discard` | Throw the running timer away |

```sh
curl -X POST -H "Authorization: Bearer ptm_…" -d '{"title":"Writing","project":"Book"}' https://time.example.com/api/v1/timer/start
curl -X POST -H "Authorization: Bearer ptm_…" https://time.example.com/api/v1/timer/stop
```

## Entries

| | |
|---|---|
| `GET /entries?from&to&project_id (or none)&app (or time = only here)&source&collected=0\|1&q&limit&offset` | Newest first, `{entries, total}` |
| `POST /entries` | Log finished time: `started_at` plus `ended_at` or `duration_s`, and the fields above. A linked app's `external_id` makes it idempotent |
| `GET/PUT/DELETE /entries/{id}` | One entry; PUT takes any subset. Collected entries (kept in another app) answer 409: change them there. `DELETE …?everywhere=1` also removes what it became in its app |
| `POST /entries/{id}/continue` | Start a timer for the same thing (`{switch}` as above) |
| `POST /entries/{id}/resend` | Send it to its app again now |
| `GET /suggestions` | Recent distinct things you timed |

An entry: `id, title, note, project_id, project_name, project_color, tags, billable, started_at, ended_at ("" while running), date, duration_s, running, source (time | manual | import | <app slug that started it>), app, app_ref, app_label, app_url, collected, deliver, delivered_at, deliver_error, remote_id`.

## Projects

`GET /projects?archived=1`, `POST /projects`, `PUT/DELETE /projects/{id}` — `{name, color, client, billable, rate, goal_min, archived, sort}`. Deleting keeps the entries, without a project.

## Reports

| | |
|---|---|
| `GET /dashboard` | Today, this week (by day, project and app), the running timer and the last seven days of entries — for Homepage |
| `GET /report?from&to&group=project\|app\|item\|tag\|title&project_id&app` | `{seconds, billable_s, amount, currency, count, days[{date, seconds, by}], groups[{key, label, color, seconds, billable_s, amount, count}]}`; entries rounded up to the person's rounding |
| `GET /report.csv?from&to` | The period's entries as CSV |
| `GET /day/YYYY-MM-DD` (also `/summary/…`) | One day: total, a one-line summary, by project and app, the entries (without notes) — what Pocket Day reads |

## The other Pocket apps

| | |
|---|---|
| `GET /apps` | Per app: how you reach it (`mode`, `via`, `detail`), whether its time is collected, the last check |
| `POST /apps/sync?app=` | Collect now (and send what is waiting) |
| `GET /apps/{slug}/items?q=` | What time can be for there (songs, issues, decks…) |

## Pocket Core link scopes

| Scope | Routes |
|---|---|
| `time:read` | `GET /timer`, `/dashboard`, `/entries`, `/entries/{id}`, `/projects`, `/report`, `/day/{date}`, `/summary/{date}` — never notes |
| `time:write` | the reads above (except day/summary), `POST /timer/start`, `POST /timer/stop`, `POST /entries`, `PUT/DELETE /entries/{id}` (only what that app logged) |

Events: `time.timer_started`, `time.timer_stopped`, `time.entry_logged` — payload `{entry_id, title, project, app, app_ref, source, started_at, ended_at, date, duration_s, minutes, user_email}`.

## Settings, data and the rest

`GET/PUT /settings` (per person; `<app>_mode|_url|_token|_public_url|_collect` per sibling app), `GET /settings/system`, `POST /integrations/test {which}`, `POST /notify/test`, `POST /notify/weekly`, `GET /export.json`, `GET /export.csv`, `POST /import` (a Pocket Time export), `POST /import/csv` (Toggl, Clockify, Kimai, Harvest CSV as the body), `GET /backup` (admins), `GET/POST/DELETE /tokens`, `GET/POST/PUT/DELETE /users`, `POST /ai/summary {from, to}`, `POST /ai/ask {question, days}`, `GET /ai/status`. Health: `GET /healthz`, `/readyz`, `/metrics`.
