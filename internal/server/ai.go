package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-time/internal/ai"
	"github.com/abjelosevic88/pocket-homelab-time/internal/store"
)

// aiFor is the person's AI: Pocket AI through Core, their own
// OpenAI-compatible key, or nil with why.
func (s *Server) aiFor(ctx context.Context, uid int64, st store.Settings) (*ai.Client, string, string) {
	email := ""
	if u, err := s.store.GetUser(ctx, uid); err == nil {
		email = u.Email
	}
	pocket, status := s.core.SelectAI(ctx, email, st.AIProvider, st.OpenAIKey != "")
	if pocket != nil {
		if !status.Ready {
			return nil, "", "Pocket AI: " + status.Detail
		}
		c := ai.New("", "", "", 150*time.Second)
		c.Pocket = pocket.WithModel(st.AIPocketModel)
		return c, "pocket", ""
	}
	if st.OpenAIKey == "" {
		return nil, "", status.Detail
	}
	return ai.New(st.OpenAIBaseURL, st.OpenAIKey, st.OpenAIModel, 150*time.Second), "openai", ""
}

func (s *Server) handleAIStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	st, _ := s.store.GetSettings(ctx, actorID(ctx))
	cl, used, why := s.aiFor(ctx, actorID(ctx), st)
	writeJSON(w, http.StatusOK, map[string]any{"ready": cl != nil, "provider": used, "detail": why})
}

func (s *Server) handleAIModels(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	c, status := s.core.SelectAI(ctx, userFrom(ctx).Email, "pocket", false)
	if c == nil || !status.Ready {
		writeErr(w, http.StatusServiceUnavailable, status.Detail)
		return
	}
	models, err := c.Models(ctx)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, models)
}

func validAIProvider(v string) bool { return core.ValidAIProvider(v) }

func validPocketModel(v string) bool { return core.ValidInferenceModel(v) }

const aiVoice = `You help someone look at their own time tracking: entries with a title, a project, the Pocket app it was for (Guitar practice, a Projects issue, studying in Learn…), when it started and how long it took. Use only the data given; never invent entries or numbers. Be concrete and short, in plain words. Durations as "3 h 20 min". No emojis, no exclamation marks, no productivity jargon, no motivational filler.`

// timeDigest renders a period's entries as compact text for the model.
func (s *Server) timeDigest(ctx context.Context, uid int64, from, to string) string {
	rep, list, err := s.buildReport(ctx, uid, from, to, "project", store.EntryFilter{})
	if err != nil || len(list) == 0 {
		return ""
	}
	st, _ := s.store.GetSettings(ctx, uid)
	apps, _, _ := s.buildReport(ctx, uid, from, to, "app", store.EntryFilter{})
	var b strings.Builder
	fmt.Fprintf(&b, "Period %s to %s. Total %s in %d entries", from, to, fmtDur(rep.Seconds), rep.Count)
	if rep.BillableS > 0 {
		fmt.Fprintf(&b, ", billable %s", fmtDur(rep.BillableS))
	}
	if st.DailyGoalMin > 0 {
		fmt.Fprintf(&b, ". Daily target %s", fmtDur(st.DailyGoalMin*60))
	}
	if st.WeeklyGoalMin > 0 {
		fmt.Fprintf(&b, ". Weekly target %s", fmtDur(st.WeeklyGoalMin*60))
	}
	b.WriteString(".\nBy project: ")
	for i, g := range rep.Groups {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(g.Label + " " + fmtDur(g.Seconds))
	}
	b.WriteString("\nBy app: ")
	for i, g := range apps.Groups {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(g.Label + " " + fmtDur(g.Seconds))
	}
	b.WriteString("\nEntries (start | length | title | project | app: for what):\n")
	n := 0
	for _, e := range list {
		if n++; n > 600 {
			b.WriteString("… (more entries left out)\n")
			break
		}
		t := ""
		if tt, ok := store.ParseTime(e.StartedAt); ok {
			t = tt.Format("Mon 2006-01-02 15:04")
		}
		app := ""
		if e.App != "" {
			app = appName(e.App) + ": " + e.AppLabel
		}
		fmt.Fprintf(&b, "%s | %s | %s | %s | %s\n", t, fmtDur(e.DurationS), e.Title, e.ProjectName, app)
	}
	return b.String()
}

// handleSummary writes a short look back at a period (this week by default).
func (s *Server) handleSummary(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	st, _ := s.store.GetSettings(ctx, uid)
	cl, used, why := s.aiFor(ctx, uid, st)
	if cl == nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": why})
		return
	}
	var in struct {
		From string `json:"from"`
		To   string `json:"to"`
	}
	_ = decode(r, &in)
	if !validDate(in.From) || !validDate(in.To) || in.From == "" || in.To == "" {
		in.From = weekStart(store.Today(), st.WeekStart)
		in.To = store.AddDays(in.From, 6)
	}
	digest := s.timeDigest(ctx, uid, in.From, in.To)
	if digest == "" {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "nothing tracked in this period yet"})
		return
	}
	prompt := "Write a short look back at this period in 4–6 sentences: where most of the time went, how it was spread over the days, anything that stands out (a project that grew, long days, gaps), and one small, practical suggestion. Then a line 'In short:' with one sentence.\n\n" + digest
	text, err := cl.Complete(ctx, aiVoice, prompt, false)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "provider": used, "from": in.From, "to": in.To, "text": strings.TrimSpace(text)})
}

// handleAsk answers a question about the person's tracked time.
func (s *Server) handleAsk(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	st, _ := s.store.GetSettings(ctx, uid)
	cl, used, why := s.aiFor(ctx, uid, st)
	if cl == nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": why})
		return
	}
	var in struct {
		Question string `json:"question"`
		Days     int    `json:"days"`
	}
	if err := decode(r, &in); err != nil || strings.TrimSpace(in.Question) == "" {
		writeErr(w, http.StatusBadRequest, "question is required")
		return
	}
	if in.Days <= 0 || in.Days > 366 {
		in.Days = 31
	}
	to := store.Today()
	from := store.AddDays(to, -(in.Days - 1))
	digest := s.timeDigest(ctx, uid, from, to)
	if digest == "" {
		digest = "Nothing tracked in this period.\n"
	}
	prompt := "Question: " + strings.TrimSpace(in.Question) + "\n\nTheir tracked time:\n" + digest +
		"\nAnswer in a few sentences, with the numbers that back it. If the data cannot answer it, say so."
	text, err := cl.Complete(ctx, aiVoice, prompt, false)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "provider": used, "text": strings.TrimSpace(text)})
}
