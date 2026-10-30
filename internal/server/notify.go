package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-time/internal/notify"
	"github.com/abjelosevic88/pocket-homelab-time/internal/store"
)

// pushes runs every minute: "your timer is still running" after the
// person's chosen number of hours (once per timer), and the weekly report
// at their chosen day and hour.
func (s *Server) pushes(ctx context.Context) {
	now := time.Now()
	// long-running timers
	if list, err := s.store.LongRunning(ctx, now.Add(-time.Hour)); err == nil {
		for _, e := range list {
			st, err := s.store.GetSettings(ctx, e.UserID)
			if err != nil || st.NotifyURL == "" || !st.NotifyLong || st.LongTimerH <= 0 {
				continue
			}
			if e.DurationS < st.LongTimerH*3600 {
				continue
			}
			key := fmt.Sprintf("push:%d:long:%d", e.UserID, e.ID)
			if _, _, done := s.store.CacheGet(ctx, key); done {
				continue
			}
			_ = s.store.CachePut(ctx, key, "1")
			where := ""
			if e.Source != "time" && e.Source != "manual" {
				where = " (started in " + appName(e.Source) + ")"
			}
			_ = notify.New(st.NotifyURL, st.NotifyFormat).Send(ctx, notify.Message{Title: "Your timer is still running",
				Body:     fmt.Sprintf("%s has been running for %s%s. Stop it if you forgot.", entryName(e), fmtDur(e.DurationS), where),
				Priority: "info", Tags: []string{"stopwatch"}, Link: s.cfg.BaseURL + "/"})
		}
	}
	// the weekly report
	if now.Minute() >= 5 {
		return
	}
	ids, err := s.store.SettingsUsers(ctx)
	if err != nil {
		return
	}
	wd := int(now.Weekday())
	if wd == 0 {
		wd = 7
	}
	today := store.Today()
	for _, uid := range ids {
		st, err := s.store.GetSettings(ctx, uid)
		if err != nil || st.NotifyURL == "" || !st.NotifyWeekly || st.WeeklyDay != wd || st.WeeklyHour != now.Hour() {
			continue
		}
		key := fmt.Sprintf("push:%d:%s:weekly", uid, today)
		if _, _, done := s.store.CacheGet(ctx, key); done {
			continue
		}
		_ = s.store.CachePut(ctx, key, "1")
		body := s.weeklyReport(ctx, uid, st)
		if body == "" {
			continue
		}
		_ = notify.New(st.NotifyURL, st.NotifyFormat).Send(ctx, notify.Message{Title: "Your week in time", Body: body, Priority: "info", Tags: []string{"stopwatch"}, Link: s.cfg.BaseURL + "/reports"})
	}
}

// weeklyReport is last week (the seven days before the current week) in a few lines.
func (s *Server) weeklyReport(ctx context.Context, uid int64, st store.Settings) string {
	ws := weekStart(store.Today(), st.WeekStart)
	from, to := store.AddDays(ws, -7), store.AddDays(ws, -1)
	rep, _, err := s.buildReport(ctx, uid, from, to, "project", store.EntryFilter{})
	if err != nil || rep.Seconds == 0 {
		return ""
	}
	apps, _, _ := s.buildReport(ctx, uid, from, to, "app", store.EntryFilter{})
	var b strings.Builder
	fmt.Fprintf(&b, "Last week you tracked %s.", fmtDur(rep.Seconds))
	if st.WeeklyGoalMin > 0 {
		pct := rep.Seconds * 100 / (st.WeeklyGoalMin * 60)
		fmt.Fprintf(&b, " That is %d%% of your weekly target.", pct)
	}
	var parts []string
	for i, g := range rep.Groups {
		if i == 4 {
			break
		}
		parts = append(parts, g.Label+" "+fmtDur(g.Seconds))
	}
	b.WriteString("\nBy project: " + strings.Join(parts, ", ") + ".")
	parts = nil
	for _, g := range apps.Groups {
		if g.Key != "time" {
			parts = append(parts, g.Label+" "+fmtDur(g.Seconds))
		}
	}
	if len(parts) > 0 {
		b.WriteString("\nFrom your apps: " + strings.Join(parts, ", ") + ".")
	}
	if rep.BillableS > 0 {
		fmt.Fprintf(&b, "\nBillable: %s.", fmtDur(rep.BillableS))
	}
	return b.String()
}

// handleWeeklyNow sends the weekly report now (Settings → Notifications).
func (s *Server) handleWeeklyNow(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	st, _ := s.store.GetSettings(ctx, uid)
	if st.NotifyURL == "" {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "add a notification address first"})
		return
	}
	body := s.weeklyReport(ctx, uid, st)
	if body == "" {
		body = "Nothing tracked last week."
	}
	if err := notify.New(st.NotifyURL, st.NotifyFormat).Send(ctx, notify.Message{Title: "Your week in time", Body: body, Priority: "info", Tags: []string{"stopwatch"}, Link: s.cfg.BaseURL + "/reports"}); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
