package server

import (
	"context"
	"encoding/csv"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/abjelosevic88/pocket-homelab-time/internal/store"
)

// ---- reports: where the time went ----

// rounded is an entry's length for reports: rounded up to the person's step.
func rounded(d, step int) int {
	if step <= 0 || d <= 0 {
		return d
	}
	s := step * 60
	return int(math.Ceil(float64(d)/float64(s))) * s
}

// weekStart is the first day of the week containing date (week_start 1 = Monday, 7 = Sunday, 6 = Saturday).
func weekStart(date string, ws int) string {
	t, err := store.ParseDate(date)
	if err != nil {
		return date
	}
	first := time.Monday
	switch ws {
	case 7:
		first = time.Sunday
	case 6:
		first = time.Saturday
	}
	back := (int(t.Weekday()) - int(first) + 7) % 7
	return t.AddDate(0, 0, -back).Format("2006-01-02")
}

type group struct {
	Key       string  `json:"key"`
	Label     string  `json:"label"`
	Color     string  `json:"color,omitempty"`
	App       string  `json:"app,omitempty"`
	Seconds   int     `json:"seconds"`
	BillableS int     `json:"billable_s"`
	Amount    float64 `json:"amount"`
	Count     int     `json:"count"`
	GoalMin   int     `json:"goal_min,omitempty"`
}

type dayTotal struct {
	Date    string         `json:"date"`
	Seconds int            `json:"seconds"`
	By      map[string]int `json:"by"`
}

type report struct {
	From      string     `json:"from"`
	To        string     `json:"to"`
	Group     string     `json:"group"`
	Seconds   int        `json:"seconds"`
	BillableS int        `json:"billable_s"`
	Amount    float64    `json:"amount"`
	Currency  string     `json:"currency"`
	Count     int        `json:"count"`
	Days      []dayTotal `json:"days"`
	Groups    []group    `json:"groups"`
	Rounding  int        `json:"rounding"`
}

// groupKey says which group an entry falls in.
func groupKey(e store.Entry, by string) (key, label, color, app string) {
	switch by {
	case "app":
		if e.App == "" {
			return "time", "Pocket Time", "", ""
		}
		return e.App, appName(e.App), "", e.App
	case "item":
		if e.App != "" {
			k := e.App + "|" + e.AppRef
			return k, firstNonEmpty(e.AppLabel, e.Title, appName(e.App)), "", e.App
		}
		return "title|" + strings.ToLower(e.Title), firstNonEmpty(e.Title, "Untitled"), e.ProjectColor, ""
	case "tag":
		tags := store.SplitTags(e.Tags)
		if len(tags) == 0 {
			return "", "No tag", "", ""
		}
		return tags[0], tags[0], "", ""
	case "title":
		return strings.ToLower(e.Title), firstNonEmpty(e.Title, "Untitled"), "", ""
	}
	if e.ProjectID == nil {
		return "none", "No project", "", ""
	}
	return strconv.FormatInt(*e.ProjectID, 10), e.ProjectName, e.ProjectColor, ""
}

// buildReport adds up a person's entries between from and to.
func (s *Server) buildReport(ctx context.Context, uid int64, from, to, by string, f store.EntryFilter) (*report, []store.Entry, error) {
	st, _ := s.store.GetSettings(ctx, uid)
	f.From, f.To, f.Asc = from, to, true
	list, _, err := s.store.ListEntries(ctx, uid, f)
	if err != nil {
		return nil, nil, err
	}
	projects, _ := s.store.ListProjects(ctx, uid, true)
	rates := map[int64]store.Project{}
	for _, p := range projects {
		rates[p.ID] = p
	}
	rep := &report{From: from, To: to, Group: by, Currency: st.Currency, Days: []dayTotal{}, Groups: []group{}, Rounding: st.Rounding}
	days := map[string]*dayTotal{}
	for d := from; d <= to; d = store.AddDays(d, 1) {
		dt := &dayTotal{Date: d, By: map[string]int{}}
		days[d] = dt
		rep.Days = append(rep.Days, *dt)
		if len(rep.Days) > 800 {
			break
		}
	}
	groups := map[string]*group{}
	for _, e := range list {
		sec := rounded(e.DurationS, st.Rounding)
		k, label, color, app := groupKey(e, by)
		g := groups[k]
		if g == nil {
			g = &group{Key: k, Label: label, Color: color, App: app}
			if by == "project" && e.ProjectID != nil {
				g.GoalMin = rates[*e.ProjectID].GoalMin
			}
			groups[k] = g
		}
		g.Seconds += sec
		g.Count++
		rep.Seconds += sec
		rep.Count++
		if e.Billable {
			g.BillableS += sec
			rep.BillableS += sec
			if e.ProjectID != nil {
				amt := float64(sec) / 3600 * rates[*e.ProjectID].Rate
				g.Amount += amt
				rep.Amount += amt
			}
		}
		if dt := days[e.Date]; dt != nil {
			dt.Seconds += sec
			dt.By[k] += sec
		}
	}
	for i := range rep.Days {
		rep.Days[i] = *days[rep.Days[i].Date]
	}
	for _, g := range groups {
		g.Amount = math.Round(g.Amount*100) / 100
		rep.Groups = append(rep.Groups, *g)
	}
	sort.Slice(rep.Groups, func(i, j int) bool {
		if rep.Groups[i].Seconds != rep.Groups[j].Seconds {
			return rep.Groups[i].Seconds > rep.Groups[j].Seconds
		}
		return rep.Groups[i].Label < rep.Groups[j].Label
	})
	rep.Amount = math.Round(rep.Amount*100) / 100
	return rep, list, nil
}

func reportFilter(r *http.Request) store.EntryFilter {
	q := r.URL.Query()
	f := store.EntryFilter{App: q.Get("app"), Q: q.Get("q")}
	if v := q.Get("project_id"); v != "" {
		f.ProjectID, _ = strconv.ParseInt(v, 10, 64)
		if v == "none" {
			f.ProjectID = -1
		}
	}
	return f
}

func (s *Server) handleReport(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	from, to := dateRange(r, 7)
	by := r.URL.Query().Get("group")
	switch by {
	case "project", "app", "item", "tag", "title":
	default:
		by = "project"
	}
	rep, _, err := s.buildReport(ctx, actorID(ctx), from, to, by, reportFilter(r))
	if err != nil {
		s.fail(w, err, "report")
		return
	}
	writeJSON(w, http.StatusOK, rep)
}

// handleReportCSV writes a period's entries as CSV (what a client or a spreadsheet wants).
func (s *Server) handleReportCSV(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	from, to := dateRange(r, 7)
	_, list, err := s.buildReport(ctx, actorID(ctx), from, to, "project", reportFilter(r))
	if err != nil {
		s.fail(w, err, "report")
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="pocket-time-`+from+`-`+to+`.csv"`)
	writeEntriesCSV(w, list)
}

func writeEntriesCSV(w http.ResponseWriter, list []store.Entry) {
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"date", "start", "end", "hours", "minutes", "title", "project", "tags", "billable", "app", "for", "note", "source", "id"})
	for _, e := range list {
		start, end := "", ""
		if t, ok := store.ParseTime(e.StartedAt); ok {
			start = t.Format("15:04")
		}
		if t, ok := store.ParseTime(e.EndedAt); ok {
			end = t.Format("15:04")
		}
		app := ""
		if e.App != "" {
			app = appName(e.App)
		}
		_ = cw.Write([]string{e.Date, start, end, strconv.FormatFloat(round2(float64(e.DurationS)/3600), 'f', -1, 64), strconv.Itoa(e.DurationS / 60),
			e.Title, e.ProjectName, e.Tags, strconv.FormatBool(e.Billable), app, e.AppLabel, e.Note, e.Source, strconv.FormatInt(e.ID, 10)})
	}
	cw.Flush()
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }

// handleDashboard is the home screen: the running timer, today, this week.
func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	st, _ := s.store.GetSettings(ctx, uid)
	today := store.Today()
	ws := weekStart(today, st.WeekStart)
	we := store.AddDays(ws, 6)
	week, _, err := s.buildReport(ctx, uid, ws, we, "project", store.EntryFilter{})
	if err != nil {
		s.fail(w, err, "dashboard")
		return
	}
	byApp, _, _ := s.buildReport(ctx, uid, ws, we, "app", store.EntryFilter{})
	todayS := 0
	for _, d := range week.Days {
		if d.Date == today {
			todayS = d.Seconds
		}
	}
	var running any
	if e, err := s.store.RunningEntry(ctx, uid); err == nil {
		running = forLink(ctx, []store.Entry{*e})[0] // counted in the totals so far
	}
	recentFrom := store.AddDays(today, -6)
	recent, _, _ := s.store.ListEntries(ctx, uid, store.EntryFilter{From: recentFrom, To: today, Limit: 300})
	writeJSON(w, http.StatusOK, map[string]any{
		"today": today, "week_from": ws, "week_to": we, "today_s": todayS, "week_s": week.Seconds,
		"daily_goal_min": st.DailyGoalMin, "weekly_goal_min": st.WeeklyGoalMin,
		"days": week.Days, "projects": week.Groups, "apps": byApp.Groups,
		"running": running, "recent": forLink(ctx, recent),
	})
}

// handleDay is one day for Pocket Day and other apps: the total, what it
// was spent on, and the entries (without notes for a linked app).
func (s *Server) handleDay(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	date := chi.URLParam(r, "date")
	if !validDate(date) || date == "" {
		writeErr(w, http.StatusBadRequest, "a date is YYYY-MM-DD")
		return
	}
	uid := actorID(ctx)
	rep, list, err := s.buildReport(ctx, uid, date, date, "project", store.EntryFilter{})
	if err != nil {
		s.fail(w, err, "day")
		return
	}
	apps, _, _ := s.buildReport(ctx, uid, date, date, "app", store.EntryFilter{})
	type line struct {
		Title     string `json:"title"`
		Project   string `json:"project"`
		App       string `json:"app"`
		AppName   string `json:"app_name"`
		For       string `json:"for"`
		StartedAt string `json:"started_at"`
		EndedAt   string `json:"ended_at"`
		DurationS int    `json:"duration_s"`
	}
	lines := []line{}
	for _, e := range list {
		an := ""
		if e.App != "" {
			an = appName(e.App)
		}
		lines = append(lines, line{e.Title, e.ProjectName, e.App, an, e.AppLabel, e.StartedAt, e.EndedAt, e.DurationS})
	}
	text := ""
	if rep.Seconds > 0 {
		parts := []string{}
		for i, g := range rep.Groups {
			if i == 3 {
				break
			}
			parts = append(parts, g.Label+" "+fmtDur(g.Seconds))
		}
		text = fmtDur(rep.Seconds) + " tracked: " + strings.Join(parts, ", ")
	}
	writeJSON(w, http.StatusOK, map[string]any{"date": date, "seconds": rep.Seconds, "minutes": rep.Seconds / 60, "summary": text,
		"projects": rep.Groups, "apps": apps.Groups, "entries": lines})
}

// fmtDur is "2 h 05 min" / "35 min".
func fmtDur(sec int) string {
	m := (sec + 30) / 60
	if m < 60 {
		return strconv.Itoa(m) + " min"
	}
	if m%60 == 0 {
		return strconv.Itoa(m/60) + " h"
	}
	return strconv.Itoa(m/60) + " h " + pad2(m%60) + " min"
}

func pad2(n int) string {
	if n < 10 {
		return "0" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}
