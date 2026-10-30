package server

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-time/internal/store"
)

// ---- the timer: one per person, shared by every Pocket app ----
//
// Pocket Time's own page and the other Pocket apps (through a Core link with
// time:write) start and stop the same timer. Starting while another runs
// answers 409 with the running one, unless the caller asks to switch: then
// the running one stops at the new start and both are returned.

// entryInput is what clients send for a timer or an entry.
type entryInput struct {
	Title      string `json:"title"`
	Note       string `json:"note"`
	Tags       string `json:"tags"`
	ProjectID  *int64 `json:"project_id"`
	Project    string `json:"project"` // or a project by name
	Billable   *bool  `json:"billable"`
	StartedAt  string `json:"started_at"`
	EndedAt    string `json:"ended_at"`
	DurationS  *int   `json:"duration_s"`
	App        string `json:"app"`
	AppRef     string `json:"app_ref"`
	AppLabel   string `json:"app_label"`
	AppURL     string `json:"app_url"`
	Deliver    *bool  `json:"deliver"`
	ExternalID string `json:"external_id"`
	Switch     bool   `json:"switch"`
	OnlyIf     int64  `json:"only_if"`
}

func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	if len([]rune(s)) > n {
		return string([]rune(s)[:n])
	}
	return s
}

// applyInput copies what was sent onto e and checks it. keys = the JSON keys
// sent (nil = all). Returns a plain-language problem or "".
func (s *Server) applyInput(ctx context.Context, uid int64, in *entryInput, keys map[string]bool, e *store.Entry) string {
	has := func(k string) bool { return keys == nil || keys[k] }
	if has("title") {
		e.Title = clip(in.Title, 300)
	}
	if has("note") {
		e.Note = clip(in.Note, 4000)
	}
	if has("tags") {
		e.Tags = store.JoinTags(in.Tags)
	}
	if has("project_id") || (has("project") && in.Project != "") {
		e.ProjectID = nil
		if in.ProjectID != nil && *in.ProjectID > 0 {
			p, err := s.store.GetProject(ctx, uid, *in.ProjectID)
			if err != nil {
				return "that project does not exist"
			}
			e.ProjectID = &p.ID
		} else if name := strings.TrimSpace(in.Project); name != "" {
			if p, err := s.store.ProjectByName(ctx, uid, name); err == nil {
				e.ProjectID = &p.ID
			}
		}
	}
	if has("billable") && in.Billable != nil {
		e.Billable = *in.Billable
	}
	if linkApp(ctx) == "" {
		if has("app") {
			app := strings.TrimSpace(in.App)
			if app != "" {
				if _, ok := appBySlug(app); !ok {
					return "unknown app " + app
				}
			}
			e.App = app
			if app == "" {
				e.AppRef, e.AppLabel, e.AppURL, e.Deliver = "", "", "", false
			}
		}
	}
	if e.App != "" || linkApp(ctx) != "" {
		if has("app_ref") {
			e.AppRef = clip(in.AppRef, 200)
		}
		if has("app_label") {
			e.AppLabel = clip(in.AppLabel, 300)
		}
		if has("app_url") {
			e.AppURL = clip(in.AppURL, 1000)
		}
		if has("deliver") && in.Deliver != nil {
			e.Deliver = *in.Deliver
		}
	}
	if e.Title == "" && e.AppLabel != "" {
		e.Title = e.AppLabel
	}
	return ""
}

// projectBillable is the default "billable" for an entry in a project.
func (s *Server) projectBillable(ctx context.Context, uid int64, pid *int64, st store.Settings) bool {
	if pid != nil {
		if p, err := s.store.GetProject(ctx, uid, *pid); err == nil {
			return p.Billable
		}
	}
	return st.DefaultBill
}

func (s *Server) handleGetTimer(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	e, err := s.store.RunningEntry(ctx, actorID(ctx))
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		s.fail(w, err, "timer")
		return
	}
	var running any
	if e != nil {
		running = forLink(ctx, []store.Entry{*e})[0]
	}
	writeJSON(w, http.StatusOK, map[string]any{"running": running, "now": store.Stamp(time.Now())})
}

// handleStartTimer starts the person's timer.
func (s *Server) handleStartTimer(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	var in entryInput
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	st, _ := s.store.GetSettings(ctx, uid)
	now := time.Now()
	start := now
	if in.StartedAt != "" {
		t, ok := store.ParseTime(in.StartedAt)
		if !ok {
			writeErr(w, http.StatusBadRequest, "started_at is a time (RFC3339)")
			return
		}
		if t.After(now.Add(time.Minute)) || t.Before(now.Add(-24*time.Hour)) {
			writeErr(w, http.StatusBadRequest, "a timer starts in the last 24 hours")
			return
		}
		start = t
	}
	e := store.Entry{Source: "time", StartedAt: store.Stamp(start), Date: store.LocalDate(start)}
	if app := linkApp(ctx); app != "" {
		// Another app starts a timer for something of its own. By default
		// Time writes the finished time back to it (deliver), unless the app
		// says it records it itself.
		e.Source, e.App, e.Deliver = app, app, true
		if _, ok := appBySlug(app); !ok {
			e.Deliver = false
		}
	} else if in.App != "" {
		e.Deliver = true
	}
	if m := s.applyInput(ctx, uid, &in, nil, &e); m != "" {
		writeErr(w, http.StatusBadRequest, m)
		return
	}
	if in.Billable == nil {
		e.Billable = s.projectBillable(ctx, uid, e.ProjectID, st)
	}
	if e.Title == "" && e.ProjectID == nil && e.App == "" && linkApp(ctx) != "" {
		e.Title = appName(linkApp(ctx))
	}
	stopped, err := s.store.StartEntry(ctx, uid, &e, in.Switch)
	if errors.Is(err, store.ErrRunning) {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "a timer is already running: " + entryName(*stopped), "running": forLink(ctx, []store.Entry{*stopped})[0]})
		return
	}
	if err != nil {
		s.fail(w, err, "start timer")
		return
	}
	out, err := s.store.GetEntry(ctx, uid, e.ID)
	if err != nil {
		s.fail(w, err, "timer")
		return
	}
	var stoppedOut any
	if stopped != nil {
		if se, err := s.store.GetEntry(ctx, uid, stopped.ID); err == nil {
			s.timerStopped(ctx, uid, se)
			stoppedOut = forLink(ctx, []store.Entry{*se})[0]
		}
	}
	s.publish(ctx, uid, "time.timer_started", "entry:"+strconv.FormatInt(out.ID, 10), entryPayload(out))
	writeJSON(w, http.StatusOK, map[string]any{"running": forLink(ctx, []store.Entry{*out})[0], "stopped": stoppedOut})
}

// handleStopTimer stops the person's timer, wherever it was started.
// only_if = the entry the caller believes is running (another one → 409).
func (s *Server) handleStopTimer(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	var in entryInput
	_ = decode(r, &in)
	cur, err := s.store.RunningEntry(ctx, uid)
	if errors.Is(err, store.ErrNotFound) {
		writeJSON(w, http.StatusOK, map[string]any{"running": nil, "stopped": nil})
		return
	}
	if err != nil {
		s.fail(w, err, "timer")
		return
	}
	if in.OnlyIf > 0 && in.OnlyIf != cur.ID {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "a different timer is running now: " + entryName(*cur), "running": forLink(ctx, []store.Entry{*cur})[0]})
		return
	}
	end := time.Now()
	if in.EndedAt != "" {
		t, ok := store.ParseTime(in.EndedAt)
		if !ok || t.After(end.Add(time.Minute)) {
			writeErr(w, http.StatusBadRequest, "ended_at is a time, not in the future")
			return
		}
		end = t
	}
	if strings.TrimSpace(in.Note) != "" {
		cur.Note = clip(strings.TrimSpace(cur.Note+"\n"+in.Note), 4000)
		if err := s.store.UpdateEntry(ctx, uid, cur); err != nil {
			s.fail(w, err, "timer")
			return
		}
	}
	out, err := s.store.StopEntry(ctx, uid, end)
	if err != nil {
		s.fail(w, err, "stop timer")
		return
	}
	s.timerStopped(ctx, uid, out)
	writeJSON(w, http.StatusOK, map[string]any{"running": nil, "stopped": forLink(ctx, []store.Entry{*out})[0]})
}

// timerStopped announces a stopped timer and sends it to its app.
func (s *Server) timerStopped(ctx context.Context, uid int64, e *store.Entry) {
	s.saved.Add(1)
	s.publish(ctx, uid, "time.timer_stopped", "entry:"+strconv.FormatInt(e.ID, 10), entryPayload(e))
	if e.Deliver && e.App != "" {
		s.sendSoon(uid)
	}
}

// handleUpdateTimer changes the running timer (what it is for, when it started).
func (s *Server) handleUpdateTimer(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	cur, err := s.store.RunningEntry(ctx, uid)
	if err != nil {
		s.fail(w, err, "timer")
		return
	}
	var in entryInput
	keys, err := decodeKeys(r, &in)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if m := s.applyInput(ctx, uid, &in, keys, cur); m != "" {
		writeErr(w, http.StatusBadRequest, m)
		return
	}
	if keys["started_at"] {
		t, ok := store.ParseTime(in.StartedAt)
		if !ok || t.After(time.Now()) {
			writeErr(w, http.StatusBadRequest, "the start is a time, not in the future")
			return
		}
		cur.StartedAt, cur.Date = store.Stamp(t), store.LocalDate(t)
	}
	if keys["project_id"] && !keys["billable"] {
		st, _ := s.store.GetSettings(ctx, uid)
		cur.Billable = s.projectBillable(ctx, uid, cur.ProjectID, st)
	}
	cur.EndedAt, cur.DurationS = "", 0
	if err := s.store.UpdateEntry(ctx, uid, cur); err != nil {
		s.fail(w, err, "timer")
		return
	}
	out, _ := s.store.GetEntry(ctx, uid, cur.ID)
	writeJSON(w, http.StatusOK, map[string]any{"running": out})
}

// handleDiscardTimer throws the running timer away.
func (s *Server) handleDiscardTimer(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	cur, err := s.store.RunningEntry(ctx, uid)
	if err != nil {
		s.fail(w, err, "timer")
		return
	}
	if err := s.store.DeleteEntry(ctx, uid, cur.ID); err != nil {
		s.fail(w, err, "timer")
		return
	}
	s.publish(ctx, uid, "time.timer_stopped", "entry:"+strconv.FormatInt(cur.ID, 10), map[string]any{"entry_id": cur.ID, "discarded": true, "app": cur.App, "app_ref": cur.AppRef})
	writeJSON(w, http.StatusOK, map[string]any{"running": nil})
}

// handleContinue starts a new timer for the same thing as an earlier entry.
func (s *Server) handleContinue(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	src, err := s.store.GetEntry(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "entry")
		return
	}
	var in struct {
		Switch bool `json:"switch"`
	}
	_ = decode(r, &in)
	now := time.Now()
	e := store.Entry{Source: "time", Title: src.Title, ProjectID: src.ProjectID, Tags: src.Tags, Billable: src.Billable,
		App: src.App, AppRef: src.AppRef, AppLabel: src.AppLabel, AppURL: src.AppURL, Deliver: src.App != "",
		StartedAt: store.Stamp(now), Date: store.LocalDate(now)}
	if _, ok := appBySlug(e.App); !ok {
		e.App, e.AppRef, e.AppLabel, e.AppURL, e.Deliver = "", "", "", "", false
	}
	stopped, err := s.store.StartEntry(ctx, uid, &e, in.Switch)
	if errors.Is(err, store.ErrRunning) {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "a timer is already running: " + entryName(*stopped), "running": stopped})
		return
	}
	if err != nil {
		s.fail(w, err, "start timer")
		return
	}
	var stoppedOut any
	if stopped != nil {
		if se, err := s.store.GetEntry(ctx, uid, stopped.ID); err == nil {
			s.timerStopped(ctx, uid, se)
			stoppedOut = se
		}
	}
	out, _ := s.store.GetEntry(ctx, uid, e.ID)
	s.publish(ctx, uid, "time.timer_started", "entry:"+strconv.FormatInt(out.ID, 10), entryPayload(out))
	writeJSON(w, http.StatusOK, map[string]any{"running": out, "stopped": stoppedOut})
}

// ---- entries ----

func (s *Server) handleListEntries(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()
	f := store.EntryFilter{From: q.Get("from"), To: q.Get("to"), App: q.Get("app"), Source: q.Get("source"), Q: q.Get("q"),
		Limit: queryInt(r, "limit"), Offset: queryInt(r, "offset")}
	if !validDate(f.From) || !validDate(f.To) {
		writeErr(w, http.StatusBadRequest, "from and to are dates (YYYY-MM-DD)")
		return
	}
	if v := q.Get("project_id"); v != "" {
		f.ProjectID, _ = strconv.ParseInt(v, 10, 64)
		if v == "none" {
			f.ProjectID = -1
		}
	}
	if v := q.Get("collected"); v != "" {
		b := v == "1" || v == "true"
		f.Collected = &b
	}
	if f.Limit <= 0 || f.Limit > 2000 {
		f.Limit = 200
	}
	if linkApp(ctx) != "" {
		f.Q = ""
	}
	list, total, err := s.store.ListEntries(ctx, actorID(ctx), f)
	if err != nil {
		s.fail(w, err, "entries")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": forLink(ctx, list), "total": total})
}

func (s *Server) handleGetEntry(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	e, err := s.store.GetEntry(ctx, actorID(ctx), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "entry")
		return
	}
	writeJSON(w, http.StatusOK, forLink(ctx, []store.Entry{*e})[0])
}

// finishTimes fills ended_at, duration and date from what was sent.
func finishTimes(in *entryInput, keys map[string]bool, e *store.Entry) string {
	has := func(k string) bool { return keys == nil || keys[k] }
	start, ok := store.ParseTime(e.StartedAt)
	if has("started_at") {
		start, ok = store.ParseTime(in.StartedAt)
		if !ok {
			return "when did it start? (started_at)"
		}
	}
	if !ok {
		return "when did it start? (started_at)"
	}
	end, endOK := store.ParseTime(e.EndedAt)
	switch {
	case has("ended_at") && in.EndedAt != "":
		end, endOK = store.ParseTime(in.EndedAt)
		if !endOK {
			return "ended_at is a time"
		}
	case has("duration_s") && in.DurationS != nil:
		end, endOK = start.Add(time.Duration(*in.DurationS)*time.Second), true
	case has("started_at") && endOK && e.DurationS > 0:
		end = start.Add(time.Duration(e.DurationS) * time.Second)
	}
	if !endOK {
		return "how long was it? (ended_at or duration_s)"
	}
	d := end.Sub(start)
	if d < 0 {
		return "it ends before it starts"
	}
	if d > 7*24*time.Hour {
		return "an entry is at most a week long"
	}
	if start.After(time.Now().Add(time.Minute)) {
		return "an entry cannot start in the future"
	}
	e.StartedAt, e.EndedAt, e.Date, e.DurationS = store.Stamp(start), store.Stamp(end), store.LocalDate(start), int(d.Seconds())
	return ""
}

// handleCreateEntry logs time done earlier (by hand, or by a linked app).
func (s *Server) handleCreateEntry(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	var in entryInput
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	st, _ := s.store.GetSettings(ctx, uid)
	e := store.Entry{Source: "manual"}
	if app := linkApp(ctx); app != "" {
		e.Source, e.App = app, app
		if ext := clip(in.ExternalID, 200); ext != "" {
			e.ExternalID = app + "/log:" + ext
		}
	} else if in.App != "" {
		e.Deliver = true
	}
	if m := s.applyInput(ctx, uid, &in, nil, &e); m != "" {
		writeErr(w, http.StatusBadRequest, m)
		return
	}
	if m := finishTimes(&in, nil, &e); m != "" {
		writeErr(w, http.StatusBadRequest, m)
		return
	}
	if in.Billable == nil {
		e.Billable = s.projectBillable(ctx, uid, e.ProjectID, st)
	}
	if err := s.store.CreateEntry(ctx, uid, &e); err != nil {
		s.fail(w, err, "save entry")
		return
	}
	out, err := s.store.GetEntry(ctx, uid, e.ID)
	if err != nil {
		s.fail(w, err, "entry")
		return
	}
	s.saved.Add(1)
	s.publish(ctx, uid, "time.entry_logged", "entry:"+strconv.FormatInt(out.ID, 10), entryPayload(out))
	if out.Deliver && out.App != "" {
		s.sendSoon(uid)
	}
	writeJSON(w, http.StatusOK, forLink(ctx, []store.Entry{*out})[0])
}

// editable fetches an entry the caller may change: collected time is changed
// in the app that keeps it, and a linked app only changes what it logged.
func (s *Server) editable(w http.ResponseWriter, r *http.Request) (*store.Entry, bool) {
	ctx := r.Context()
	e, err := s.store.GetEntry(ctx, actorID(ctx), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "entry")
		return nil, false
	}
	if app := linkApp(ctx); app != "" && e.Source != app {
		writeErr(w, http.StatusForbidden, "this time was not logged by "+appName(app))
		return nil, false
	}
	if e.Collected {
		writeErr(w, http.StatusConflict, "this time is kept in "+appName(e.App)+"; change it there")
		return nil, false
	}
	return e, true
}

func (s *Server) handleUpdateEntry(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	e, ok := s.editable(w, r)
	if !ok {
		return
	}
	var in entryInput
	keys, err := decodeKeys(r, &in)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	wasApp, wasRemote := e.App, e.RemoteID
	if m := s.applyInput(ctx, uid, &in, keys, e); m != "" {
		writeErr(w, http.StatusBadRequest, m)
		return
	}
	if !e.Running && (keys["started_at"] || keys["ended_at"] || keys["duration_s"]) {
		if m := finishTimes(&in, keys, e); m != "" {
			writeErr(w, http.StatusBadRequest, m)
			return
		}
	}
	if keys["project_id"] && !keys["billable"] {
		st, _ := s.store.GetSettings(ctx, uid)
		e.Billable = s.projectBillable(ctx, uid, e.ProjectID, st)
	}
	if err := s.store.UpdateEntry(ctx, uid, e); err != nil {
		s.fail(w, err, "save entry")
		return
	}
	if !e.Running && e.Deliver && e.App != "" {
		// sent before: send again (the app updates its copy by our id)
		if wasApp != "" && wasApp != e.App && wasRemote != "" {
			s.unsendLater(uid, wasApp, wasRemote)
		}
		_ = s.store.ResetDelivery(ctx, uid, e.ID)
		s.sendSoon(uid)
	}
	out, _ := s.store.GetEntry(ctx, uid, e.ID)
	writeJSON(w, http.StatusOK, forLink(ctx, []store.Entry{*out})[0])
}

func (s *Server) handleDeleteEntry(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	e, ok := s.editable(w, r)
	if !ok {
		return
	}
	if err := s.store.DeleteEntry(ctx, uid, e.ID); err != nil {
		s.fail(w, err, "delete entry")
		return
	}
	if e.RemoteID != "" && e.App != "" && queryBool(r, "everywhere") {
		s.unsendLater(uid, e.App, e.RemoteID)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleResend sends an entry to its app again now.
func (s *Server) handleResend(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	e, ok := s.editable(w, r)
	if !ok {
		return
	}
	if e.App == "" || e.Running {
		writeErr(w, http.StatusBadRequest, "nothing to send")
		return
	}
	if !e.Deliver {
		e.Deliver = true
		_ = s.store.UpdateEntry(ctx, uid, e)
	}
	_ = s.store.ResetDelivery(ctx, uid, e.ID)
	s.sendPending(ctx, uid)
	out, _ := s.store.GetEntry(ctx, uid, e.ID)
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleSuggestions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	list, err := s.store.RecentTitles(ctx, actorID(ctx), 12)
	if err != nil {
		s.fail(w, err, "suggestions")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// forLink drops what a linked app never sees (notes).
func forLink(ctx context.Context, list []store.Entry) []store.Entry {
	if linkApp(ctx) == "" {
		return list
	}
	out := make([]store.Entry, len(list))
	for i, e := range list {
		e.Note = ""
		out[i] = e
	}
	return out
}

func entryName(e store.Entry) string {
	switch {
	case e.Title != "":
		return e.Title
	case e.ProjectName != "":
		return e.ProjectName
	case e.App != "":
		return appName(e.App)
	}
	return "untitled"
}

func entryPayload(e *store.Entry) map[string]any {
	return map[string]any{"entry_id": e.ID, "title": e.Title, "project": e.ProjectName, "app": e.App, "app_ref": e.AppRef, "source": e.Source,
		"started_at": e.StartedAt, "ended_at": e.EndedAt, "date": e.Date, "duration_s": e.DurationS, "minutes": e.DurationS / 60}
}

// appName is a sibling app's display name.
func appName(slug string) string {
	if a, ok := appBySlug(slug); ok {
		return a.name
	}
	switch slug {
	case "", "time":
		return "Pocket Time"
	}
	return "Pocket " + strings.ToUpper(slug[:1]) + slug[1:]
}

// ---- projects ----

type projectInput struct {
	Name     string  `json:"name"`
	Color    string  `json:"color"`
	Client   string  `json:"client"`
	Billable bool    `json:"billable"`
	Rate     float64 `json:"rate"`
	GoalMin  int     `json:"goal_min"`
	Archived bool    `json:"archived"`
	Sort     int     `json:"sort"`
}

func (in projectInput) apply(keys map[string]bool, p *store.Project) string {
	has := func(k string) bool { return keys == nil || keys[k] }
	if has("name") {
		p.Name = clip(in.Name, 120)
		if p.Name == "" {
			return "a project needs a name"
		}
	}
	if has("color") {
		p.Color = clip(in.Color, 20)
	}
	if has("client") {
		p.Client = clip(in.Client, 120)
	}
	if has("billable") {
		p.Billable = in.Billable
	}
	if has("rate") {
		if in.Rate < 0 || in.Rate > 1e6 {
			return "the rate is 0 or more"
		}
		p.Rate = in.Rate
	}
	if has("goal_min") {
		if in.GoalMin < 0 || in.GoalMin > 7*24*60 {
			return "a weekly target is up to a week"
		}
		p.GoalMin = in.GoalMin
	}
	if has("archived") {
		p.Archived = in.Archived
	}
	if has("sort") {
		p.Sort = in.Sort
	}
	return ""
}

func (s *Server) handleListProjects(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	list, err := s.store.ListProjects(ctx, actorID(ctx), queryBool(r, "archived"))
	if err != nil {
		s.fail(w, err, "projects")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var in projectInput
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	p := store.Project{}
	if m := in.apply(nil, &p); m != "" {
		writeErr(w, http.StatusBadRequest, m)
		return
	}
	if err := s.store.CreateProject(ctx, actorID(ctx), &p); err != nil {
		if errors.Is(err, store.ErrConflict) {
			writeErr(w, http.StatusConflict, "you already have a project called "+p.Name)
			return
		}
		s.fail(w, err, "project")
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) handleUpdateProject(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	p, err := s.store.GetProject(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "project")
		return
	}
	var in projectInput
	keys, err := decodeKeys(r, &in)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if m := in.apply(keys, p); m != "" {
		writeErr(w, http.StatusBadRequest, m)
		return
	}
	if err := s.store.UpdateProject(ctx, uid, p); err != nil {
		if errors.Is(err, store.ErrConflict) {
			writeErr(w, http.StatusConflict, "you already have a project called "+p.Name)
			return
		}
		s.fail(w, err, "project")
		return
	}
	out, _ := s.store.GetProject(ctx, uid, p.ID)
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleDeleteProject(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := s.store.DeleteProject(ctx, actorID(ctx), idParam(r, "id")); err != nil {
		s.fail(w, err, "project")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
