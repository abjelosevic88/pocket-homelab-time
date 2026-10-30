CREATE TABLE users (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  email         TEXT NOT NULL UNIQUE,
  name          TEXT NOT NULL DEFAULT '',
  password_hash TEXT NOT NULL DEFAULT '',
  role          TEXT NOT NULL DEFAULT 'member',
  created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at    TEXT
);

CREATE TABLE sessions (
  id         TEXT PRIMARY KEY,
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  expires_at TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE TABLE api_tokens (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name         TEXT NOT NULL DEFAULT '',
  token_hash   TEXT NOT NULL UNIQUE,
  prefix       TEXT NOT NULL DEFAULT '',
  last_used_at TEXT,
  created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

-- Box-wide settings (title, setup flag); everything else is personal.
CREATE TABLE settings (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL DEFAULT ''
);

CREATE TABLE user_settings (
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  key     TEXT NOT NULL,
  value   TEXT NOT NULL DEFAULT '',
  PRIMARY KEY (user_id, key)
);

CREATE TABLE cache (
  key        TEXT PRIMARY KEY,
  value      TEXT NOT NULL DEFAULT '',
  fetched_at TEXT NOT NULL
);

-- Your own projects: client work, a side project, a hobby.
CREATE TABLE projects (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name       TEXT NOT NULL,
  color      TEXT NOT NULL DEFAULT '',
  client     TEXT NOT NULL DEFAULT '',
  billable   INTEGER NOT NULL DEFAULT 0,   -- new entries in it are billable
  rate       REAL NOT NULL DEFAULT 0,      -- per hour, in the person's currency
  goal_min   INTEGER NOT NULL DEFAULT 0,   -- weekly target, 0 = none
  archived   INTEGER NOT NULL DEFAULT 0,
  sort       INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX projects_user ON projects(user_id, archived, sort);
CREATE UNIQUE INDEX projects_user_name ON projects(user_id, name COLLATE NOCASE);

-- One stretch of tracked time. Timed here (by Pocket Time's own timer, or by
-- another Pocket app starting and stopping it through Pocket Time), logged by
-- hand, or collected: a read-only copy of time a sibling app keeps in its own
-- log. ended_at is '' while the timer runs, and a person has at most one
-- running entry (the index below).
CREATE TABLE entries (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id       INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  title         TEXT NOT NULL DEFAULT '',
  note          TEXT NOT NULL DEFAULT '',
  project_id    INTEGER REFERENCES projects(id) ON DELETE SET NULL,
  tags          TEXT NOT NULL DEFAULT '',
  billable      INTEGER NOT NULL DEFAULT 0,
  started_at    TEXT NOT NULL,                -- RFC3339 with offset
  ended_at      TEXT NOT NULL DEFAULT '',     -- '' = running
  date          TEXT NOT NULL,                -- local date of started_at
  duration_s    INTEGER NOT NULL DEFAULT 0,
  source        TEXT NOT NULL DEFAULT 'time', -- time | manual | import | <pocket app slug that started it>
  app           TEXT NOT NULL DEFAULT '',     -- the Pocket app the time was for ('' = only here)
  app_ref       TEXT NOT NULL DEFAULT '',     -- that app's id for the item: "tab:42", "issue:ATH-12"
  app_label     TEXT NOT NULL DEFAULT '',
  app_url       TEXT NOT NULL DEFAULT '',
  collected     INTEGER NOT NULL DEFAULT 0,   -- 1 = a copy of a record kept in app
  external_id   TEXT NOT NULL DEFAULT '',     -- collected/imported: "<app>:<their id>"
  deliver       INTEGER NOT NULL DEFAULT 0,   -- 1 = write the finished entry to app
  delivered_at  TEXT NOT NULL DEFAULT '',
  deliver_error TEXT NOT NULL DEFAULT '',
  remote_id     TEXT NOT NULL DEFAULT '',     -- what it became there
  created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX entries_user_date ON entries(user_id, date);
CREATE INDEX entries_user_started ON entries(user_id, started_at);
CREATE INDEX entries_user_app ON entries(user_id, app, collected);
CREATE UNIQUE INDEX entries_running ON entries(user_id) WHERE ended_at = '';
CREATE UNIQUE INDEX entries_external ON entries(user_id, external_id) WHERE external_id <> '';

-- How collecting from each sibling app last went, per person.
CREATE TABLE app_syncs (
  user_id   INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  app       TEXT NOT NULL,
  synced_at TEXT NOT NULL DEFAULT '',
  error     TEXT NOT NULL DEFAULT '',
  count     INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (user_id, app)
);
