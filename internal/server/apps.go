package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/abjelosevic88/pocket-homelab-time/internal/pocket"
	"github.com/abjelosevic88/pocket-homelab-time/internal/store"
)

// ---- what Time does with each sibling app ----
//
// collect: read the time the app keeps itself (Guitar practice, Still
// practices, a Projects/Invoicing/Learn worklog) into read-only entries;
// items: what time can be for there (a song, an issue, a deck), for the
// timer's "For" picker; send: write a finished entry there (idempotent on
// external_id "time:<entry id>", so sending again updates it); unsend:
// remove what was sent. Records the app made from Time's own entries
// (external_id "time:…" or source "time") are skipped when collecting, so
// nothing is counted twice.

// item is one thing time can be for in a sibling app.
type item struct {
	Ref    string `json:"ref"`
	Label  string `json:"label"`
	Detail string `json:"detail,omitempty"`
	URL    string `json:"url,omitempty"`
}

type adapter interface {
	collect(ctx context.Context, c *pocket.Client, from, to string) ([]store.Entry, error)
	items(ctx context.Context, c *pocket.Client, q string) ([]item, error)
	send(ctx context.Context, c *pocket.Client, e store.Entry) (string, error)
	unsend(ctx context.Context, c *pocket.Client, remoteID string) error
}

func adapterFor(slug string) adapter {
	switch slug {
	case "guitar":
		return guitarAdapter{}
	case "still":
		return stillAdapter{}
	}
	return worklogAdapter{}
}

func ownTime(source, external string) bool {
	return source == "time" || strings.HasPrefix(external, "time:")
}

// endOf works out a record's end from its start and length.
func endOf(started, ended string, dur int) string {
	if ended != "" {
		if t, ok := store.ParseTime(ended); ok {
			return store.Stamp(t)
		}
	}
	if t, ok := store.ParseTime(started); ok {
		return store.Stamp(t.Add(time.Duration(dur) * time.Second))
	}
	return ""
}

// ---- Pocket Guitar: practice sessions, songs ----

type guitarAdapter struct{}

type guitarPractice struct {
	ID         int64  `json:"id"`
	TabID      *int64 `json:"tab_id"`
	Title      string `json:"title"`
	StartedAt  string `json:"started_at"`
	EndedAt    string `json:"ended_at"`
	Date       string `json:"date"`
	DurationS  int    `json:"duration_s"`
	Source     string `json:"source"`
	ExternalID string `json:"external_id"`
}

func (guitarAdapter) collect(ctx context.Context, c *pocket.Client, from, to string) ([]store.Entry, error) {
	var out []store.Entry
	for off := 0; off < 20000; off += 1000 {
		var res struct {
			Sessions []guitarPractice `json:"sessions"`
			Total    int              `json:"total"`
		}
		q := url.Values{"from": {from}, "to": {to}, "limit": {"1000"}, "offset": {strconv.Itoa(off)}}
		if err := c.Do(ctx, http.MethodGet, "/api/v1/practice", q, nil, &res); err != nil {
			return nil, err
		}
		for _, p := range res.Sessions {
			if ownTime(p.Source, p.ExternalID) || p.DurationS <= 0 {
				continue
			}
			e := store.Entry{Title: firstNonEmpty(p.Title, "Guitar practice"), StartedAt: p.StartedAt, EndedAt: endOf(p.StartedAt, p.EndedAt, p.DurationS),
				Date: p.Date, DurationS: p.DurationS, ExternalID: fmt.Sprintf("guitar:%d", p.ID), AppLabel: p.Title}
			if p.TabID != nil {
				e.AppRef, e.AppURL = fmt.Sprintf("tab:%d", *p.TabID), fmt.Sprintf("/tab/%d", *p.TabID)
			} else {
				e.AppURL = "/practice"
			}
			out = append(out, e)
		}
		if len(res.Sessions) < 1000 {
			break
		}
	}
	return out, nil
}

func (guitarAdapter) items(ctx context.Context, c *pocket.Client, q string) ([]item, error) {
	var res struct {
		Tabs []struct {
			ID     int64  `json:"id"`
			Title  string `json:"title"`
			Artist string `json:"artist"`
		} `json:"tabs"`
	}
	v := url.Values{"limit": {"40"}}
	if q != "" {
		v.Set("q", q)
	} else {
		v.Set("sort", "recent")
	}
	if err := c.Do(ctx, http.MethodGet, "/api/v1/tabs", v, nil, &res); err != nil {
		return nil, err
	}
	out := []item{{Ref: "", Label: "Free practice", Detail: "Not a particular song", URL: "/practice"}}
	for _, t := range res.Tabs {
		out = append(out, item{Ref: fmt.Sprintf("tab:%d", t.ID), Label: t.Title, Detail: t.Artist, URL: fmt.Sprintf("/tab/%d", t.ID)})
	}
	return out, nil
}

func (guitarAdapter) send(ctx context.Context, c *pocket.Client, e store.Entry) (string, error) {
	body := map[string]any{"title": firstNonEmpty(e.AppLabel, e.Title), "kind": "free", "started_at": e.StartedAt, "duration_s": e.DurationS,
		"note": e.Note, "external_id": fmt.Sprintf("time:%d", e.ID)}
	if id, ok := strings.CutPrefix(e.AppRef, "tab:"); ok {
		if n, err := strconv.ParseInt(id, 10, 64); err == nil {
			body["tab_id"], body["kind"] = n, "tab"
		}
	}
	var out struct {
		ID int64 `json:"id"`
	}
	if err := c.Do(ctx, http.MethodPost, "/api/v1/practice", nil, body, &out); err != nil {
		return "", err
	}
	return strconv.FormatInt(out.ID, 10), nil
}

func (guitarAdapter) unsend(ctx context.Context, c *pocket.Client, id string) error {
	return c.Do(ctx, http.MethodDelete, "/api/v1/practice/"+url.PathEscape(id), nil, nil, nil)
}

// ---- Pocket Still: breathing and meditation practices ----

type stillAdapter struct{}

var stillKinds = []item{
	{Ref: "kind:meditation", Label: "Meditation"}, {Ref: "kind:breath", Label: "Breathing"}, {Ref: "kind:body_scan", Label: "Body scan"},
	{Ref: "kind:loving_kindness", Label: "Loving-kindness"}, {Ref: "kind:walking", Label: "Mindful walk"}, {Ref: "kind:other", Label: "Other practice"},
}

func (stillAdapter) collect(ctx context.Context, c *pocket.Client, from, to string) ([]store.Entry, error) {
	var out []store.Entry
	for off := 0; off < 20000; off += 1000 {
		var res struct {
			Practices []struct {
				ID         int64  `json:"id"`
				Kind       string `json:"kind"`
				Title      string `json:"title"`
				StartedAt  string `json:"started_at"`
				EndedAt    string `json:"ended_at"`
				Date       string `json:"date"`
				DurationS  int    `json:"duration_s"`
				Source     string `json:"source"`
				ExternalID string `json:"external_id"`
			} `json:"practices"`
		}
		q := url.Values{"from": {from}, "to": {to}, "limit": {"1000"}, "offset": {strconv.Itoa(off)}}
		if err := c.Do(ctx, http.MethodGet, "/api/v1/practices", q, nil, &res); err != nil {
			return nil, err
		}
		for _, p := range res.Practices {
			if ownTime(p.Source, p.ExternalID) || p.DurationS <= 0 {
				continue
			}
			out = append(out, store.Entry{Title: firstNonEmpty(p.Title, "Practice"), StartedAt: p.StartedAt, EndedAt: endOf(p.StartedAt, p.EndedAt, p.DurationS),
				Date: p.Date, DurationS: p.DurationS, ExternalID: fmt.Sprintf("still:%d", p.ID),
				AppRef: "kind:" + p.Kind, AppLabel: p.Title, AppURL: fmt.Sprintf("/journal/%d", p.ID)})
		}
		if len(res.Practices) < 1000 {
			break
		}
	}
	return out, nil
}

func (stillAdapter) items(_ context.Context, _ *pocket.Client, q string) ([]item, error) {
	q = strings.ToLower(strings.TrimSpace(q))
	var out []item
	for _, k := range stillKinds {
		if q == "" || strings.Contains(strings.ToLower(k.Label), q) {
			out = append(out, k)
		}
	}
	return out, nil
}

func (stillAdapter) send(ctx context.Context, c *pocket.Client, e store.Entry) (string, error) {
	kind := strings.TrimPrefix(e.AppRef, "kind:")
	if kind == "" || kind == e.AppRef {
		kind = "meditation"
	}
	body := map[string]any{"kind": kind, "title": firstNonEmpty(e.Title, e.AppLabel), "started_at": e.StartedAt, "duration_s": e.DurationS,
		"completed": true, "note": e.Note, "external_id": fmt.Sprintf("time:%d", e.ID)}
	var out struct {
		ID int64 `json:"id"`
	}
	if err := c.Do(ctx, http.MethodPost, "/api/v1/practices", nil, body, &out); err != nil {
		return "", err
	}
	return strconv.FormatInt(out.ID, 10), nil
}

func (stillAdapter) unsend(ctx context.Context, c *pocket.Client, id string) error {
	return c.Do(ctx, http.MethodDelete, "/api/v1/practices/"+url.PathEscape(id), nil, nil, nil)
}

// ---- the worklog contract (Projects, Invoicing, Learn; docs/worklog.md) ----

type worklogAdapter struct{}

type worklogEntry struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Note       string `json:"note"`
	StartedAt  string `json:"started_at"`
	EndedAt    string `json:"ended_at"`
	Date       string `json:"date"`
	DurationS  int    `json:"duration_s"`
	Ref        string `json:"ref"`
	Label      string `json:"label"`
	URL        string `json:"url"`
	Source     string `json:"source"`
	ExternalID string `json:"external_id"`
}

func (worklogAdapter) collect(ctx context.Context, c *pocket.Client, from, to string) ([]store.Entry, error) {
	var res struct {
		Entries []worklogEntry `json:"entries"`
	}
	if err := c.Do(ctx, http.MethodGet, "/api/v1/worklog", url.Values{"from": {from}, "to": {to}}, nil, &res); err != nil {
		return nil, err
	}
	out := make([]store.Entry, 0, len(res.Entries))
	for _, w := range res.Entries {
		if ownTime(w.Source, w.ExternalID) || w.DurationS <= 0 || w.ID == "" {
			continue
		}
		start := w.StartedAt
		if _, ok := store.ParseTime(start); !ok {
			// only a day is known: noon that day
			if !validDate(w.Date) || w.Date == "" {
				continue
			}
			t, _ := time.ParseInLocation("2006-01-02 15:04", w.Date+" 12:00", time.Local)
			start = store.Stamp(t)
		}
		t, _ := store.ParseTime(start)
		date := firstNonEmpty(w.Date, store.LocalDate(t))
		out = append(out, store.Entry{Title: firstNonEmpty(w.Title, w.Label, "Logged time"), StartedAt: store.Stamp(t), EndedAt: endOf(start, w.EndedAt, w.DurationS),
			Date: date, DurationS: w.DurationS, ExternalID: "w:" + w.ID, AppRef: w.Ref, AppLabel: w.Label, AppURL: w.URL})
	}
	return out, nil
}

func (worklogAdapter) items(ctx context.Context, c *pocket.Client, q string) ([]item, error) {
	var res struct {
		Items []item `json:"items"`
	}
	v := url.Values{}
	if q != "" {
		v.Set("q", q)
	}
	if err := c.Do(ctx, http.MethodGet, "/api/v1/worklog/items", v, nil, &res); err != nil {
		return nil, err
	}
	return res.Items, nil
}

func (worklogAdapter) send(ctx context.Context, c *pocket.Client, e store.Entry) (string, error) {
	body := map[string]any{"ref": e.AppRef, "title": e.Title, "note": e.Note, "started_at": e.StartedAt, "ended_at": e.EndedAt, "date": e.Date,
		"duration_s": e.DurationS, "billable": e.Billable, "project": e.ProjectName, "external_id": fmt.Sprintf("time:%d", e.ID)}
	var out struct {
		ID string `json:"id"`
	}
	if err := c.Do(ctx, http.MethodPost, "/api/v1/worklog", nil, body, &out); err != nil {
		return "", err
	}
	return out.ID, nil
}

func (worklogAdapter) unsend(ctx context.Context, c *pocket.Client, id string) error {
	return c.Do(ctx, http.MethodDelete, "/api/v1/worklog/"+url.PathEscape(id), nil, nil, nil)
}

// ---- collecting ----

// collectDays is how far back a person's sibling time is collected.
func collectDays(st store.Settings) int {
	if st.CollectDays < 7 || st.CollectDays > 730 {
		return 90
	}
	return st.CollectDays
}

// collect reads the sibling apps' own time for one person (only = one app).
// Apps not connected or with collecting off are skipped quietly.
func (s *Server) collect(ctx context.Context, uid int64, only string) {
	s.syncMu.Lock()
	if s.syncing[uid] {
		s.syncMu.Unlock()
		return
	}
	s.syncing[uid] = true
	s.syncMu.Unlock()
	defer func() {
		s.syncMu.Lock()
		delete(s.syncing, uid)
		s.syncMu.Unlock()
	}()
	cl := s.clients(ctx, uid)
	to := store.Today()
	from := store.AddDays(to, -collectDays(cl.st))
	for _, a := range pocketApps {
		if only != "" && a.slug != only {
			continue
		}
		if !cl.st.Conn(a.slug).Collects() {
			continue
		}
		sib, err := cl.conn(ctx, a, a.read)
		if isNotConnected(err) {
			if mode, _, tok, _ := connSettings(cl.st, a); s.connMode(mode, tok) == modeOff {
				continue
			}
		}
		res := store.AppSync{App: a.slug, SyncedAt: store.Now()}
		if err == nil {
			var list []store.Entry
			cctx, cancel := context.WithTimeout(ctx, 45*time.Second)
			list, err = adapterFor(a.slug).collect(cctx, sib.client, from, to)
			cancel()
			if err == nil {
				res.Count, err = s.store.ReplaceCollected(ctx, uid, a.slug, from, to, list)
			}
		}
		if err != nil {
			res.Error = plainErr(a, err)
			s.log.Debug("collect", "app", a.slug, "user", uid, "err", err)
		}
		_ = s.store.SaveAppSync(ctx, uid, res)
	}
}

// plainErr says in plain words why talking to an app failed.
func plainErr(a pocketApp, err error) string {
	var nc *notConnected
	if errors.As(err, &nc) {
		return nc.msg
	}
	if errors.Is(err, pocket.ErrNotFound) {
		return "this " + a.name + " version cannot share its time yet (update it)"
	}
	msg := err.Error()
	if strings.Contains(msg, "HTTP 403") {
		return a.name + " did not allow it: switch the link on in Pocket Core → Links"
	}
	if strings.Contains(msg, "connection refused") || strings.Contains(msg, "no such host") || strings.Contains(msg, "deadline") {
		return a.name + " did not answer"
	}
	return msg
}

// ---- sending ----

// sendSoon sends a person's finished time in the background.
func (s *Server) sendSoon(uid int64) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		s.sendPending(ctx, uid)
	}()
}

// sendPending writes a person's finished entries that should go to an app.
func (s *Server) sendPending(ctx context.Context, uid int64) {
	s.sendMu.Lock()
	defer s.sendMu.Unlock()
	list, err := s.store.PendingDeliveries(ctx, uid, 14)
	if err != nil || len(list) == 0 {
		return
	}
	cl := s.clients(ctx, uid)
	for _, e := range list {
		a, ok := appBySlug(e.App)
		if !ok {
			_ = s.store.SetDelivery(ctx, uid, e.ID, "", "this app does not take time from Pocket Time")
			continue
		}
		sib, err := cl.conn(ctx, a, a.write)
		var remote string
		if err == nil {
			sctx, cancel := context.WithTimeout(ctx, 30*time.Second)
			remote, err = adapterFor(a.slug).send(sctx, sib.client, e)
			cancel()
		}
		msg := ""
		if err != nil {
			msg = plainErr(a, err)
			s.log.Debug("send", "app", a.slug, "entry", e.ID, "err", err)
		}
		_ = s.store.SetDelivery(ctx, uid, e.ID, remote, msg)
	}
}

// unsendLater removes, in the background, what an entry became in an app.
func (s *Server) unsendLater(uid int64, app, remoteID string) {
	a, ok := appBySlug(app)
	if !ok {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		sib, err := s.clients(ctx, uid).conn(ctx, a, a.write)
		if err == nil {
			err = adapterFor(app).unsend(ctx, sib.client, remoteID)
		}
		if err != nil {
			s.log.Debug("unsend", "app", app, "remote", remoteID, "err", err)
		}
	}()
}

// ---- the apps API ----

// appsView is, per sibling app, how the person reaches it and how collecting went.
func (s *Server) appsView(ctx context.Context, uid int64) []map[string]any {
	st, _ := s.store.GetSettings(ctx, uid)
	u := userFrom(ctx)
	if u == nil || u.ID != uid {
		u, _ = s.store.GetUser(ctx, uid)
	}
	syncs, _ := s.store.AppSyncs(ctx, uid)
	out := []map[string]any{}
	for _, a := range pocketApps {
		mode, via, detail := s.connStatus(ctx, u, st, a)
		_, _, _, public := connSettings(st, a)
		if public == "" && via != "" {
			public = s.resolveSibling(ctx, a.slug)
		}
		sy := syncs[a.slug]
		out = append(out, map[string]any{"slug": a.slug, "name": a.name, "item_name": a.itemsName, "reason": a.reason,
			"mode": mode, "via": via, "detail": detail, "connected": via != "", "collect": st.Conn(a.slug).Collects(),
			"synced_at": sy.SyncedAt, "sync_error": sy.Error, "count": sy.Count, "open_url": public})
	}
	return out
}

func (s *Server) handleApps(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	writeJSON(w, http.StatusOK, s.appsView(ctx, actorID(ctx)))
}

// handleSyncApps collects now (all apps, or ?app=slug) and returns the apps.
func (s *Server) handleSyncApps(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	cctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	s.collect(cctx, uid, r.URL.Query().Get("app"))
	s.sendPending(cctx, uid)
	writeJSON(w, http.StatusOK, s.appsView(ctx, uid))
}

// handleAppItems lists what time can be for in an app (?q= to search).
func (s *Server) handleAppItems(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	a, ok := appBySlug(chi.URLParam(r, "slug"))
	if !ok {
		writeErr(w, http.StatusNotFound, "unknown app")
		return
	}
	sib, err := s.clients(ctx, uid).conn(ctx, a, a.read)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"items": []item{}, "error": plainErr(a, err)})
		return
	}
	ictx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	list, err := adapterFor(a.slug).items(ictx, sib.client, strings.TrimSpace(r.URL.Query().Get("q")))
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"items": []item{}, "error": plainErr(a, err)})
		return
	}
	if list == nil {
		list = []item{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": list, "open_url": firstNonEmpty(sib.public, "")})
}
