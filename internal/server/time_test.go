package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/abjelosevic88/pocket-homelab-time/internal/config"
	"github.com/abjelosevic88/pocket-homelab-time/internal/store"
)

type app struct {
	t     *testing.T
	url   string
	s     *Server
	st    *store.Store
	alice string
	bob   string
	ids   map[string]int64
}

func newApp(t *testing.T, tweak func(*config.Config)) *app {
	t.Helper()
	cfg := testConfig(t.TempDir())
	cfg.BaseURL = "http://time.test"
	cfg.MaxBodyBytes = 64 << 20
	if tweak != nil {
		tweak(cfg)
	}
	st := openTestStore(t, cfg)
	ctx := context.Background()
	a := &app{t: t, st: st, ids: map[string]int64{}}
	for _, u := range []*store.User{{Email: "alice@example.com", Name: "Alice", Role: "admin"}, {Email: "bob@example.com", Name: "Bob", Role: "member"}} {
		if err := st.CreateUser(ctx, u); err != nil {
			t.Fatal(err)
		}
		tok, _, _ := st.CreateAPIToken(ctx, u.ID, "t")
		a.ids[u.Name] = u.ID
		if u.Name == "Alice" {
			a.alice = tok
		} else {
			a.bob = tok
		}
	}
	ctx, cancel := context.WithCancel(ctx)
	t.Cleanup(cancel)
	a.s = New(cfg, st, slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	if err := a.s.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(a.s.Router())
	t.Cleanup(ts.Close)
	a.url = ts.URL
	return a
}

func (a *app) call(auth, method, path string, body any) (int, map[string]any, []any) {
	a.t.Helper()
	var rd io.Reader
	if body != nil {
		j, _ := json.Marshal(body)
		rd = bytes.NewReader(j)
	}
	req, _ := http.NewRequest(method, a.url+path, rd)
	req.Header.Set("Content-Type", "application/json")
	if auth != "" {
		req.Header.Set("Authorization", "Bearer "+auth)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		a.t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	var m map[string]any
	var l []any
	if json.Unmarshal(raw, &m) != nil {
		_ = json.Unmarshal(raw, &l)
	}
	return res.StatusCode, m, l
}

func (a *app) ok(auth, method, path string, body any) map[string]any {
	a.t.Helper()
	code, m, _ := a.call(auth, method, path, body)
	if code >= 300 {
		a.t.Fatalf("%s %s → %d %v", method, path, code, m)
	}
	return m
}

func (a *app) expect(want int, auth, method, path string, body any) map[string]any {
	a.t.Helper()
	code, m, _ := a.call(auth, method, path, body)
	if code != want {
		a.t.Fatalf("%s %s → %d, want %d (%v)", method, path, code, want, m)
	}
	return m
}

func num(m map[string]any, k string) float64 { v, _ := m[k].(float64); return v }

func sub(m map[string]any, k string) map[string]any { v, _ := m[k].(map[string]any); return v }

// asLink calls a handler the way a linked Pocket app (pcx_ token) reaches it.
func (a *app) asLink(slug, who string, h http.HandlerFunc, method, path string, body any, params map[string]string) (int, map[string]any) {
	a.t.Helper()
	j, _ := json.Marshal(body)
	req := httptest.NewRequest(method, path, bytes.NewReader(j))
	u, err := a.st.GetUser(context.Background(), a.ids[who])
	if err != nil {
		a.t.Fatal(err)
	}
	rc := chi.NewRouteContext()
	for k, v := range params {
		rc.URLParams.Add(k, v)
	}
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rc)
	ctx = context.WithValue(ctx, userKey, u)
	ctx = context.WithValue(ctx, linkAppKey, slug)
	if !linkAllows([]string{"time:read", "time:write"}, req) {
		return http.StatusForbidden, nil
	}
	rec := httptest.NewRecorder()
	h(rec, req.WithContext(ctx))
	var m map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &m)
	return rec.Code, m
}

func TestTimerOneAtATime(t *testing.T) {
	a := newApp(t, nil)
	r := a.ok(a.alice, "POST", "/api/v1/timer/start", map[string]any{"title": "Write report"})
	first := sub(r, "running")
	if first["title"] != "Write report" || first["running"] != true {
		t.Fatalf("start = %v", r)
	}
	// a second start without switching is refused and names the running one
	m := a.expect(409, a.alice, "POST", "/api/v1/timer/start", map[string]any{"title": "Email"})
	if sub(m, "running")["title"] != "Write report" {
		t.Fatalf("409 should carry the running timer: %v", m)
	}
	// switching stops the first at the second's start
	r = a.ok(a.alice, "POST", "/api/v1/timer/start", map[string]any{"title": "Email", "switch": true})
	if sub(r, "stopped")["title"] != "Write report" || sub(r, "stopped")["ended_at"] == "" || sub(r, "running")["title"] != "Email" {
		t.Fatalf("switch = %v", r)
	}
	// stopping with only_if for the wrong entry is refused
	a.expect(409, a.alice, "POST", "/api/v1/timer/stop", map[string]any{"only_if": num(first, "id")})
	r = a.ok(a.alice, "POST", "/api/v1/timer/stop", nil)
	if sub(r, "stopped")["title"] != "Email" || r["running"] != nil {
		t.Fatalf("stop = %v", r)
	}
	if g := a.ok(a.alice, "GET", "/api/v1/timer", nil); g["running"] != nil {
		t.Fatalf("nothing should run: %v", g)
	}
	// stopping again is harmless
	a.ok(a.alice, "POST", "/api/v1/timer/stop", nil)
	l := a.ok(a.alice, "GET", "/api/v1/entries", nil)
	if num(l, "total") != 2 {
		t.Fatalf("entries = %v", l)
	}
}

func TestPerPersonIsolation(t *testing.T) {
	a := newApp(t, nil)
	p := a.ok(a.alice, "POST", "/api/v1/projects", map[string]any{"name": "Client A", "billable": true, "rate": 50})
	pid := int(num(p, "id"))
	e := a.ok(a.alice, "POST", "/api/v1/entries", map[string]any{"title": "Design", "project_id": pid, "started_at": time.Now().Add(-2 * time.Hour).Format(time.RFC3339), "duration_s": 3600})
	eid := int(num(e, "id"))
	if e["billable"] != true || e["project_name"] != "Client A" {
		t.Fatalf("entry = %v", e)
	}
	a.ok(a.alice, "POST", "/api/v1/timer/start", map[string]any{"title": "Alice runs"})

	// Bob sees none of it and cannot touch it
	a.expect(404, a.bob, "GET", fmt.Sprintf("/api/v1/entries/%d", eid), nil)
	a.expect(404, a.bob, "PUT", fmt.Sprintf("/api/v1/entries/%d", eid), map[string]any{"title": "x"})
	a.expect(404, a.bob, "DELETE", fmt.Sprintf("/api/v1/entries/%d", eid), nil)
	a.expect(404, a.bob, "POST", fmt.Sprintf("/api/v1/entries/%d/continue", eid), nil)
	a.expect(404, a.bob, "PUT", fmt.Sprintf("/api/v1/projects/%d", pid), map[string]any{"name": "x"})
	a.expect(404, a.bob, "DELETE", fmt.Sprintf("/api/v1/projects/%d", pid), nil)
	// nor use her project
	a.expect(400, a.bob, "POST", "/api/v1/timer/start", map[string]any{"title": "Bob", "project_id": pid})
	if l := a.ok(a.bob, "GET", "/api/v1/entries", nil); num(l, "total") != 0 {
		t.Fatalf("bob sees %v", l)
	}
	if g := a.ok(a.bob, "GET", "/api/v1/timer", nil); g["running"] != nil {
		t.Fatalf("bob sees alice's timer: %v", g)
	}
	_, _, projects := a.call(a.bob, "GET", "/api/v1/projects", nil)
	if len(projects) != 0 {
		t.Fatalf("bob sees projects %v", projects)
	}
	// Bob has his own timer at the same time
	a.ok(a.bob, "POST", "/api/v1/timer/start", map[string]any{"title": "Bob runs"})
	if g := a.ok(a.alice, "GET", "/api/v1/timer", nil); sub(g, "running")["title"] != "Alice runs" {
		t.Fatalf("alice's timer = %v", g)
	}
	rep := a.ok(a.bob, "GET", "/api/v1/report", nil)
	if num(rep, "count") != 1 {
		t.Fatalf("bob's report counts %v", rep["count"])
	}
	day := a.ok(a.bob, "GET", "/api/v1/day/"+store.Today(), nil)
	for _, l := range day["entries"].([]any) {
		if l.(map[string]any)["title"] != "Bob runs" {
			t.Fatalf("bob's day shows %v", l)
		}
	}
}

func TestLinkedAppSharesTheTimer(t *testing.T) {
	a := newApp(t, nil)
	// Pocket Guitar starts a timer for a song
	code, m := a.asLink("guitar", "Alice", a.s.handleStartTimer, "POST", "/api/v1/timer/start",
		map[string]any{"app_ref": "tab:42", "app_label": "Wonderwall", "app_url": "/tab/42", "app": "projects"}, nil)
	if code != 200 {
		t.Fatalf("link start → %d %v", code, m)
	}
	run := sub(m, "running")
	if run["source"] != "guitar" || run["app"] != "guitar" || run["deliver"] != true || run["title"] != "Wonderwall" {
		t.Fatalf("linked timer = %v", run)
	}
	// Pocket Time sees it running
	if g := a.ok(a.alice, "GET", "/api/v1/timer", nil); sub(g, "running")["app_ref"] != "tab:42" {
		t.Fatalf("time sees %v", g)
	}
	// Pocket Projects cannot start another without switching
	code, m = a.asLink("projects", "Alice", a.s.handleStartTimer, "POST", "/api/v1/timer/start", map[string]any{"app_ref": "issue:ATH-1"}, nil)
	if code != 409 || sub(m, "running")["app"] != "guitar" {
		t.Fatalf("second link start → %d %v", code, m)
	}
	// stopping in Pocket Time stops it for Guitar too
	a.ok(a.alice, "POST", "/api/v1/timer/stop", nil)
	code, m = a.asLink("guitar", "Alice", a.s.handleGetTimer, "GET", "/api/v1/timer", nil, nil)
	if code != 200 || m["running"] != nil {
		t.Fatalf("guitar sees %d %v", code, m)
	}
	// a linked app changes only what it logged
	e := a.ok(a.alice, "POST", "/api/v1/entries", map[string]any{"title": "Mine", "started_at": time.Now().Add(-time.Hour).Format(time.RFC3339), "duration_s": 600, "note": "private"})
	id := fmt.Sprint(int(num(e, "id")))
	code, _ = a.asLink("guitar", "Alice", a.s.handleUpdateEntry, "PUT", "/api/v1/entries/"+id, map[string]any{"title": "x"}, map[string]string{"id": id})
	if code != 403 {
		t.Fatalf("link edit of someone else's entry → %d", code)
	}
	code, m = a.asLink("guitar", "Alice", a.s.handleGetEntry, "GET", "/api/v1/entries/"+id, nil, map[string]string{"id": id})
	if code != 200 || m["note"] != "" {
		t.Fatalf("a linked app must not see notes: %d %v", code, m)
	}
	// the same app log twice adds one entry
	for i := 0; i < 2; i++ {
		code, m = a.asLink("guitar", "Alice", a.s.handleCreateEntry, "POST", "/api/v1/entries",
			map[string]any{"title": "Lesson", "started_at": time.Now().Add(-3 * time.Hour).Format(time.RFC3339), "duration_s": 1800, "external_id": "lesson-1"}, nil)
		if code != 200 {
			t.Fatalf("link log → %d %v", code, m)
		}
	}
	if l := a.ok(a.alice, "GET", "/api/v1/entries?source=guitar", nil); num(l, "total") != 2 {
		t.Fatalf("guitar entries = %v", l["total"])
	}
}

func TestLinkRoutes(t *testing.T) {
	cases := []struct {
		method, path string
		scopes       []string
		want         bool
	}{
		{"GET", "/api/v1/timer", []string{"time:read"}, true},
		{"POST", "/api/v1/timer/start", []string{"time:read"}, false},
		{"POST", "/api/v1/timer/start", []string{"time:write"}, true},
		{"POST", "/api/v1/timer/stop", []string{"time:write"}, true},
		{"POST", "/api/v1/timer/discard", []string{"time:write"}, false},
		{"PUT", "/api/v1/timer", []string{"time:write"}, false},
		{"GET", "/api/v1/entries", []string{"time:write"}, true},
		{"GET", "/api/v1/day/2026-10-30", []string{"time:read"}, true},
		{"DELETE", "/api/v1/entries/5", []string{"time:read"}, false},
		{"DELETE", "/api/v1/entries/5", []string{"time:write"}, true},
		{"GET", "/api/v1/settings", []string{"time:read", "time:write"}, false},
		{"GET", "/api/v1/export.json", []string{"time:read"}, false},
		{"GET", "/api/v1/auth/me", nil, true},
	}
	for _, c := range cases {
		r := httptest.NewRequest(c.method, c.path, nil)
		if got := linkAllows(c.scopes, r); got != c.want {
			t.Errorf("%s %s with %v = %v, want %v", c.method, c.path, c.scopes, got, c.want)
		}
	}
}

func TestCollectedTimeIsReadOnlyAndReplaced(t *testing.T) {
	a := newApp(t, nil)
	ctx := context.Background()
	uid := a.ids["Alice"]
	today := store.Today()
	mk := func(ext, title string, min int) store.Entry {
		st := time.Now().Add(-2 * time.Hour)
		return store.Entry{Title: title, StartedAt: store.Stamp(st), EndedAt: store.Stamp(st.Add(time.Duration(min) * time.Minute)), Date: today, DurationS: min * 60, ExternalID: ext}
	}
	n, err := a.st.ReplaceCollected(ctx, uid, "guitar", store.AddDays(today, -7), today, []store.Entry{mk("guitar:1", "Blackbird", 20), mk("guitar:2", "Wonderwall", 15)})
	if err != nil || n != 2 {
		t.Fatal(n, err)
	}
	// again with one changed and one gone
	if _, err := a.st.ReplaceCollected(ctx, uid, "guitar", store.AddDays(today, -7), today, []store.Entry{mk("guitar:1", "Blackbird", 25)}); err != nil {
		t.Fatal(err)
	}
	l := a.ok(a.alice, "GET", "/api/v1/entries?app=guitar", nil)
	list := l["entries"].([]any)
	if len(list) != 1 || num(list[0].(map[string]any), "duration_s") != 1500 {
		t.Fatalf("collected = %v", list)
	}
	id := int(num(list[0].(map[string]any), "id"))
	a.expect(409, a.alice, "PUT", fmt.Sprintf("/api/v1/entries/%d", id), map[string]any{"title": "x"})
	a.expect(409, a.alice, "DELETE", fmt.Sprintf("/api/v1/entries/%d", id), nil)
	// Bob does not get Alice's collected time
	if l := a.ok(a.bob, "GET", "/api/v1/entries?app=guitar", nil); num(l, "total") != 0 {
		t.Fatalf("bob sees collected %v", l)
	}
	// switching collecting off clears it
	a.ok(a.alice, "PUT", "/api/v1/settings", map[string]any{"guitar_collect": false, "guitar_mode": "off"})
	if l := a.ok(a.alice, "GET", "/api/v1/entries?app=guitar", nil); num(l, "total") != 0 {
		t.Fatalf("collected left after switching off: %v", l)
	}
}

func TestReportRoundingAndBillable(t *testing.T) {
	a := newApp(t, nil)
	p := a.ok(a.alice, "POST", "/api/v1/projects", map[string]any{"name": "Client", "billable": true, "rate": 60})
	start := time.Now().Add(-5 * time.Hour)
	a.ok(a.alice, "POST", "/api/v1/entries", map[string]any{"title": "a", "project_id": num(p, "id"), "started_at": start.Format(time.RFC3339), "duration_s": 61 * 60})
	a.ok(a.alice, "POST", "/api/v1/entries", map[string]any{"title": "b", "started_at": start.Add(2 * time.Hour).Format(time.RFC3339), "duration_s": 10 * 60})
	a.ok(a.alice, "PUT", "/api/v1/settings", map[string]any{"rounding": 15})
	d := start.Format("2006-01-02")
	rep := a.ok(a.alice, "GET", "/api/v1/report?from="+d+"&to="+d, nil)
	if num(rep, "seconds") != (75+15)*60 || num(rep, "billable_s") != 75*60 || num(rep, "amount") != 75 {
		t.Fatalf("report = %v %v %v", rep["seconds"], rep["billable_s"], rep["amount"])
	}
	a.expect(400, a.alice, "PUT", "/api/v1/settings", map[string]any{"rounding": 7})
}

func TestExportImportIdempotent(t *testing.T) {
	a := newApp(t, nil)
	p := a.ok(a.alice, "POST", "/api/v1/projects", map[string]any{"name": "Book"})
	a.ok(a.alice, "POST", "/api/v1/entries", map[string]any{"title": "Chapter 1", "project_id": num(p, "id"), "started_at": time.Now().Add(-3 * time.Hour).Format(time.RFC3339), "duration_s": 1200})
	doc := a.ok(a.alice, "GET", "/api/v1/export.json", nil)
	for i := 0; i < 2; i++ {
		a.ok(a.bob, "POST", "/api/v1/import", doc)
		_ = i
	}
	if l := a.ok(a.bob, "GET", "/api/v1/entries", nil); num(l, "total") != 1 {
		t.Fatalf("bob has %v entries after two imports", l["total"])
	}
	_, _, projects := a.call(a.bob, "GET", "/api/v1/projects", nil)
	if len(projects) != 1 {
		t.Fatalf("bob has %d projects", len(projects))
	}
}

func TestImportTogglCSV(t *testing.T) {
	a := newApp(t, nil)
	csv := "User,Email,Client,Project,Task,Description,Billable,Start date,Start time,End date,End time,Duration,Tags\n" +
		"Ana,ana@x,Acme,Website,,Homepage,Yes,2026-10-01,09:00:00,2026-10-01,10:30:00,01:30:00,design\n" +
		"Ana,ana@x,Acme,Website,,Review,No,2026-10-02,14:00:00,2026-10-02,14:45:00,00:45:00,\n"
	for i := 0; i < 2; i++ {
		req, _ := http.NewRequest("POST", a.url+"/api/v1/import/csv", strings.NewReader(csv))
		req.Header.Set("Authorization", "Bearer "+a.alice)
		res, err := http.DefaultClient.Do(req)
		if err != nil || res.StatusCode != 200 {
			t.Fatal(err, res.StatusCode)
		}
		res.Body.Close()
	}
	l := a.ok(a.alice, "GET", "/api/v1/entries?from=2026-10-01&to=2026-10-02", nil)
	if num(l, "total") != 2 {
		t.Fatalf("csv import twice = %v entries", l["total"])
	}
	first := l["entries"].([]any)[1].(map[string]any)
	if first["title"] != "Homepage" || num(first, "duration_s") != 5400 || first["project_name"] != "Website" || first["billable"] != true {
		t.Fatalf("row = %v", first)
	}
}

func TestSettingsArePersonal(t *testing.T) {
	a := newApp(t, nil)
	a.ok(a.alice, "PUT", "/api/v1/settings", map[string]any{"daily_goal_min": 300, "guitar_mode": "direct", "guitar_url": "http://g/", "guitar_token": "pgt_secret_token_1234"})
	b := a.ok(a.bob, "GET", "/api/v1/settings", nil)
	if num(b, "daily_goal_min") != 0 || b["guitar_url"] != "" || b["guitar_token"] != "" {
		t.Fatalf("bob got alice's settings: %v %v %v", b["daily_goal_min"], b["guitar_url"], b["guitar_token"])
	}
	m := a.ok(a.alice, "GET", "/api/v1/settings", nil)
	if m["guitar_url"] != "http://g" || m["guitar_token"] == "pgt_secret_token_1234" || num(m, "daily_goal_min") != 300 {
		t.Fatalf("alice's settings = %v %v", m["guitar_url"], m["guitar_token"])
	}
}

func TestDemoSeeds(t *testing.T) {
	a := newApp(t, func(c *config.Config) { c.DemoData = true })
	ctx := context.Background()
	owner := a.st.OwnerID(ctx)
	if err := a.s.seedDemo(ctx, owner); err != nil {
		t.Fatal(err)
	}
	d := a.ok(a.alice, "GET", "/api/v1/dashboard", nil)
	if d["running"] == nil || num(d, "week_s") == 0 {
		t.Fatalf("demo dashboard = running %v, week %v", d["running"], d["week_s"])
	}
}

func TestDirectAppSource(t *testing.T) {
	a := newApp(t, nil)
	r := a.ok(a.alice, "POST", "/api/v1/timer/start", map[string]any{"app": "guitar", "source": "guitar", "app_ref": "tab:7", "app_label": "Blackbird"})
	run := sub(r, "running")
	if run["source"] != "guitar" || run["app_ref"] != "tab:7" || run["deliver"] != true {
		t.Fatalf("direct start = %v", run)
	}
	a.ok(a.alice, "POST", "/api/v1/timer/stop", nil)
	// a source that is not the app is ignored
	r = a.ok(a.alice, "POST", "/api/v1/timer/start", map[string]any{"app": "guitar", "source": "projects", "app_ref": "tab:7"})
	if sub(r, "running")["source"] != "time" {
		t.Fatalf("mismatched source taken: %v", sub(r, "running"))
	}
}
