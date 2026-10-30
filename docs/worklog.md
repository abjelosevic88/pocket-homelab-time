# Pocket apps and Pocket Time

Pocket Time is the one place for a person's timers. Two things connect a Pocket app to it, both through Pocket Core app links (per person, nothing to paste):

1. **The shared timer.** The app's own timer button starts and stops the person's *one* timer in Pocket Time (scope `time:write` on Time, declared in the app's `uses`). Start it in Pocket Guitar, open Pocket Time: it runs there. Stop it in either place: both show it stopped.
2. **Its own time, both ways.** Pocket Time shows the time the app keeps itself (read-only), and writes time tracked in Pocket Time for something in the app back into it. Pocket Guitar and Pocket Still do this with their practice APIs (`guitar:read|write`, `mindfulness:read|write`); every other app implements the small **worklog** contract below and offers `worklog:read` / `worklog:write`.

Nothing is counted twice: what Time writes into an app carries `external_id: "time:<entry id>"` (and, on the app side, `source` = `time`), and Time skips those when it reads the app's time.

## 1. The shared timer (an app using Pocket Time)

Declare in the manifest: `core.Use{App: "time", Scopes: []string{"time:read", "time:write"}, Reason: "Your timer here is the same timer as in Pocket Time"}`. Get a token per call with `ActAs(ctx, email, "time", "time:write")`.

| Call | Body | Answer |
|---|---|---|
| `GET /api/v1/timer` | – | `{"running": Entry \| null, "now": "…"}` |
| `POST /api/v1/timer/start` | `{"app_ref": "tab:42", "app_label": "Wonderwall", "app_url": "/tab/42", "title"?, "project"?, "deliver"?: true, "switch"?: false}` | `200 {"running": Entry, "stopped": Entry \| null}` or **`409 {"error", "running": Entry}`** when another timer runs and `switch` is not set |
| `POST /api/v1/timer/stop` | `{"only_if"?: <entry id>, "note"?: "…"}` | `{"running": null, "stopped": Entry \| null}`; `409` if `only_if` is not the one running |

- `app` and `source` of a timer an app starts are always that app's slug; `app_ref` is the app's own id for the item (`tab:42`, `issue:ATH-12`, `deck:3`), `app_url` a path in the app (Time prefixes the app's public URL).
- **One timer at a time.** On `409`, show the running one (`running.app`/`running.source` say where it was started; `entryName` = `title` or `app_label`) and offer "Stop it and start this" → the same start with `"switch": true`. That stops the running one at the new start.
- Send `only_if` with the id you started, so pressing stop in your app never stops a different timer started meanwhile.
- **Who records the time:** by default (`deliver: true`) Pocket Time writes the finished time back into the app through the app's own write scope (Guitar `POST /practice`, Still `POST /practices`, others `POST /worklog`) — whether it was stopped in the app or in Time. So the app should NOT also save the session itself, or it would be there twice. Send `"deliver": false` if the app records it itself (then give the record `external_id: "time:<entry id>"`, so Time does not collect it again).
- Show a running timer the app did not start (poll `GET /timer` every 15–30 s while visible, or on focus): "Timer running in Pocket Time: X — Stop".
- Without Core or with the link off, the app's timer works as before, on its own.

## 2. The worklog contract (an app offering its time)

Offer `worklog:read` ("See time logged here") and `worklog:write` ("Log time here"), with link routes:

```
GET    ^/api/v1/worklog$              worklog:read
GET    ^/api/v1/worklog/items$        worklog:read
POST   ^/api/v1/worklog$              worklog:write
DELETE ^/api/v1/worklog/[^/]+$        worklog:write
```

### `GET /api/v1/worklog?from=YYYY-MM-DD&to=YYYY-MM-DD`

The person's own logged time in that period (inclusive, by local date):

```json
{"entries": [{
  "id": "381",                       // the app's id, a string
  "title": "Fix the login redirect",
  "started_at": "2026-10-28T09:10:00+01:00",   // optional when only a day is known
  "ended_at": "2026-10-28T10:25:00+01:00",     // optional
  "date": "2026-10-28",
  "duration_s": 4500,
  "ref": "issue:ATH-142",            // what it was for, the same ids as in items
  "label": "ATH-142 Fix the login redirect",
  "url": "/issue/ATH-142",           // a path in the app, or a full URL
  "source": "app",                   // "time" for what Pocket Time wrote
  "external_id": ""                  // "time:<id>" for what Pocket Time wrote
}]}
```

Never notes or anything private beyond title and label.

### `GET /api/v1/worklog/items?q=`

What time can be for, for the "For" picker in Pocket Time: recent/active first, at most ~50, `q` searches.

```json
{"items": [{"ref": "issue:ATH-142", "label": "ATH-142 Fix the login redirect", "detail": "Athena · In progress", "url": "/issue/ATH-142"}]}
```

### `POST /api/v1/worklog`

Log time tracked in Pocket Time. **Idempotent on `external_id`**: the same `external_id` again updates that record (an edited entry is sent again).

```json
{"ref": "issue:ATH-142", "title": "…", "note": "…", "started_at": "…", "ended_at": "…", "date": "2026-10-28",
 "duration_s": 4500, "billable": true, "project": "Harbor mobile app", "external_id": "time:812"}
```

Answer `{"id": "381", "url": "/issue/ATH-142"}`. Unknown `ref` → 400 with a plain message. The record's `source` is the acting app (`time`); the link may change only records it made.

### `DELETE /api/v1/worklog/{id}`

Remove a record Pocket Time made (only those; others → 403/404).
