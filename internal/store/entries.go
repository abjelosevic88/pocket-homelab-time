package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// ErrRunning is returned when a timer should start while another one runs
// and the caller did not ask to switch.
var ErrRunning = errors.New("a timer is already running")

// ParseTime reads RFC3339 (with or without fractions) or a local
// "YYYY-MM-DDTHH:MM[:SS]".
func ParseTime(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	for _, f := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05", "2006-01-02T15:04", "2006-01-02 15:04:05", "2006-01-02 15:04"} {
		var t time.Time
		var err error
		if strings.Contains(f, "Z07") {
			t, err = time.Parse(f, s)
		} else {
			t, err = time.ParseInLocation(f, s, time.Local)
		}
		if err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// Stamp is the stored form of a moment: RFC3339 in local time, whole seconds.
func Stamp(t time.Time) string { return t.In(time.Local).Truncate(time.Second).Format(time.RFC3339) }

// LocalDate is the local calendar date of a moment.
func LocalDate(t time.Time) string { return t.In(time.Local).Format("2006-01-02") }

const entryCols = `e.id, e.user_id, e.title, e.note, e.project_id, e.tags, e.billable, e.started_at, e.ended_at, e.date, e.duration_s,
	e.source, e.app, e.app_ref, e.app_label, e.app_url, e.collected, e.external_id,
	e.deliver, e.delivered_at, e.deliver_error, e.remote_id, e.created_at, e.updated_at,
	COALESCE(p.name, ''), COALESCE(p.color, '')`

const entryFrom = ` FROM entries e LEFT JOIN projects p ON p.id = e.project_id AND p.user_id = e.user_id`

func scanEntry(row interface{ Scan(...any) error }) (*Entry, error) {
	var e Entry
	var pid sql.NullInt64
	var bill, coll, del int
	if err := row.Scan(&e.ID, &e.UserID, &e.Title, &e.Note, &pid, &e.Tags, &bill, &e.StartedAt, &e.EndedAt, &e.Date, &e.DurationS,
		&e.Source, &e.App, &e.AppRef, &e.AppLabel, &e.AppURL, &coll, &e.ExternalID,
		&del, &e.DeliveredAt, &e.DeliverError, &e.RemoteID, &e.CreatedAt, &e.UpdatedAt,
		&e.ProjectName, &e.ProjectColor); err != nil {
		return nil, err
	}
	if pid.Valid {
		v := pid.Int64
		e.ProjectID = &v
	}
	e.Billable, e.Collected, e.Deliver = bill == 1, coll == 1, del == 1
	e.Live()
	return &e, nil
}

// Live fills in the running state and, while running, the length so far.
func (e *Entry) Live() {
	e.Running = e.EndedAt == ""
	if e.Running {
		if t, ok := ParseTime(e.StartedAt); ok {
			e.DurationS = int(time.Since(t).Seconds())
			if e.DurationS < 0 {
				e.DurationS = 0
			}
		}
	}
}

// EntryFilter narrows ListEntries.
type EntryFilter struct {
	From, To  string // local dates, inclusive
	ProjectID int64  // -1 = no project
	App       string // "time" = only here (no app)
	Source    string
	Q         string
	Collected *bool
	Limit     int
	Offset    int
	Asc       bool
}

// ListEntries returns one person's entries, newest first, and how many match.
func (s *Store) ListEntries(ctx context.Context, uid int64, f EntryFilter) ([]Entry, int, error) {
	where := []string{"e.user_id = ?"}
	args := []any{uid}
	if f.From != "" {
		where = append(where, "e.date >= ?")
		args = append(args, f.From)
	}
	if f.To != "" {
		where = append(where, "e.date <= ?")
		args = append(args, f.To)
	}
	switch {
	case f.ProjectID > 0:
		where = append(where, "e.project_id = ?")
		args = append(args, f.ProjectID)
	case f.ProjectID < 0:
		where = append(where, "e.project_id IS NULL")
	}
	switch f.App {
	case "":
	case "time":
		where = append(where, "e.app = ''")
	default:
		where = append(where, "e.app = ?")
		args = append(args, f.App)
	}
	if f.Source != "" {
		where = append(where, "e.source = ?")
		args = append(args, f.Source)
	}
	if f.Collected != nil {
		where = append(where, "e.collected = ?")
		args = append(args, boolInt(*f.Collected))
	}
	if q := strings.TrimSpace(f.Q); q != "" {
		where = append(where, "(e.title LIKE ? OR e.note LIKE ? OR e.tags LIKE ? OR e.app_label LIKE ? OR p.name LIKE ?)")
		like := "%" + q + "%"
		args = append(args, like, like, like, like, like)
	}
	w := " WHERE " + strings.Join(where, " AND ")
	var total int
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(1)`+entryFrom+w, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	order := " ORDER BY e.started_at DESC, e.id DESC"
	if f.Asc {
		order = " ORDER BY e.started_at, e.id"
	}
	q := `SELECT ` + entryCols + entryFrom + w + order
	if f.Limit > 0 {
		q += " LIMIT ? OFFSET ?"
		args = append(args, f.Limit, f.Offset)
	}
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []Entry{}
	for rows.Next() {
		e, err := scanEntry(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *e)
	}
	return out, total, rows.Err()
}

// GetEntry returns one of the person's entries.
func (s *Store) GetEntry(ctx context.Context, uid, id int64) (*Entry, error) {
	e, err := scanEntry(s.DB.QueryRowContext(ctx, `SELECT `+entryCols+entryFrom+` WHERE e.user_id = ? AND e.id = ?`, uid, id))
	return e, notFound(err)
}

// RunningEntry returns the person's running timer (ErrNotFound if none).
func (s *Store) RunningEntry(ctx context.Context, uid int64) (*Entry, error) {
	e, err := scanEntry(s.DB.QueryRowContext(ctx, `SELECT `+entryCols+entryFrom+` WHERE e.user_id = ? AND e.ended_at = ''`, uid))
	return e, notFound(err)
}

type execer interface {
	ExecContext(ctx context.Context, q string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row
}

func insertEntry(ctx context.Context, x execer, uid int64, e *Entry) error {
	now := Now()
	res, err := x.ExecContext(ctx, `INSERT INTO entries (user_id, title, note, project_id, tags, billable, started_at, ended_at, date, duration_s,
		source, app, app_ref, app_label, app_url, collected, external_id, deliver, delivered_at, deliver_error, remote_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		uid, e.Title, e.Note, nullI(e.ProjectID), e.Tags, boolInt(e.Billable), e.StartedAt, e.EndedAt, e.Date, e.DurationS,
		e.Source, e.App, e.AppRef, e.AppLabel, e.AppURL, boolInt(e.Collected), e.ExternalID, boolInt(e.Deliver), e.DeliveredAt, e.DeliverError, e.RemoteID, now, now)
	if err != nil {
		return conflict(err)
	}
	e.ID, _ = res.LastInsertId()
	e.UserID = uid
	return nil
}

// stopAt finishes a running entry at end (never before its start).
func stopAt(ctx context.Context, x execer, uid int64, id int64, startedAt string, end time.Time) error {
	st, ok := ParseTime(startedAt)
	if !ok {
		st = end
	}
	if end.Before(st) {
		end = st
	}
	_, err := x.ExecContext(ctx, `UPDATE entries SET ended_at = ?, duration_s = ?, updated_at = ? WHERE user_id = ? AND id = ? AND ended_at = ''`,
		Stamp(end), int(end.Sub(st).Seconds()), Now(), uid, id)
	return err
}

// StartEntry starts e as the person's timer. When another timer runs it is
// stopped first if switchRunning is set (and returned), else ErrRunning.
func (s *Store) StartEntry(ctx context.Context, uid int64, e *Entry, switchRunning bool) (*Entry, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	var stopped *Entry
	cur, err := scanEntry(tx.QueryRowContext(ctx, `SELECT `+entryCols+entryFrom+` WHERE e.user_id = ? AND e.ended_at = ''`, uid))
	switch {
	case err == nil:
		if !switchRunning {
			return cur, ErrRunning
		}
		start, _ := ParseTime(e.StartedAt)
		if err := stopAt(ctx, tx, uid, cur.ID, cur.StartedAt, start); err != nil {
			return nil, err
		}
		stopped = cur
	case !errors.Is(err, sql.ErrNoRows):
		return nil, err
	}
	e.EndedAt, e.DurationS = "", 0
	if err := insertEntry(ctx, tx, uid, e); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return stopped, nil
}

// StopEntry finishes the running timer at end.
func (s *Store) StopEntry(ctx context.Context, uid int64, end time.Time) (*Entry, error) {
	cur, err := s.RunningEntry(ctx, uid)
	if err != nil {
		return nil, err
	}
	if err := stopAt(ctx, s.DB, uid, cur.ID, cur.StartedAt, end); err != nil {
		return nil, err
	}
	return s.GetEntry(ctx, uid, cur.ID)
}

// CreateEntry adds a finished entry. With an external id already present for
// the person it updates that one instead (the same import or app log twice
// adds nothing).
func (s *Store) CreateEntry(ctx context.Context, uid int64, e *Entry) error {
	if e.ExternalID != "" {
		var id int64
		if err := s.DB.QueryRowContext(ctx, `SELECT id FROM entries WHERE user_id = ? AND external_id = ?`, uid, e.ExternalID).Scan(&id); err == nil {
			e.ID = id
			return s.UpdateEntry(ctx, uid, e)
		}
	}
	return insertEntry(ctx, s.DB, uid, e)
}

// UpdateEntry saves an entry's fields (not its delivery state).
func (s *Store) UpdateEntry(ctx context.Context, uid int64, e *Entry) error {
	res, err := s.DB.ExecContext(ctx, `UPDATE entries SET title = ?, note = ?, project_id = ?, tags = ?, billable = ?, started_at = ?, ended_at = ?, date = ?, duration_s = ?,
		app = ?, app_ref = ?, app_label = ?, app_url = ?, deliver = ?, updated_at = ? WHERE user_id = ? AND id = ?`,
		e.Title, e.Note, nullI(e.ProjectID), e.Tags, boolInt(e.Billable), e.StartedAt, e.EndedAt, e.Date, e.DurationS,
		e.App, e.AppRef, e.AppLabel, e.AppURL, boolInt(e.Deliver), Now(), uid, e.ID)
	if err != nil {
		return conflict(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteEntry removes one of the person's entries.
func (s *Store) DeleteEntry(ctx context.Context, uid, id int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM entries WHERE user_id = ? AND id = ?`, uid, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// SetDelivery records how sending an entry to its app went.
func (s *Store) SetDelivery(ctx context.Context, uid, id int64, remoteID, errText string) error {
	at := ""
	if errText == "" {
		at = Now()
	}
	_, err := s.DB.ExecContext(ctx, `UPDATE entries SET delivered_at = ?, deliver_error = ?, remote_id = CASE WHEN ? <> '' THEN ? ELSE remote_id END WHERE user_id = ? AND id = ?`,
		at, errText, remoteID, remoteID, uid, id)
	return err
}

// ResetDelivery marks an entry to be sent again (after an edit).
func (s *Store) ResetDelivery(ctx context.Context, uid, id int64) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE entries SET delivered_at = '', deliver_error = '' WHERE user_id = ? AND id = ?`, uid, id)
	return err
}

// PendingDeliveries are finished entries still to be written to their app
// (changed in the last `days` days).
func (s *Store) PendingDeliveries(ctx context.Context, uid int64, days int) ([]Entry, error) {
	since := time.Now().AddDate(0, 0, -days).UTC().Format(time.RFC3339)
	rows, err := s.DB.QueryContext(ctx, `SELECT `+entryCols+entryFrom+` WHERE e.user_id = ? AND e.deliver = 1 AND e.app <> '' AND e.ended_at <> '' AND e.delivered_at = '' AND e.collected = 0 AND e.updated_at >= ? ORDER BY e.id`, uid, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Entry
	for rows.Next() {
		e, err := scanEntry(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *e)
	}
	return out, rows.Err()
}

// ReplaceCollected makes the person's collected entries from app between
// from and to exactly list: new ones are added, changed ones updated, and
// ones the app no longer has are removed. Returns how many it holds.
func (s *Store) ReplaceCollected(ctx context.Context, uid int64, app, from, to string, list []Entry) (int, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	keep := map[string]bool{}
	for i := range list {
		e := list[i]
		e.App, e.Collected, e.Deliver = app, true, false
		if e.Source == "" {
			e.Source = app
		}
		keep[e.ExternalID] = true
		res, err := tx.ExecContext(ctx, `UPDATE entries SET title = ?, note = ?, started_at = ?, ended_at = ?, date = ?, duration_s = ?, app_ref = ?, app_label = ?, app_url = ?, updated_at = ?
			WHERE user_id = ? AND external_id = ? AND collected = 1 AND (title <> ? OR note <> ? OR started_at <> ? OR ended_at <> ? OR duration_s <> ? OR app_ref <> ? OR app_label <> ? OR app_url <> ?)`,
			e.Title, e.Note, e.StartedAt, e.EndedAt, e.Date, e.DurationS, e.AppRef, e.AppLabel, e.AppURL, Now(),
			uid, e.ExternalID, e.Title, e.Note, e.StartedAt, e.EndedAt, e.DurationS, e.AppRef, e.AppLabel, e.AppURL)
		if err != nil {
			return 0, err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			continue
		}
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(1) FROM entries WHERE user_id = ? AND external_id = ?`, uid, e.ExternalID).Scan(&n); err != nil {
			return 0, err
		}
		if n > 0 {
			continue
		}
		if err := insertEntry(ctx, tx, uid, &e); err != nil {
			return 0, err
		}
	}
	rows, err := tx.QueryContext(ctx, `SELECT id, external_id FROM entries WHERE user_id = ? AND app = ? AND collected = 1 AND date >= ? AND date <= ?`, uid, app, from, to)
	if err != nil {
		return 0, err
	}
	var gone []int64
	for rows.Next() {
		var id int64
		var ext string
		if err := rows.Scan(&id, &ext); err != nil {
			rows.Close()
			return 0, err
		}
		if !keep[ext] {
			gone = append(gone, id)
		}
	}
	rows.Close()
	for _, id := range gone {
		if _, err := tx.ExecContext(ctx, `DELETE FROM entries WHERE user_id = ? AND id = ?`, uid, id); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return len(list), nil
}

// ClearCollected removes everything collected from an app (collecting switched off).
func (s *Store) ClearCollected(ctx context.Context, uid int64, app string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM entries WHERE user_id = ? AND app = ? AND collected = 1`, uid, app)
	return err
}

// SaveAppSync records how collecting from an app went.
func (s *Store) SaveAppSync(ctx context.Context, uid int64, a AppSync) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO app_syncs (user_id, app, synced_at, error, count) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(user_id, app) DO UPDATE SET synced_at = excluded.synced_at, error = excluded.error, count = excluded.count`, uid, a.App, a.SyncedAt, a.Error, a.Count)
	return err
}

// AppSyncs returns how collecting went per app for a person.
func (s *Store) AppSyncs(ctx context.Context, uid int64) (map[string]AppSync, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT app, synced_at, error, count FROM app_syncs WHERE user_id = ?`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]AppSync{}
	for rows.Next() {
		var a AppSync
		if err := rows.Scan(&a.App, &a.SyncedAt, &a.Error, &a.Count); err != nil {
			return nil, err
		}
		out[a.App] = a
	}
	return out, rows.Err()
}

// RecentTitles are the person's most used recent entry titles (for suggestions).
func (s *Store) RecentTitles(ctx context.Context, uid int64, limit int) ([]Entry, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+entryCols+entryFrom+` WHERE e.id IN (
		SELECT MAX(id) FROM entries WHERE user_id = ? AND collected = 0 AND title <> '' GROUP BY title, COALESCE(project_id, 0), app, app_ref
		) ORDER BY e.started_at DESC LIMIT ?`, uid, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Entry{}
	for rows.Next() {
		e, err := scanEntry(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *e)
	}
	return out, rows.Err()
}

// LongRunning lists running timers started before cutoff, across everybody
// (for the "still running?" reminder).
func (s *Store) LongRunning(ctx context.Context, cutoff time.Time) ([]Entry, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+entryCols+entryFrom+` WHERE e.ended_at = ''`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Entry
	for rows.Next() {
		e, err := scanEntry(rows)
		if err != nil {
			return nil, err
		}
		if t, ok := ParseTime(e.StartedAt); ok && t.Before(cutoff) {
			out = append(out, *e)
		}
	}
	return out, rows.Err()
}
