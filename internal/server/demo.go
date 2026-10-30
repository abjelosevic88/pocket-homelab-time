package server

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	"github.com/abjelosevic88/pocket-homelab-time/internal/store"
)

// seedDemo fills the owner's account with four months of plausible tracked
// time (DEMO_DATA=true, for screenshots and trying the app): client work in
// projects, issues from Pocket Projects, guitar practice and study sessions
// timed through the shared timer, time collected from Guitar and Still, and
// a timer running now that Pocket Projects started. Runs once.
func (s *Server) seedDemo(ctx context.Context, owner int64) error {
	key := fmt.Sprintf("seeded:%d:demo", owner)
	if _, _, done := s.store.CacheGet(ctx, key); done {
		return nil
	}
	rng := rand.New(rand.NewSource(42))
	projects := []store.Project{
		{Name: "Northwind redesign", Color: "#2f6fdf", Client: "Northwind Traders", Billable: true, Rate: 55, GoalMin: 15 * 60},
		{Name: "Harbor mobile app", Color: "#0f9d77", Client: "Harbor & Co", Billable: true, Rate: 60, GoalMin: 10 * 60},
		{Name: "Internal & admin", Color: "#8a8f98"},
		{Name: "Side project", Color: "#c2410c", GoalMin: 4 * 60},
		{Name: "Guitar", Color: "#b8231f", GoalMin: 3 * 60},
		{Name: "Learning", Color: "#4263eb", GoalMin: 2 * 60},
	}
	for i := range projects {
		projects[i].Sort = i
		if err := s.store.CreateProject(ctx, owner, &projects[i]); err != nil {
			return err
		}
	}
	pid := func(i int) *int64 { v := projects[i].ID; return &v }
	work := [][]string{
		{"Homepage wireframes", "Design review with the client", "Checkout flow", "Style guide", "Accessibility pass", "Handoff notes"},
		{"Push notifications", "Login screen", "Offline sync", "App Store screenshots", "Crash fixes", "Sprint planning"},
		{"Email and invoices", "Bookkeeping", "Weekly planning", "Team call"},
		{"Landing page", "Blog post draft", "Pricing experiments"},
	}
	issues := []struct{ ref, label string }{
		{"issue:ATH-142", "ATH-142 Fix the login redirect"}, {"issue:ATH-157", "ATH-157 Export timesheets"},
		{"issue:ATH-163", "ATH-163 Slow dashboard on phones"}, {"issue:ATH-171", "ATH-171 Dark mode polish"},
		{"issue:ATH-188", "ATH-188 Invite teammates"},
	}
	songs := []struct{ ref, label string }{
		{"tab:3", "Wonderwall"}, {"tab:7", "Blackbird"}, {"tab:12", "Hotel California"}, {"tab:15", "Wish You Were Here"}, {"tab:21", "Nothing Else Matters"},
	}
	decks := []struct{ ref, label string }{{"deck:1", "Spanish — everyday words"}, {"deck:4", "Go interview questions"}, {"deck:6", "Music theory"}}
	notes := []string{"", "", "", "", "Went faster than planned.", "Blocked waiting for feedback.", "Good focus today.", "Call ran long."}

	add := func(e store.Entry) error {
		if t, ok := store.ParseTime(e.StartedAt); ok {
			e.EndedAt = store.Stamp(t.Add(time.Duration(e.DurationS) * time.Second))
			e.Date = store.LocalDate(t)
		}
		return s.store.CreateEntry(ctx, owner, &e)
	}
	now := time.Now()
	today, _ := time.ParseInLocation("2006-01-02", store.Today(), time.Local)
	for back := 120; back >= 0; back-- {
		day := today.AddDate(0, 0, -back)
		wd := day.Weekday()
		weekend := wd == time.Saturday || wd == time.Sunday
		if !weekend && back > 4 && rng.Float64() < 0.06 {
			continue // a day off
		}
		at := day.Add(time.Duration(8*60+rng.Intn(70)) * time.Minute)
		if !weekend {
			blocks := 3 + rng.Intn(3)
			for b := 0; b < blocks; b++ {
				p := rng.Intn(10)
				var e store.Entry
				switch {
				case p < 4:
					e = store.Entry{Title: work[0][rng.Intn(len(work[0]))], ProjectID: pid(0), Billable: true}
				case p < 7:
					is := issues[rng.Intn(len(issues))]
					e = store.Entry{Title: is.label, ProjectID: pid(1), Billable: true, App: "projects", AppRef: is.ref, AppLabel: is.label, AppURL: "/issue/" + is.ref[6:], Deliver: true, DeliveredAt: store.Now(), RemoteID: fmt.Sprint(1000 + rng.Intn(900))}
					if rng.Intn(2) == 0 {
						e.Source = "projects"
					}
				case p < 9:
					e = store.Entry{Title: work[2][rng.Intn(len(work[2]))], ProjectID: pid(2)}
				default:
					e = store.Entry{Title: work[3][rng.Intn(len(work[3]))], ProjectID: pid(3)}
				}
				if e.Source == "" {
					e.Source = "time"
				}
				e.Note = notes[rng.Intn(len(notes))]
				e.StartedAt = store.Stamp(at)
				e.DurationS = (25 + rng.Intn(140)) * 60
				if at.Add(time.Duration(e.DurationS)*time.Second).After(now) || back == 0 && at.Hour() >= now.Hour()-1 {
					break
				}
				if err := add(e); err != nil {
					return err
				}
				at = at.Add(time.Duration(e.DurationS+(5+rng.Intn(40))*60) * time.Second)
				if b == 1 {
					at = at.Add(45 * time.Minute) // lunch
				}
			}
		}
		// guitar practice in the evening, timed with the shared timer from Pocket Guitar
		if rng.Float64() < 0.55 {
			sg := songs[rng.Intn(len(songs))]
			start := day.Add(time.Duration(19*60+rng.Intn(120)) * time.Minute)
			if start.Before(now.Add(-time.Hour)) {
				e := store.Entry{Title: sg.label, ProjectID: pid(4), Source: "guitar", App: "guitar", AppRef: sg.ref, AppLabel: sg.label, AppURL: "/tab/" + sg.ref[4:],
					Deliver: true, DeliveredAt: store.Now(), RemoteID: fmt.Sprint(500 + rng.Intn(400)), StartedAt: store.Stamp(start), DurationS: (12 + rng.Intn(40)) * 60}
				if err := add(e); err != nil {
					return err
				}
			}
		}
		// studying, a few times a week
		if rng.Float64() < 0.35 {
			dk := decks[rng.Intn(len(decks))]
			start := day.Add(time.Duration(7*60+rng.Intn(40)) * time.Minute)
			if weekend {
				start = day.Add(time.Duration(10*60+rng.Intn(120)) * time.Minute)
			}
			if start.Before(now.Add(-time.Hour)) {
				e := store.Entry{Title: dk.label, ProjectID: pid(5), Source: "learn", App: "learn", AppRef: dk.ref, AppLabel: dk.label, AppURL: "/decks/" + dk.ref[5:],
					Deliver: true, DeliveredAt: store.Now(), RemoteID: fmt.Sprint(200 + rng.Intn(300)), StartedAt: store.Stamp(start), DurationS: (10 + rng.Intn(25)) * 60}
				if err := add(e); err != nil {
					return err
				}
			}
		}
		// collected: practice the apps logged themselves (song player, meditation timer)
		if rng.Float64() < 0.25 {
			sg := songs[rng.Intn(len(songs))]
			start := day.Add(time.Duration(17*60+rng.Intn(60)) * time.Minute)
			if start.Before(now.Add(-time.Hour)) {
				e := store.Entry{Title: sg.label, Source: "guitar", App: "guitar", AppRef: sg.ref, AppLabel: sg.label, AppURL: "/tab/" + sg.ref[4:],
					Collected: true, ExternalID: fmt.Sprintf("guitar:%d", 9000+back), StartedAt: store.Stamp(start), DurationS: (8 + rng.Intn(20)) * 60}
				if err := add(e); err != nil {
					return err
				}
			}
		}
		if rng.Float64() < 0.5 {
			start := day.Add(time.Duration(6*60+30+rng.Intn(30)) * time.Minute)
			if start.Before(now.Add(-time.Hour)) {
				kinds := []string{"Morning sit", "Box breathing", "Body scan"}
				k := kinds[rng.Intn(len(kinds))]
				e := store.Entry{Title: k, Source: "still", App: "still", AppRef: "kind:meditation", AppLabel: k, AppURL: fmt.Sprintf("/journal/%d", 7000+back),
					Collected: true, ExternalID: fmt.Sprintf("still:%d", 7000+back), StartedAt: store.Stamp(start), DurationS: (5 + rng.Intn(16)) * 60}
				if err := add(e); err != nil {
					return err
				}
			}
		}
	}
	// a timer running now, started from an issue in Pocket Projects
	run := store.Entry{Title: "ATH-157 Export timesheets", ProjectID: pid(1), Billable: true, Source: "projects", App: "projects", AppRef: "issue:ATH-157",
		AppLabel: "ATH-157 Export timesheets", AppURL: "/issue/ATH-157", Deliver: true, StartedAt: store.Stamp(now.Add(-47 * time.Minute)), Date: store.LocalDate(now.Add(-47 * time.Minute))}
	if _, err := s.store.StartEntry(ctx, owner, &run, true); err != nil {
		return err
	}
	for _, a := range []string{"projects", "guitar", "learn", "still"} {
		_ = s.store.SaveAppSync(ctx, owner, store.AppSync{App: a, SyncedAt: store.Now(), Count: 20 + rng.Intn(80)})
	}
	st, _ := s.store.GetSettings(ctx, owner)
	st.DailyGoalMin, st.WeeklyGoalMin, st.Currency = 6*60, 32*60, "EUR"
	_ = s.store.SaveUserSettings(ctx, owner, st)
	return s.store.CachePut(ctx, key, "1")
}
