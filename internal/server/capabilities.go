package server

import (
	"context"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-time/internal/store"
)

// What Pocket Search, Pocket Day and Pocket Hub ask Time (the capabilities
// value in links.go): one route each, in the shape every Pocket app answers
// in. They read what GET /entries, /day and /dashboard read, as the request's
// person, with the person's rounding and week start; nothing here changes
// anything, and nothing starts or stops the timer. Notes are looked in by
// search but never shown, as for the app's own links.

// entriesPath opens the week an entry is in on the Entries page.
func entriesPath(date string, ws int) string { return "/entries?week=" + weekStart(date, ws) }

// projectPath opens a project's entries (the list button on the Projects page).
func projectPath(id int64) string { return "/entries?project=" + strconv.FormatInt(id, 10) }

// dots joins the parts of a line that are there: "Website · 1 h 30 min · today".
func dots(parts ...string) string {
	out := parts[:0:0]
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, " · ")
}

// displayName is what an entry is called on a line: what it was for, else
// the item in its app, its project, its app.
func displayName(e store.Entry) string {
	app := ""
	if e.App != "" {
		app = appName(e.App)
	}
	return firstNonEmpty(e.Title, e.AppLabel, e.ProjectName, app, "Untitled")
}

// dayWords says which day a date is, next to a line: "today", "yesterday",
// "Tue 3 Nov", "3 Nov 2025".
func dayWords(date, today string) string {
	d, err := store.ParseDate(date)
	if err != nil {
		return ""
	}
	switch date {
	case today:
		return "today"
	case store.AddDays(today, -1):
		return "yesterday"
	}
	if date[:4] != today[:4] {
		return d.Format("2 Jan 2006")
	}
	return d.Format("Mon 2 Jan")
}

// clockOf is a stored moment as "15:04" in loc ("" when it is not one).
func clockOf(stamp string, loc *time.Location) string {
	if t, ok := store.ParseTime(stamp); ok {
		return t.In(loc).Format("15:04")
	}
	return ""
}

// ---------- search ----------

// spent is the time a project (or a client's projects) took lately.
type spent struct{ week, month int }

// words: "3 h 20 min this week, 12 h this month", "nothing tracked this month".
func (s spent) words() string {
	switch {
	case s.week > 0 && s.month > 0:
		return fmtDur(s.week) + " this week, " + fmtDur(s.month) + " this month"
	case s.month > 0:
		return fmtDur(s.month) + " this month"
	case s.week > 0:
		return fmtDur(s.week) + " this week"
	}
	return "nothing tracked this month"
}

// recentSpent adds up the person's time per project this week and this
// month, rounded as reports round it, and the last day each project was
// worked on in that time.
func (s *Server) recentSpent(ctx context.Context, uid int64, st store.Settings) (map[int64]spent, map[int64]string, error) {
	today := store.Today()
	ws, ms := weekStart(today, st.WeekStart), today[:8]+"01"
	from := ws
	if ms < from {
		from = ms
	}
	list, _, err := s.store.ListEntries(ctx, uid, store.EntryFilter{From: from, To: today})
	if err != nil {
		return nil, nil, err
	}
	out, last := map[int64]spent{}, map[int64]string{}
	for _, e := range list {
		if e.ProjectID == nil {
			continue
		}
		id, sec := *e.ProjectID, rounded(e.DurationS, st.Rounding)
		sp := out[id]
		if e.Date >= ws {
			sp.week += sec
		}
		if e.Date >= ms {
			sp.month += sec
		}
		out[id] = sp
		if e.Date > last[id] {
			last[id] = e.Date
		}
	}
	return out, last, nil
}

// pocketSearch answers Pocket Search with the person's projects and clients
// whose name has the words, and the entries GET /entries?q= finds by what
// they were for, their notes and tags (not those found only by their
// project's name: the project itself is the answer then). Projects come
// first, then clients, then entries, the newest first.
func (s *Server) pocketSearch(r *http.Request, q string, limit int) ([]core.SearchItem, error) {
	ctx := r.Context()
	// nothing to look for in "??" or "*"; in SQL's LIKE "%" and "_" would find everything
	if strings.IndexFunc(q, func(c rune) bool { return unicode.IsLetter(c) || unicode.IsDigit(c) }) < 0 {
		return nil, nil
	}
	uid := actorID(ctx)
	st, _ := s.store.GetSettings(ctx, uid)
	needle := strings.ToLower(q)
	has := func(v ...string) bool { return strings.Contains(strings.ToLower(strings.Join(v, "\n")), needle) }
	out := []core.SearchItem{}

	projects, err := s.store.ListProjects(ctx, uid, true)
	if err != nil {
		return nil, err
	}
	spentBy, lastBy, err := s.recentSpent(ctx, uid, st)
	if err != nil {
		return nil, err
	}
	type client struct {
		name     string
		projects []string
		spent    spent
		last     string
	}
	clients := map[string]*client{}
	var clientOrder []string
	for _, p := range projects {
		sp := spentBy[p.ID]
		if p.Client != "" {
			k := strings.ToLower(p.Client)
			c := clients[k]
			if c == nil {
				c = &client{name: p.Client}
				clients[k] = c
				clientOrder = append(clientOrder, k)
			}
			c.projects = append(c.projects, p.Name)
			c.spent.week += sp.week
			c.spent.month += sp.month
			if lastBy[p.ID] > c.last {
				c.last = lastBy[p.ID]
			}
		}
		if !has(p.Name) {
			continue
		}
		archived := ""
		if p.Archived {
			archived = "archived"
		}
		out = append(out, core.SearchItem{ID: "project:" + strconv.FormatInt(p.ID, 10), Title: p.Name, Kind: "project",
			Snippet: dots(p.Client, sp.words(), archived), Date: firstNonEmpty(lastBy[p.ID], p.UpdatedAt), URL: projectPath(p.ID), Score: 3})
	}
	for _, k := range clientOrder {
		c := clients[k]
		if !has(c.name) {
			continue
		}
		names := strings.Join(c.projects, ", ")
		if len(c.projects) > 3 {
			names = strings.Join(c.projects[:3], ", ") + " and " + strconv.Itoa(len(c.projects)-3) + " more"
		}
		out = append(out, core.SearchItem{ID: "client:" + k, Title: c.name, Kind: "client", Snippet: dots(names, c.spent.words()), Date: c.last, URL: "/projects", Score: 2})
	}

	// SQLite's LIKE ignores capitals only in plain ASCII, so words with
	// other letters ("Đurđevdan") are looked for here, in the newest entries
	f := store.EntryFilter{Q: q, Limit: 200}
	if strings.IndexFunc(q, func(c rune) bool { return c > unicode.MaxASCII }) >= 0 {
		f.Q, f.Limit = "", 2000
	}
	entries, _, err := s.store.ListEntries(ctx, uid, f)
	if err != nil {
		return nil, err
	}
	today, found := store.Today(), 0
	for _, e := range entries {
		if found == limit {
			break
		}
		if !has(e.Title, e.Note, e.Tags, e.AppLabel) {
			continue
		}
		found++
		length := fmtDur(e.DurationS)
		if e.Running {
			length = "running now"
		}
		out = append(out, core.SearchItem{ID: "entry:" + strconv.FormatInt(e.ID, 10), Title: displayName(e), Kind: "entry",
			Snippet: dots(e.ProjectName, length, dayWords(e.Date, today)), Date: e.StartedAt, URL: entriesPath(e.Date, st.WeekStart), Score: 1})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// ---------- day ----------

// pocketDay answers Pocket Day with what GET /day/{date} reports: one line
// per entry with its project and client, the minutes as reports round them,
// a timer that is still running marked so, and the day's total. The app
// keeps each entry's day as the server saw it; a caller that gives its time
// zone gets the entries that started on its own day, with their times in
// its clock.
func (s *Server) pocketDay(r *http.Request, date string, tz *time.Location) (*core.Day, error) {
	ctx := r.Context()
	uid := actorID(ctx)
	st, _ := s.store.GetSettings(ctx, uid)
	loc := time.Local
	f := store.EntryFilter{From: date, To: date, Asc: true}
	if tz != nil {
		loc = tz
		f.From, f.To = store.AddDays(date, -1), store.AddDays(date, 1)
	}
	list, _, err := s.store.ListEntries(ctx, uid, f)
	if err != nil {
		return nil, err
	}
	projects, err := s.store.ListProjects(ctx, uid, true)
	if err != nil {
		return nil, err
	}
	clientOf := map[int64]string{}
	for _, p := range projects {
		clientOf[p.ID] = p.Client
	}

	day := &core.Day{Date: date}
	total, billable := 0, 0
	byProject := map[string]int{}
	var order []string
	running := false
	for _, e := range list {
		start, ok := store.ParseTime(e.StartedAt)
		if tz != nil && (!ok || start.In(loc).Format("2006-01-02") != date) {
			continue
		}
		sec := rounded(e.DurationS, st.Rounding)
		total += sec
		if e.Billable {
			billable += sec
		}
		if e.ProjectName != "" {
			if _, seen := byProject[e.ProjectName]; !seen {
				order = append(order, e.ProjectName)
			}
			byProject[e.ProjectName] += sec
		}
		// the line under the title: project · client, and the app the time is kept in
		title, project, client, app, kind := displayName(e), e.ProjectName, "", "", "tracked"
		if e.ProjectID != nil {
			client = clientOf[*e.ProjectID]
		}
		if project == title {
			project = ""
		}
		if e.App != "" {
			app = appName(e.App)
		}
		if e.Running {
			running, kind = true, "running"
			project = dots("still running", project)
		}
		day.Items = append(day.Items, core.DayItem{ID: strconv.FormatInt(e.ID, 10), Time: clockOf(e.StartedAt, loc), End: clockOf(e.EndedAt, loc), Title: title,
			Sub: dots(project, client, app), Kind: kind, Minutes: float64((sec + 30) / 60), URL: entriesPath(e.Date, st.WeekStart)})
	}
	if len(day.Items) == 0 {
		return nil, nil
	}

	// "6 h 20 min tracked, mostly on Website"; time without a project names none
	sort.SliceStable(order, func(i, j int) bool { return byProject[order[i]] > byProject[order[j]] })
	parts := []string{fmtDur(total) + " tracked"}
	if total < 30 {
		parts[0] = "less than a minute tracked" // a timer just started
	}
	switch {
	case len(order) == 0:
	case len(order) == 1 && byProject[order[0]] == total:
		parts[0] += ", all on " + order[0]
	case byProject[order[0]]*2 >= total:
		parts[0] += ", mostly on " + order[0]
	}
	if running {
		parts = append(parts, "a timer is still running")
	}
	day.Summary = strings.Join(parts, "; ")
	day.Metrics = []core.Metric{{Label: "Total", Value: fmtDur(total)}}
	if billable > 0 {
		day.Metrics = append(day.Metrics, core.Metric{Label: "Billable", Value: fmtDur(billable)})
	}
	return day, nil
}

// ---------- widget ----------

// pocketWidget answers Pocket Hub with the first page in a few lines: the
// timer that is running, else today's total; today and this week as the
// dashboard counts them (with the person's targets); today's entries, the
// newest first. Someone who never tracked anything gets nothing to show.
func (s *Server) pocketWidget(r *http.Request) (*core.WidgetData, error) {
	ctx := r.Context()
	uid := actorID(ctx)
	st, _ := s.store.GetSettings(ctx, uid)
	today := store.Today()
	ws := weekStart(today, st.WeekStart)
	week, _, err := s.buildReport(ctx, uid, ws, store.AddDays(ws, 6), "project", store.EntryFilter{})
	if err != nil {
		return nil, err
	}
	todayS := 0
	for _, d := range week.Days {
		if d.Date == today {
			todayS = d.Seconds
		}
	}
	rows, _, err := s.store.ListEntries(ctx, uid, store.EntryFilter{From: today, To: today, Limit: 8})
	if err != nil {
		return nil, err
	}
	if week.Seconds == 0 && len(rows) == 0 {
		if ever, _, err := s.store.ListEntries(ctx, uid, store.EntryFilter{Limit: 1}); err != nil || len(ever) == 0 {
			return nil, err
		}
	}

	of := func(sec, goalMin int) string {
		if goalMin > 0 {
			return fmtDur(sec) + " of " + fmtDur(goalMin*60)
		}
		return fmtDur(sec)
	}
	w := &core.WidgetData{URL: "/", Metrics: []core.Metric{
		{Label: "Today", Value: of(todayS, st.DailyGoalMin)},
		{Label: "This week", Value: of(week.Seconds, st.WeeklyGoalMin)},
	}}
	run, err := s.store.RunningEntry(ctx, uid)
	switch {
	case err == nil:
		w.Line = "Running: " + dots(displayName(*run), fmtDur(run.DurationS))
	case todayS > 0:
		w.Line = fmtDur(todayS) + " tracked today"
	default:
		w.Line = "Nothing tracked today"
	}
	for _, e := range rows {
		length := fmtDur(e.DurationS)
		if e.Running {
			length = "running since " + clockOf(e.StartedAt, time.Local)
		} else if from := clockOf(e.StartedAt, time.Local); from != "" {
			length = from + "–" + clockOf(e.EndedAt, time.Local) + ", " + length
		}
		project := e.ProjectName
		if project == displayName(e) {
			project = ""
		}
		w.Items = append(w.Items, core.WidgetItem{ID: strconv.FormatInt(e.ID, 10), Title: displayName(e), Detail: dots(project, length), URL: entriesPath(e.Date, st.WeekStart)})
	}
	return w, nil
}
