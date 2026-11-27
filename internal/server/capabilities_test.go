package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-time/internal/config"
	"github.com/abjelosevic88/pocket-homelab-time/internal/store"
)

// capApp starts Time behind a fake Core whose pcx_<who>_<word> tokens are
// links acting for alice@ or bob@: search, day, widget and capture carry that
// one pocket:* scope (Pocket Hub), read and write the app's own scopes,
// export and import Pocket Move's. The second value is the manifest Time
// registered with last.
func capApp(t *testing.T) (*app, func() map[string]any) {
	t.Helper()
	var mu sync.Mutex
	var registered map[string]any
	coreSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v1/apps/register" {
			var m map[string]any
			_ = json.NewDecoder(r.Body).Decode(&m)
			mu.Lock()
			registered = m
			mu.Unlock()
		}
		if r.URL.Path != "/api/v1/auth/introspect" {
			io.WriteString(w, `{}`)
			return
		}
		var in struct {
			Token string `json:"token"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		parts := strings.SplitN(strings.TrimPrefix(in.Token, "pcx_"), "_", 2)
		scope := ""
		if len(parts) == 2 {
			scope = map[string]string{"search": core.ScopeSearch, "day": core.ScopeDay, "widget": core.ScopeWidget, "capture": core.ScopeCapture,
				"read": "time:read", "write": "time:write", "export": core.ScopeMoveExport, "import": core.ScopeMoveImport}[parts[1]]
		}
		if scope == "" || (parts[0] != "alice" && parts[0] != "bob") {
			io.WriteString(w, `{"active":false}`)
			return
		}
		email := parts[0] + "@example.com"
		fmt.Fprintf(w, `{"active":true,"kind":"delegated","scope":%q,"audience":"time","user":{"id":1,"sub":%q,"email":%q,"name":%q,"role":"member"},"app":{"id":8,"slug":"hub","name":"Pocket Hub"}}`, scope, email, email, parts[0])
	}))
	t.Cleanup(coreSrv.Close)
	a := newApp(t, func(c *config.Config) { c.CoreURL, c.CoreRegistrationKey = coreSrv.URL, "pcr_test" })
	return a, func() map[string]any {
		mu.Lock()
		defer mu.Unlock()
		return registered
	}
}

// project adds a project and returns its id.
func (a *app) project(auth, name, client string, billable bool) int64 {
	a.t.Helper()
	return int64(num(a.ok(auth, "POST", "/api/v1/projects", map[string]any{"name": name, "client": client, "billable": billable}), "id"))
}

// logTime adds a finished entry by hand and returns its id.
func (a *app) logTime(auth string, body map[string]any, startedAt string, minutes int) int64 {
	a.t.Helper()
	b := map[string]any{"started_at": startedAt, "duration_s": minutes * 60}
	for k, v := range body {
		b[k] = v
	}
	return int64(num(a.ok(auth, "POST", "/api/v1/entries", b), "id"))
}

// into reads a route's answer into v and returns the status.
func (a *app) into(auth, method, path string, v any) int {
	a.t.Helper()
	code, raw := a.raw(auth, method, path, "", nil)
	if v != nil {
		_ = json.Unmarshal(raw, v)
	}
	return code
}

func (a *app) search(auth, q string) []core.SearchItem {
	a.t.Helper()
	var res core.SearchResults
	if code := a.into(auth, "GET", "/api/v1/pocket/search?"+q, &res); code != 200 || res.Items == nil {
		a.t.Fatalf("search %s = %d %+v", q, code, res)
	}
	return res.Items
}

func searchTitles(items []core.SearchItem) string {
	var t []string
	for _, it := range items {
		t = append(t, it.Title)
	}
	return strings.Join(t, " | ")
}

// todayAt is a moment today, seconds after local midnight (so it is never in the future).
func todayAt(sec int) string {
	y, m, d := time.Now().Date()
	return time.Date(y, m, d, 0, 0, sec, 0, time.Local).Format(time.RFC3339)
}

// Pocket Search finds a person's projects and clients by name and their
// entries by what they were for, notes and tags, and says what each is.
func TestPocketSearch(t *testing.T) {
	a := newApp(t, nil)
	today := store.Today()
	week := weekStart(today, 1)
	web := a.project(a.alice, "Website", "Acme", true)
	mob := a.project(a.alice, "Mobile app", "Acme", false)
	old := a.project(a.alice, "Old website", "", false)
	a.ok(a.alice, "PUT", fmt.Sprintf("/api/v1/projects/%d", old), map[string]any{"archived": true})
	a.project(a.bob, "Secret website", "Acme", false)
	login := a.logTime(a.alice, map[string]any{"title": "Fix login", "project_id": web, "tags": "bugfix", "note": "the xyzzy cookie"}, todayAt(1), 90)
	copyID := a.logTime(a.alice, map[string]any{"title": "Write copy", "note": "text for the website"}, todayAt(2), 40)
	a.logTime(a.alice, map[string]any{"title": "Splash screen", "project_id": mob}, todayAt(3), 30)
	a.logTime(a.alice, map[string]any{"title": "Đurđevdan rehearsal"}, todayAt(4), 15)
	a.logTime(a.bob, map[string]any{"title": "Bob's website login"}, todayAt(1), 60)

	// by name: the projects first (time this week and month), then the entries that say it
	got := a.search(a.alice, "q=website")
	if searchTitles(got) != "Website | Old website | Write copy" {
		t.Fatalf("website = %s", searchTitles(got))
	}
	if it := got[0]; it.ID != fmt.Sprintf("project:%d", web) || it.Kind != "project" || it.URL != fmt.Sprintf("/entries?project=%d", web) ||
		it.Snippet != "Acme · 1 h 30 min this week, 1 h 30 min this month" || it.Date != today {
		t.Errorf("project = %+v", it)
	}
	if it := got[1]; it.Snippet != "nothing tracked this month · archived" || it.Date == "" {
		t.Errorf("archived project = %+v", it)
	}
	if it := got[2]; it.ID != fmt.Sprintf("entry:%d", copyID) || it.Kind != "entry" || it.Snippet != "40 min · today" ||
		it.URL != "/entries?week="+week || !strings.HasPrefix(it.Date, today) {
		t.Errorf("entry by its note = %+v", it)
	}
	// a client by name, with its projects and their time together
	got = a.search(a.alice, "q=ACME")
	if len(got) != 1 || got[0].ID != "client:acme" || got[0].Title != "Acme" || got[0].Kind != "client" || got[0].URL != "/projects" ||
		got[0].Snippet != "Mobile app, Website · 2 h this week, 2 h this month" || got[0].Date != today {
		t.Errorf("client = %+v", got)
	}
	// an entry by its tag and by its note: the line says its project, length and day, never the note
	got = a.search(a.alice, "q=bugfix")
	if len(got) != 1 || got[0].ID != fmt.Sprintf("entry:%d", login) || got[0].Title != "Fix login" || got[0].Snippet != "Website · 1 h 30 min · today" {
		t.Errorf("tag = %+v", got)
	}
	if got = a.search(a.alice, "q=xyzzy"); searchTitles(got) != "Fix login" || strings.Contains(got[0].Snippet, "xyzzy") {
		t.Errorf("note = %+v", got)
	}
	// entries found only by their project's name are not listed again
	if got = a.search(a.alice, "q=mobile"); searchTitles(got) != "Mobile app" {
		t.Errorf("mobile = %s", searchTitles(got))
	}
	// letters SQLite does not fold are found whatever the capitals
	if got = a.search(a.alice, "q=ĐURĐEVDAN"); searchTitles(got) != "Đurđevdan rehearsal" {
		t.Errorf("đurđevdan = %s", searchTitles(got))
	}
	// a timer that runs says so
	a.ok(a.alice, "POST", "/api/v1/timer/start", map[string]any{"title": "Deploy website", "project_id": web, "started_at": todayAt(5)})
	if got = a.search(a.alice, "q=deploy"); len(got) != 1 || got[0].Snippet != "Website · running now · today" {
		t.Errorf("running = %+v", got)
	}
	// the limit, and nothing to look for: only signs (LIKE's own among them)
	if got = a.search(a.alice, "q=website&limit=2"); searchTitles(got) != "Website | Old website" {
		t.Errorf("limit 2 = %s", searchTitles(got))
	}
	for _, q := range []string{"q=", "q=+", "q=%25", "q=_", "q=%3F%3F", "q=*"} {
		if got := a.search(a.alice, q); len(got) != 0 {
			t.Errorf("%s = %s", q, searchTitles(got))
		}
	}
	// a sign inside the words is that sign, not "any letter"
	if got = a.search(a.alice, "q=fix_login"); len(got) != 0 {
		t.Errorf("fix_login = %s", searchTitles(got))
	}
	// each person finds only their own
	for _, q := range []string{"q=secret", "q=bob", "q=website", "q=acme"} {
		for _, it := range a.search(a.alice, q) {
			if strings.Contains(it.Title, "Secret") || strings.Contains(it.Title, "Bob") {
				t.Errorf("Alice found Bob's %q with %s", it.Title, q)
			}
		}
	}
	if got = a.search(a.bob, "q=website"); searchTitles(got) != "Secret website | Bob's website login" {
		t.Errorf("Bob = %s", searchTitles(got))
	}
	if got = a.search(a.bob, "q=acme"); len(got) != 1 || got[0].Snippet != "Secret website · nothing tracked this month" {
		t.Errorf("Bob's client = %+v", got)
	}
	// GET /entries?q= is as it was (it finds by project name too)
	if l := a.ok(a.alice, "GET", "/api/v1/entries?q=mobile", nil); num(l, "total") != 1 {
		t.Errorf("/entries?q=mobile = %v", l["total"])
	}
	// days further back
	if got := dayWords(store.AddDays(today, -1), today); got != "yesterday" {
		t.Errorf("yesterday = %q", got)
	}
	if got := dayWords("2025-11-03", "2026-11-27"); got != "3 Nov 2025" {
		t.Errorf("last year = %q", got)
	}
	if got := dayWords("2026-11-03", "2026-11-27"); got != "Tue 3 Nov" {
		t.Errorf("this year = %q", got)
	}
}

func (a *app) dayOf(auth, date, tz string) core.Day {
	a.t.Helper()
	path := "/api/v1/pocket/day/" + date
	if tz != "" {
		path += "?tz=" + tz
	}
	var d core.Day
	if code := a.into(auth, "GET", path, &d); code != 200 || d.Date != date || d.Items == nil {
		a.t.Fatalf("day %s = %d %+v", date, code, d)
	}
	return d
}

func metricLine(m []core.Metric) string {
	var out []string
	for _, x := range m {
		out = append(out, x.Label+": "+x.Value)
	}
	return strings.Join(out, " | ")
}

// Pocket Day gets one line per entry with its project and client, and the
// day's total, as GET /day/{date} counts it.
func TestPocketDay(t *testing.T) {
	a := newApp(t, nil)
	const day = "2026-10-05"
	web := a.project(a.alice, "Website", "Acme", true)
	login := a.logTime(a.alice, map[string]any{"title": "Fix login", "project_id": web, "note": "private words"}, day+"T09:00:00Z", 90)
	a.logTime(a.alice, map[string]any{"title": "Write copy"}, day+"T11:00:00Z", 40)
	a.logTime(a.alice, map[string]any{"project_id": web}, day+"T13:00:00Z", 50)
	a.logTime(a.alice, map[string]any{"title": "Late fix"}, day+"T23:30:00Z", 20)
	a.logTime(a.bob, map[string]any{"title": "Bob's work"}, day+"T09:00:00Z", 30)

	d := a.dayOf(a.alice, day, "UTC")
	if d.Summary != "3 h 20 min tracked, mostly on Website" || len(d.Items) != 4 || metricLine(d.Metrics) != "Total: 3 h 20 min | Billable: 2 h 20 min" {
		t.Fatalf("day = %q %+v %+v", d.Summary, d.Items, d.Metrics)
	}
	week := weekStart(day, 1)
	if it := d.Items[0]; it.ID != strconv.FormatInt(login, 10) || it.Title != "Fix login" || it.Sub != "Website · Acme" || it.Kind != "tracked" || it.Minutes != 90 ||
		it.Time != "09:00" || it.End != "10:30" || it.Note != "" || it.URL != "/entries?week="+week {
		t.Errorf("entry = %+v", it)
	}
	if it := d.Items[1]; it.Title != "Write copy" || it.Sub != "" || it.Minutes != 40 {
		t.Errorf("no project = %+v", it)
	}
	if it := d.Items[2]; it.Title != "Website" || it.Sub != "Acme" || it.Time != "13:00" {
		t.Errorf("only a project = %+v", it)
	}

	// the caller's time zone decides the day and the clock: 23:30 UTC is the next morning in Tokyo
	d = a.dayOf(a.alice, day, "Asia/Tokyo")
	if len(d.Items) != 3 || d.Items[0].Time != "18:00" || d.Summary != "3 h tracked, mostly on Website" {
		t.Errorf("Tokyo = %q %+v", d.Summary, d.Items)
	}
	if d = a.dayOf(a.alice, "2026-10-06", "Asia/Tokyo"); len(d.Items) != 1 || d.Items[0].Time != "08:30" || d.Summary != "20 min tracked" || metricLine(d.Metrics) != "Total: 20 min" {
		t.Errorf("Tokyo, the next day = %q %+v %+v", d.Summary, d.Items, d.Metrics)
	}

	// without a time zone the day is the one the app keeps (the server's), as GET /day
	a.logTime(a.alice, map[string]any{"project_id": web, "title": "Standup"}, "2026-10-08T07:15:00", 15)
	if d = a.dayOf(a.alice, "2026-10-08", ""); d.Summary != "15 min tracked, all on Website" || len(d.Items) != 1 || d.Items[0].Time != "07:15" {
		t.Errorf("server's day = %q %+v", d.Summary, d.Items)
	}
	if old := a.ok(a.alice, "GET", "/api/v1/day/2026-10-08", nil); num(old, "minutes") != 15 || len(old["entries"].([]any)) != 1 {
		t.Errorf("/day = %v", old)
	}

	// the minutes are rounded as the reports round them
	a.ok(a.alice, "PUT", "/api/v1/settings", map[string]any{"rounding": 15})
	a.logTime(a.alice, map[string]any{"title": "Quick call"}, "2026-10-09T10:00:00Z", 7)
	if d = a.dayOf(a.alice, "2026-10-09", "UTC"); d.Items[0].Minutes != 15 || d.Summary != "15 min tracked" {
		t.Errorf("rounded = %q %+v", d.Summary, d.Items)
	}
	a.ok(a.alice, "PUT", "/api/v1/settings", map[string]any{"rounding": 0})

	// a timer that still runs is shown as such
	a.ok(a.alice, "POST", "/api/v1/timer/start", map[string]any{"title": "Deploy", "project_id": web, "started_at": todayAt(1)})
	d = a.dayOf(a.alice, store.Today(), "")
	last := d.Items[len(d.Items)-1]
	if last.Kind != "running" || last.Title != "Deploy" || last.Sub != "still running · Website · Acme" || last.End != "" || !strings.HasSuffix(d.Summary, "; a timer is still running") {
		t.Errorf("running = %q %+v", d.Summary, d.Items)
	}

	// nothing that day; nothing of Alice's on Bob's day; a bad date
	if d = a.dayOf(a.alice, "2026-01-01", "UTC"); d.Summary != "" || len(d.Items) != 0 || len(d.Metrics) != 0 {
		t.Errorf("empty day = %+v", d)
	}
	if d = a.dayOf(a.bob, day, "UTC"); d.Summary != "30 min tracked" || len(d.Items) != 1 || metricLine(d.Metrics) != "Total: 30 min" {
		t.Errorf("Bob's day = %q %+v", d.Summary, d.Items)
	}
	if code := a.into(a.alice, "GET", "/api/v1/pocket/day/yesterday", nil); code != 400 {
		t.Errorf("bad date = %d, want 400", code)
	}
}

// Pocket Hub's widget is the first page in a few lines.
func TestPocketWidget(t *testing.T) {
	a := newApp(t, nil)
	widget := func(auth string) core.WidgetData {
		t.Helper()
		var w core.WidgetData
		if code := a.into(auth, "GET", "/api/v1/pocket/widget", &w); code != 200 || w.Items == nil {
			t.Fatalf("widget = %d %+v", code, w)
		}
		return w
	}
	// never tracked anything: nothing to show
	if w := widget(a.alice); w.Line != "" || len(w.Items) != 0 || len(w.Metrics) != 0 || w.URL != "" {
		t.Errorf("empty = %+v", w)
	}
	// only time long ago: the numbers say nothing today
	a.logTime(a.alice, map[string]any{"title": "Ancient"}, "2025-01-06T10:00:00Z", 60)
	if w := widget(a.alice); w.Line != "Nothing tracked today" || len(w.Items) != 0 || metricLine(w.Metrics) != "Today: 0 min | This week: 0 min" || w.URL != "/" {
		t.Errorf("nothing today = %+v", w)
	}

	web := a.project(a.alice, "Website", "Acme", false)
	first := a.logTime(a.alice, map[string]any{"title": "Fix login", "project_id": web}, todayAt(1), 30)
	a.logTime(a.alice, map[string]any{"project_id": web}, todayAt(2), 45)
	a.logTime(a.bob, map[string]any{"title": "Bob's"}, todayAt(1), 50)
	w := widget(a.alice)
	if w.Line != "1 h 15 min tracked today" || metricLine(w.Metrics) != "Today: 1 h 15 min | This week: 1 h 15 min" || len(w.Items) != 2 {
		t.Fatalf("today = %+v", w)
	}
	// the newest first; an entry that is only its project does not say it twice
	if it := w.Items[0]; it.Title != "Website" || !strings.HasSuffix(it.Detail, ", 45 min") || strings.Contains(it.Detail, "Website") {
		t.Errorf("newest = %+v", it)
	}
	if it := w.Items[1]; it.ID != strconv.FormatInt(first, 10) || it.Title != "Fix login" || !strings.HasPrefix(it.Detail, "Website · 00:00–00:30") ||
		it.URL != "/entries?week="+weekStart(store.Today(), 1) {
		t.Errorf("first = %+v", it)
	}

	// a running timer comes first, with the targets next to the numbers
	a.ok(a.alice, "PUT", "/api/v1/settings", map[string]any{"daily_goal_min": 360, "weekly_goal_min": 1800})
	a.ok(a.alice, "POST", "/api/v1/timer/start", map[string]any{"title": "Deploy", "project_id": web, "started_at": todayAt(3)})
	w = widget(a.alice)
	if !strings.HasPrefix(w.Line, "Running: Deploy · ") || len(w.Items) != 3 || w.Items[0].Title != "Deploy" || !strings.HasPrefix(w.Items[0].Detail, "Website · running since 00:00") ||
		!strings.HasSuffix(w.Metrics[0].Value, " of 6 h") || !strings.HasSuffix(w.Metrics[1].Value, " of 30 h") {
		t.Errorf("running = %+v", w)
	}
	// Bob sees only his own
	if w = widget(a.bob); w.Line != "50 min tracked today" || len(w.Items) != 1 || w.Items[0].Title != "Bob's" {
		t.Errorf("Bob = %+v", w)
	}
}

// A link token with one of the pocket:* scopes reaches its one route (and
// /auth/me), as the person it acts for; the app's own scopes do not open them.
func TestPocketLinkScopes(t *testing.T) {
	a, _ := capApp(t)
	day := store.Today()
	a.logTime(a.bob, map[string]any{"title": "Bob website"}, todayAt(1), 15)
	a.logTime(a.alice, map[string]any{"title": "Secret website"}, todayAt(1), 15)
	type call struct{ m, p string }
	routes := map[string]call{
		"search": {"GET", "/api/v1/pocket/search?q=website"},
		"day":    {"GET", "/api/v1/pocket/day/" + day},
		"widget": {"GET", "/api/v1/pocket/widget"},
	}
	for scope := range routes {
		tok := "pcx_bob_" + scope
		for name, c := range routes {
			code, body := a.raw(tok, c.m, c.p, "", nil)
			switch {
			case name == scope && (code != 200 || strings.Contains(string(body), "Secret") || !strings.Contains(string(body), "Bob website")):
				t.Errorf("pocket:%s %s %s = %d %s", scope, c.m, c.p, code, body)
			case name != scope && (code != 403 || !strings.Contains(string(body), "link does not cover")):
				t.Errorf("pocket:%s on %s %s = %d %s, want 403", scope, c.m, c.p, code, body)
			}
		}
		var me map[string]any
		if code := a.into(tok, "GET", "/api/v1/auth/me", &me); code != 200 || me["email"] != "bob@example.com" {
			t.Errorf("%s auth/me = %d %v", tok, code, me)
		}
		// nothing else in the app, and never the timer
		for _, c := range []call{
			{"GET", "/api/v1/timer"}, {"POST", "/api/v1/timer/start"}, {"POST", "/api/v1/timer/stop"}, {"GET", "/api/v1/entries"}, {"POST", "/api/v1/entries"},
			{"GET", "/api/v1/projects"}, {"GET", "/api/v1/dashboard"}, {"GET", "/api/v1/day/" + day}, {"GET", "/api/v1/report"}, {"GET", "/api/v1/settings"},
			{"GET", "/api/v1/export.json"}, {"GET", core.PortablePath},
		} {
			if code, _ := a.raw(tok, c.m, c.p, "application/json", []byte("{}")); code != 403 {
				t.Errorf("%s %s %s = %d, want 403", tok, c.m, c.p, code)
			}
		}
		// and its own route in no other way
		for _, c := range []call{
			{"POST", "/api/v1/pocket/search?q=e"}, {"POST", "/api/v1/pocket/widget"}, {"DELETE", "/api/v1/pocket/day/" + day}, {"GET", "/api/v1/pocket/day/" + day + "/more"},
			{"POST", "/api/v1/pocket/capture"},
		} {
			if code, _ := a.raw(tok, c.m, c.p, "application/json", []byte("{}")); code != 403 && code != 404 && code != 405 {
				t.Errorf("%s %s %s = %d", tok, c.m, c.p, code)
			}
		}
	}
	// the links the app had before do not open the new routes, and there is no quick line to keep
	for _, tok := range []string{"pcx_bob_read", "pcx_bob_write", "pcx_bob_export", "pcx_bob_import", "pcx_bob_capture"} {
		for _, c := range routes {
			if code, _ := a.raw(tok, c.m, c.p, "", nil); code != 403 {
				t.Errorf("%s %s %s = %d, want 403", tok, c.m, c.p, code)
			}
		}
		if code, _ := a.raw(tok, "POST", "/api/v1/pocket/capture", "application/json", []byte(`{"request_id":"hub-0000000000000001","text":"1h30 website"}`)); code != 403 && code != 404 {
			t.Errorf("%s capture = %d, want no such route", tok, code)
		}
	}
	if code, _ := a.raw(a.bob, "POST", "/api/v1/pocket/capture", "application/json", []byte(`{"request_id":"hub-0000000000000001","text":"1h30 website"}`)); code != 404 && code != 405 {
		t.Errorf("capture as the person = %d, want no such route", code)
	}
	// the app's own links still open what they did
	if code, _ := a.raw("pcx_bob_read", "GET", "/api/v1/day/"+day, "", nil); code != 200 {
		t.Errorf("time:read on /day = %d", code)
	}
	// a token Core does not vouch for
	if code, _ := a.raw("pcx_carol_widget", "GET", "/api/v1/pocket/widget", "", nil); code != 401 {
		t.Errorf("unknown link token = %d, want 401", code)
	}
	// nothing was added or changed along the way, and no timer runs
	if l := a.ok(a.bob, "GET", "/api/v1/entries", nil); num(l, "total") != 1 {
		t.Errorf("Bob has %v entries", l["total"])
	}
	if tm := a.ok(a.bob, "GET", "/api/v1/timer", nil); tm["running"] != nil {
		t.Errorf("a timer runs: %v", tm)
	}
}

// The manifest tells Pocket Core the shelf Hub puts the app on and what
// Search, Day and Hub can ask it.
func TestManifestCapabilities(t *testing.T) {
	a, manifest := capApp(t)
	if err := a.s.registerWithCore(context.Background()); err != nil {
		t.Fatal(err)
	}
	m := manifest()
	if m["slug"] != "time" || m["category"] != "Work" {
		t.Fatalf("manifest: %v %v", m["slug"], m["category"])
	}
	search, _ := m["search"].(map[string]any)
	day, _ := m["day"].(map[string]any)
	widget, _ := m["widget"].(map[string]any)
	if search == nil || day == nil || widget == nil {
		t.Fatalf("capabilities: %v %v %v", search, day, widget)
	}
	if _, has := m["capture"]; has {
		t.Errorf("capture: %v", m["capture"])
	}
	if fmt.Sprint(search["bangs"]) != "[time timer]" || fmt.Sprint(search["kinds"]) != "[project client entry]" || search["what"] == "" {
		t.Errorf("search: %v", search)
	}
	if day["label"] != "Time" || day["one"] != "entry" || day["many"] != "entries" || day["unit"] != "min" || day["what"] == "" {
		t.Errorf("day: %v", day)
	}
	if widget["title"] != "Time" || widget["what"] == "" {
		t.Errorf("widget: %v", widget)
	}
	offered := map[string]bool{}
	for _, o := range m["offers"].([]any) {
		offered[o.(map[string]any)["scope"].(string)] = true
	}
	for _, sc := range []string{"time:read", "time:write", core.ScopeSearch, core.ScopeDay, core.ScopeWidget, core.ScopeMoveExport, core.ScopeMoveImport} {
		if !offered[sc] {
			t.Errorf("%s is not offered", sc)
		}
	}
	if offered[core.ScopeCapture] {
		t.Error("pocket:capture is offered, but there is nothing to keep a quick line as")
	}
}

// Without Pocket Core the routes answer the person signed in to the app.
func TestPocketRoutesWithoutCore(t *testing.T) {
	a := newApp(t, nil)
	web := a.project(a.alice, "Website", "", false)
	a.logTime(a.alice, map[string]any{"title": "Fix login", "project_id": web}, "2026-10-05T09:00:00Z", 15)
	if got := a.search(a.alice, "q=login"); len(got) != 1 || got[0].Snippet != "Website · 15 min · Mon 5 Oct" {
		t.Errorf("search = %+v", got)
	}
	if d := a.dayOf(a.alice, "2026-10-05", "UTC"); d.Summary != "15 min tracked, all on Website" {
		t.Errorf("day = %+v", d)
	}
	var w core.WidgetData
	if code := a.into(a.alice, "GET", "/api/v1/pocket/widget", &w); code != 200 || len(w.Metrics) != 2 || w.Line != "Nothing tracked today" {
		t.Errorf("widget = %d %+v", code, w)
	}
	// a link token means nothing here, and nobody is nobody
	for _, tok := range []string{"pcx_alice_widget", ""} {
		if code, _ := a.raw(tok, "GET", "/api/v1/pocket/widget", "", nil); code != 401 {
			t.Errorf("%q = %d, want 401", tok, code)
		}
	}
}

// The capability scopes open their routes and nothing else; the app's own still do theirs.
func TestCapabilityLinkRoutes(t *testing.T) {
	for _, c := range []struct {
		method, path string
		scopes       []string
		want         bool
	}{
		{"GET", "/api/v1/pocket/search", []string{core.ScopeSearch}, true},
		{"GET", "/api/v1/pocket/day/2026-10-30", []string{core.ScopeDay}, true},
		{"GET", "/api/v1/pocket/widget", []string{core.ScopeWidget}, true},
		{"POST", "/api/v1/pocket/capture", []string{core.ScopeCapture}, false},
		{"GET", "/api/v1/pocket/widget", []string{"time:read", "time:write"}, false},
		{"GET", "/api/v1/day/2026-10-30", []string{core.ScopeDay}, false},
		{"POST", "/api/v1/timer/start", []string{core.ScopeWidget, core.ScopeDay, core.ScopeSearch}, false},
		{"GET", "/api/v1/day/2026-10-30", []string{"time:read"}, true},
	} {
		r := httptest.NewRequest(c.method, c.path, nil)
		if got := linkAllows(c.scopes, r); got != c.want {
			t.Errorf("%s %s with %v = %v, want %v", c.method, c.path, c.scopes, got, c.want)
		}
	}
}
