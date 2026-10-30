package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-time/internal/ai"
	"github.com/abjelosevic88/pocket-homelab-time/internal/notify"
	"github.com/abjelosevic88/pocket-homelab-time/internal/pocket"
	"github.com/abjelosevic88/pocket-homelab-time/internal/store"
)

// ---- settings ----

func mask(s string) string {
	if s == "" {
		return ""
	}
	if len(s) <= 8 {
		return "••••"
	}
	return s[:4] + "••••" + s[len(s)-4:]
}

// publicSettings is what one person sees: their settings with the secrets
// masked and, per Pocket app, the mode in effect (<app>_mode), whether it
// works and how (<app>_via: core | direct | "") and why not (<app>_detail).
func (s *Server) publicSettings(ctx context.Context, u *store.User, st store.Settings) map[string]any {
	out := map[string]any{
		"core_configured": s.core.Configured(), "core_registered": s.coreStatus()["registered"],
		"title": st.Title, "date_format": st.DateFormat, "time_format": st.TimeFormat, "week_start": st.WeekStart,
		"daily_goal_min": st.DailyGoalMin, "weekly_goal_min": st.WeeklyGoalMin, "rounding": st.Rounding, "currency": st.Currency,
		"default_billable": st.DefaultBill, "switch_ask": st.SwitchAsk, "long_timer_h": st.LongTimerH, "title_in_tab": st.TitleInTab,
		"collect_days": collectDays(st),
		"notify_long":  st.NotifyLong, "notify_weekly": st.NotifyWeekly, "weekly_day": st.WeeklyDay, "weekly_hour": st.WeeklyHour,
		"notify_url": st.NotifyURL, "notify_format": st.NotifyFormat, "notify_configured": st.NotifyURL != "",
		"ai_provider": st.AIProvider, "ai_pocket_model": st.AIPocketModel, "openai_api_key": mask(st.OpenAIKey), "openai_model": st.OpenAIModel, "openai_base_url": st.OpenAIBaseURL,
		"setup_complete": st.SetupComplete, "base_url": s.cfg.BaseURL,
	}
	for _, a := range pocketApps {
		c := st.Conn(a.slug)
		mode, via, detail := s.connStatus(ctx, u, st, a)
		out[a.slug+"_mode"], out[a.slug+"_via"], out[a.slug+"_detail"] = mode, via, detail
		out[a.slug+"_configured"] = via != ""
		out[a.slug+"_url"], out[a.slug+"_token"], out[a.slug+"_public_url"] = c.URL, mask(c.Token), c.PublicURL
		out[a.slug+"_collect"] = c.Collects()
		public := c.PublicURL
		if public == "" && via != "" {
			public = s.resolveSibling(ctx, a.slug)
		}
		out[a.slug+"_open_url"] = public
	}
	return out
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	st, err := s.store.GetSettings(r.Context(), actorID(r.Context()))
	if err != nil {
		s.fail(w, err, "settings")
		return
	}
	writeJSON(w, http.StatusOK, s.publicSettings(r.Context(), userFrom(r.Context()), st))
}

// handlePutSettings saves the caller's personal settings. The box-wide title
// is only taken from an admin.
func (s *Server) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	me := userFrom(r.Context())
	isAdmin := me.Role == "admin"
	cur, err := s.store.GetSettings(r.Context(), me.ID)
	if err != nil {
		s.fail(w, err, "settings")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(nil, r.Body, 1<<20))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	var raw map[string]json.RawMessage
	var in store.Settings
	if json.Unmarshal(body, &raw) != nil || json.Unmarshal(body, &in) != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	keys := map[string]bool{}
	for k := range raw {
		keys[k] = true
	}
	bad := func(msg string) { writeErr(w, http.StatusBadRequest, msg) }
	if keys["title"] && isAdmin {
		cur.Title = strings.TrimSpace(in.Title)
		if cur.Title == "" {
			cur.Title = store.DefaultTitle
		}
	}
	if keys["date_format"] {
		cur.DateFormat = in.DateFormat
	}
	if keys["time_format"] {
		cur.TimeFormat = in.TimeFormat
	}
	if keys["week_start"] && (in.WeekStart == 1 || in.WeekStart == 7 || in.WeekStart == 6) {
		cur.WeekStart = in.WeekStart
	}
	intIn := func(k string, dst *int, v, lo, hi int) bool {
		if !keys[k] {
			return true
		}
		if v < lo || v > hi {
			bad(fmt.Sprintf("%s is %d to %d", k, lo, hi))
			return false
		}
		*dst = v
		return true
	}
	if !intIn("daily_goal_min", &cur.DailyGoalMin, in.DailyGoalMin, 0, 24*60) || !intIn("weekly_goal_min", &cur.WeeklyGoalMin, in.WeeklyGoalMin, 0, 7*24*60) ||
		!intIn("long_timer_h", &cur.LongTimerH, in.LongTimerH, 0, 48) || !intIn("collect_days", &cur.CollectDays, in.CollectDays, 7, 730) ||
		!intIn("weekly_day", &cur.WeeklyDay, in.WeeklyDay, 1, 7) || !intIn("weekly_hour", &cur.WeeklyHour, in.WeeklyHour, 0, 23) {
		return
	}
	if keys["rounding"] {
		switch in.Rounding {
		case 0, 1, 5, 6, 10, 15, 30, 60:
			cur.Rounding = in.Rounding
		default:
			bad("rounding is 0, 1, 5, 6, 10, 15, 30 or 60 minutes")
			return
		}
	}
	secret := func(k string, dst *string, v string) {
		if keys[k] && !strings.Contains(v, "••") {
			*dst = strings.TrimSpace(v)
		}
	}
	urlIn := func(v string) string { return strings.TrimRight(strings.TrimSpace(v), "/") }
	text := func(k string, dst *string, v string) {
		if keys[k] {
			*dst = strings.TrimSpace(v)
		}
	}
	boolIn := func(k string, dst *bool, v bool) {
		if keys[k] {
			*dst = v
		}
	}
	text("currency", &cur.Currency, strings.ToUpper(in.Currency))
	boolIn("default_billable", &cur.DefaultBill, in.DefaultBill)
	boolIn("switch_ask", &cur.SwitchAsk, in.SwitchAsk)
	boolIn("title_in_tab", &cur.TitleInTab, in.TitleInTab)
	boolIn("notify_long", &cur.NotifyLong, in.NotifyLong)
	boolIn("notify_weekly", &cur.NotifyWeekly, in.NotifyWeekly)
	text("notify_url", &cur.NotifyURL, in.NotifyURL)
	if keys["notify_format"] && in.NotifyFormat != "" {
		cur.NotifyFormat = in.NotifyFormat
	}
	if keys["ai_provider"] {
		if !validAIProvider(in.AIProvider) {
			bad("AI is automatic, Pocket AI or OpenAI")
			return
		}
		cur.AIProvider = in.AIProvider
	}
	if keys["ai_pocket_model"] {
		if !validPocketModel(in.AIPocketModel) {
			bad("unknown model")
			return
		}
		cur.AIPocketModel = in.AIPocketModel
	}
	secret("openai_api_key", &cur.OpenAIKey, in.OpenAIKey)
	text("openai_model", &cur.OpenAIModel, in.OpenAIModel)
	if keys["openai_base_url"] {
		cur.OpenAIBaseURL = urlIn(in.OpenAIBaseURL)
	}
	// per app: <slug>_mode, _url, _token, _public_url, _collect
	var stopCollect []string
	for _, a := range pocketApps {
		c := cur.Conn(a.slug)
		str := func(k string) (string, bool) {
			v, ok := raw[a.slug+"_"+k]
			if !ok {
				return "", false
			}
			var x string
			_ = json.Unmarshal(v, &x)
			return x, true
		}
		if v, ok := str("mode"); ok && (v == modeCore || v == modeDirect || v == modeOff) {
			c.Mode = v
		}
		if v, ok := str("url"); ok {
			c.URL = urlIn(v)
		}
		if v, ok := str("token"); ok && !strings.Contains(v, "••") {
			c.Token = strings.TrimSpace(v)
		}
		if v, ok := str("public_url"); ok {
			c.PublicURL = urlIn(v)
		}
		if v, ok := raw[a.slug+"_collect"]; ok {
			var b bool
			if json.Unmarshal(v, &b) == nil {
				if !b && c.Collects() {
					stopCollect = append(stopCollect, a.slug)
				}
				c.Collect = &b
			}
		}
		cur.SetConn(a.slug, c)
	}
	if err := s.store.SaveUserSettings(r.Context(), me.ID, cur); err != nil {
		s.fail(w, err, "save settings")
		return
	}
	if isAdmin && keys["title"] {
		if err := s.store.SaveBoxSettings(r.Context(), cur); err != nil {
			s.fail(w, err, "save settings")
			return
		}
	}
	for _, slug := range stopCollect {
		_ = s.store.ClearCollected(r.Context(), me.ID, slug)
	}
	for k := range keys {
		if modeKey(k) {
			s.reportModesSoon()
			break
		}
	}
	for k := range keys {
		if strings.HasSuffix(k, "_mode") || strings.HasSuffix(k, "_token") || strings.HasSuffix(k, "_collect") || k == "collect_days" {
			go func(uid int64) {
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
				defer cancel()
				s.collect(ctx, uid, "")
			}(me.ID)
			break
		}
	}
	writeJSON(w, http.StatusOK, s.publicSettings(r.Context(), me, cur))
}

// handleIntegrationTest checks a connection with the caller's own saved
// settings (or the URL/key typed into the form).
func (s *Server) handleIntegrationTest(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Which  string `json:"which"`
		URL    string `json:"url"`
		Key    string `json:"key"`
		Format string `json:"format"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	uid := actorID(r.Context())
	st, _ := s.store.GetSettings(r.Context(), uid)
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	keyOr := func(saved string) string {
		if strings.Contains(in.Key, "••") || in.Key == "" {
			return saved
		}
		return in.Key
	}
	urlOr := func(saved string) string {
		if strings.TrimSpace(in.URL) == "" {
			return saved
		}
		return strings.TrimRight(strings.TrimSpace(in.URL), "/")
	}
	var msg string
	var err error
	if a, ok := appBySlug(in.Which); ok {
		mode, savedURL, savedTok, _ := connSettings(st, a)
		typed := in.Key != "" && !strings.Contains(in.Key, "••")
		var conn *siblingConn
		if tok := keyOr(savedTok); tok != "" && (typed || s.connMode(mode, savedTok) == modeDirect) {
			base := urlOr(savedURL)
			if base == "" {
				base = s.resolveSibling(ctx, a.slug)
			}
			conn = &siblingConn{client: pocket.New(base, tok, 15*time.Second), via: modeDirect}
		} else {
			conn, err = s.clients(ctx, uid).conn(ctx, a, a.scopes()...)
		}
		if err == nil {
			how := "direct connection"
			if conn.via == modeCore {
				how = "through Pocket Core"
			}
			var v string
			if v, err = conn.client.Health(ctx); err == nil {
				to := store.Today()
				var list []store.Entry
				list, err = adapterFor(a.slug).collect(ctx, conn.client, store.AddDays(to, -6), to)
				if err == nil {
					msg = fmt.Sprintf("%s %s, %s, %d entries in the last 7 days", a.name, v, how, len(list))
				} else {
					err = fmt.Errorf("%s", plainErr(a, err))
				}
			}
		}
	} else if in.Which == "openai" {
		c := ai.New(urlOr(st.OpenAIBaseURL), keyOr(st.OpenAIKey), st.OpenAIModel, 20*time.Second)
		var ids []string
		if ids, err = c.Models(ctx); err == nil {
			msg = fmt.Sprintf("The key works (%d models)", len(ids))
		}
	} else if in.Which == "notify" {
		f := in.Format
		if f == "" {
			f = st.NotifyFormat
		}
		n := notify.New(urlOr(st.NotifyURL), f)
		err = n.Send(ctx, notify.Message{Title: "Pocket Time", Body: "Notifications work. Timer reminders and the weekly report will arrive here.", Priority: "info", Tags: []string{"stopwatch"}})
		msg = "Test message sent via " + n.Format
	} else {
		writeErr(w, http.StatusBadRequest, "unknown integration")
		return
	}
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": msg})
}

func (s *Server) handleNotifyTest(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	st, _ := s.store.GetSettings(ctx, actorID(ctx))
	n := notify.New(st.NotifyURL, st.NotifyFormat)
	if err := n.Send(ctx, notify.Message{Title: "Pocket Time", Body: "Notifications work.", Priority: "info", Tags: []string{"stopwatch"}}); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ---- system / backup / export ----

func (s *Server) handleSystemInfo(w http.ResponseWriter, r *http.Request) {
	var size int64
	if fi, err := os.Stat(s.cfg.DBPath); err == nil {
		size = fi.Size()
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"version": Version, "go": runtime.Version(), "os": runtime.GOOS, "arch": runtime.GOARCH,
		"core":      s.coreStatus(),
		"auth_mode": s.cfg.AuthMode, "data_dir": s.cfg.DataDir, "db_path": s.cfg.DBPath, "db_bytes": size,
		"uptime": time.Since(s.started).Round(time.Second).String(), "timezone": time.Now().Location().String(),
		"counts": s.store.CountsFor(r.Context(), actorID(r.Context())), "saved": s.saved.Load(),
	})
}

// handleBackup downloads the whole database, an admin-only box operation.
func (s *Server) handleBackup(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	dir := filepath.Join(s.cfg.DataDir, "backups")
	name := fmt.Sprintf("pocket-time-%s.db", time.Now().Format("20060102-150405"))
	dst := filepath.Join(dir, name)
	if err := s.store.Backup(r.Context(), dst); err != nil {
		s.fail(w, err, "backup")
		return
	}
	w.Header().Set("Content-Type", "application/vnd.sqlite3")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	http.ServeFile(w, r, dst)
}

// exportDoc is the JSON export of one person's data. Collected time is left
// out: it lives in the app that keeps it.
type exportDoc struct {
	App      string          `json:"app"`
	Version  string          `json:"version"`
	Exported string          `json:"exported_at"`
	Projects []store.Project `json:"projects"`
	Entries  []store.Entry   `json:"entries"`
}

func (s *Server) handleExportJSON(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	no := false
	doc := exportDoc{App: "pocket-time", Version: Version, Exported: store.Now()}
	doc.Projects, _ = s.store.ListProjects(ctx, uid, true)
	doc.Entries, _, _ = s.store.ListEntries(ctx, uid, store.EntryFilter{Collected: &no, Asc: true})
	w.Header().Set("Content-Disposition", `attachment; filename="pocket-time-export.json"`)
	writeJSON(w, http.StatusOK, doc)
}

// handleExportCSV writes every entry (collected ones too) as a CSV row.
func (s *Server) handleExportCSV(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	list, _, err := s.store.ListEntries(ctx, actorID(ctx), store.EntryFilter{Asc: true})
	if err != nil {
		s.fail(w, err, "export")
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="pocket-time-entries.csv"`)
	writeEntriesCSV(w, list)
}

// handleImportJSON merges a Pocket Time export into the caller's own data.
// Entries keep their identity (external id "import:<original id>"), so
// importing the same file twice adds nothing; imported time is not sent to
// other apps again.
func (s *Server) handleImportJSON(w http.ResponseWriter, r *http.Request) {
	var doc exportDoc
	if err := decode(r, &doc); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if doc.App != "" && doc.App != "pocket-time" {
		writeErr(w, http.StatusBadRequest, "this is not a Pocket Time export")
		return
	}
	ctx := r.Context()
	uid := actorID(ctx)
	counts := map[string]int{"projects": 0, "entries": 0, "skipped": 0}
	pids := map[int64]int64{}
	for _, p := range doc.Projects {
		old := p.ID
		if existing, err := s.store.ProjectByName(ctx, uid, p.Name); err == nil {
			pids[old] = existing.ID
			continue
		}
		p.ID = 0
		if strings.TrimSpace(p.Name) == "" {
			continue
		}
		if err := s.store.CreateProject(ctx, uid, &p); err == nil {
			pids[old] = p.ID
			counts["projects"]++
		}
	}
	for _, e := range doc.Entries {
		if e.Collected || e.EndedAt == "" {
			counts["skipped"]++
			continue
		}
		old := e.ID
		e.ID = 0
		if e.ExternalID == "" {
			e.ExternalID = fmt.Sprintf("import:%d", old)
		}
		if e.ProjectID != nil {
			if np, ok := pids[*e.ProjectID]; ok {
				e.ProjectID = &np
			} else {
				e.ProjectID = nil
			}
		}
		e.Source, e.Deliver = "import", false
		if _, ok := appBySlug(e.App); !ok {
			e.App, e.AppRef, e.AppLabel, e.AppURL = "", "", "", ""
		}
		in := entryInput{StartedAt: e.StartedAt, EndedAt: e.EndedAt}
		if m := finishTimes(&in, nil, &e); m != "" {
			counts["skipped"]++
			continue
		}
		if err := s.store.CreateEntry(ctx, uid, &e); err != nil {
			counts["skipped"]++
			continue
		}
		counts["entries"]++
	}
	writeJSON(w, http.StatusOK, counts)
}
