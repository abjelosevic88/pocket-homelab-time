package store

import "strings"

// User is an account on this box.
type User struct {
	ID           int64  `json:"id"`
	Email        string `json:"email"`
	Name         string `json:"name"`
	PasswordHash string `json:"-"`
	Role         string `json:"role"` // admin | member
	CreatedAt    string `json:"created_at"`
}

// APIToken is a personal access token (hash stored, plaintext shown once).
type APIToken struct {
	ID         int64   `json:"id"`
	UserID     int64   `json:"user_id"`
	Name       string  `json:"name"`
	Prefix     string  `json:"prefix"`
	LastUsedAt *string `json:"last_used_at"`
	CreatedAt  string  `json:"created_at"`
}

// Entry is one stretch of tracked time. It is either timed here (the timer
// in Pocket Time, or another Pocket app starting and stopping it through
// Pocket Time), logged by hand, or collected: a read-only copy of time a
// sibling app keeps in its own log (Guitar practice, a Projects time entry).
// EndedAt is "" while the timer runs; one running entry per person.
type Entry struct {
	ID        int64  `json:"id"`
	UserID    int64  `json:"-"`
	Title     string `json:"title"`
	Note      string `json:"note"`
	ProjectID *int64 `json:"project_id"`
	Tags      string `json:"tags"`
	Billable  bool   `json:"billable"`
	StartedAt string `json:"started_at"` // RFC3339 with offset
	EndedAt   string `json:"ended_at"`   // "" = running
	Date      string `json:"date"`       // local date of StartedAt
	DurationS int    `json:"duration_s"` // final length; while running, computed when read
	Running   bool   `json:"running"`

	// Who made it: "time" (here), "manual", "import", or the slug of the
	// Pocket app that started it through Pocket Time.
	Source string `json:"source"`
	// The item in another Pocket app this time was for ("" = only here).
	App      string `json:"app"`
	AppRef   string `json:"app_ref"`   // that app's id for it: "tab:42", "issue:ATH-12"
	AppLabel string `json:"app_label"` // "Wonderwall", "ATH-12 Fix the login"
	AppURL   string `json:"app_url"`   // open it there

	// Collected entries mirror a record kept in App; ExternalID is "<app>:<their id>".
	Collected  bool   `json:"collected"`
	ExternalID string `json:"external_id"`

	// Sending: when Deliver is set, the finished entry is written to App.
	Deliver      bool   `json:"deliver"`
	DeliveredAt  string `json:"delivered_at"`
	DeliverError string `json:"deliver_error"`
	RemoteID     string `json:"remote_id"`

	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`

	// Filled in for the UI.
	ProjectName  string `json:"project_name,omitempty"`
	ProjectColor string `json:"project_color,omitempty"`
}

// Minutes is the entry's length in minutes.
func (e Entry) Minutes() float64 { return float64(e.DurationS) / 60 }

// Project groups entries: client work, a side project, a hobby.
type Project struct {
	ID        int64   `json:"id"`
	UserID    int64   `json:"-"`
	Name      string  `json:"name"`
	Color     string  `json:"color"`
	Client    string  `json:"client"`
	Billable  bool    `json:"billable"` // new entries in it are billable
	Rate      float64 `json:"rate"`     // per hour, in the person's currency (0 = none)
	GoalMin   int     `json:"goal_min"` // a weekly target (0 = none)
	Archived  bool    `json:"archived"`
	Sort      int     `json:"sort"`
	CreatedAt string  `json:"created_at"`
	UpdatedAt string  `json:"updated_at"`
}

// AppSync is how collecting from one sibling app went for a person.
type AppSync struct {
	App      string `json:"app"`
	SyncedAt string `json:"synced_at"`
	Error    string `json:"error"`
	Count    int    `json:"count"`
}

// Conn is how a person reaches one sibling Pocket app.
type Conn struct {
	Mode      string `json:"mode"` // core | direct | off | "" (not chosen)
	URL       string `json:"url"`
	Token     string `json:"token"`
	PublicURL string `json:"public_url"`
	Collect   *bool  `json:"collect"` // show that app's own time here (nil = yes)
}

// Collects reports whether the app's own time is collected (default yes).
func (c Conn) Collects() bool { return c.Collect == nil || *c.Collect }

// Settings are the box-wide values plus one person's preferences and their
// own connections. Connection modes: "core" (an app link through Pocket
// Core), "direct" (URL + token) or "off"; "" = not chosen yet.
type Settings struct {
	Title         string `json:"title"`
	SetupComplete bool   `json:"setup_complete"`

	DateFormat string `json:"date_format"`
	TimeFormat string `json:"time_format"`
	WeekStart  int    `json:"week_start"` // 1 = Monday, 7 = Sunday, 6 = Saturday

	// Targets and reports.
	DailyGoalMin  int    `json:"daily_goal_min"`  // 0 = no daily target
	WeeklyGoalMin int    `json:"weekly_goal_min"` // 0 = no weekly target
	Rounding      int    `json:"rounding"`        // minutes to round entries to in reports (0 = exact)
	Currency      string `json:"currency"`        // for project rates
	DefaultBill   bool   `json:"default_billable"`

	// The timer.
	SwitchAsk   bool `json:"switch_ask"`   // ask before a new timer stops the running one
	LongTimerH  int  `json:"long_timer_h"` // remind when a timer runs this long (0 = never)
	TitleInTab  bool `json:"title_in_tab"` // show the running time in the browser tab
	CollectDays int  `json:"collect_days"` // how many days back sibling apps' time is collected

	// Notifications.
	NotifyLong   bool   `json:"notify_long"`
	NotifyWeekly bool   `json:"notify_weekly"`
	WeeklyDay    int    `json:"weekly_day"` // ISO weekday the weekly report goes out
	WeeklyHour   int    `json:"weekly_hour"`
	NotifyURL    string `json:"notify_url"`
	NotifyFormat string `json:"notify_format"` // apprise | ntfy | json

	// Sibling Pocket apps, by slug.
	Conns map[string]Conn `json:"conns"`

	// AI for summaries and questions: Pocket AI through Core, or an
	// OpenAI-compatible API with the person's own key.
	AIProvider    string `json:"ai_provider"` // auto | pocket | openai
	AIPocketModel string `json:"ai_pocket_model"`
	OpenAIKey     string `json:"openai_api_key"`
	OpenAIModel   string `json:"openai_model"`
	OpenAIBaseURL string `json:"openai_base_url"`
}

// Conn returns a person's connection to an app (zero value if none).
func (st Settings) Conn(slug string) Conn {
	if st.Conns == nil {
		return Conn{}
	}
	return st.Conns[slug]
}

// SetConn stores a person's connection to an app.
func (st *Settings) SetConn(slug string, c Conn) {
	if st.Conns == nil {
		st.Conns = map[string]Conn{}
	}
	st.Conns[slug] = c
}

// SplitTags turns "a, b ,c" into clean lowercase tags.
func SplitTags(s string) []string {
	var out []string
	seen := map[string]bool{}
	for _, t := range strings.Split(s, ",") {
		t = strings.ToLower(strings.TrimSpace(t))
		if t != "" && !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	return out
}

// JoinTags is the stored form of tags.
func JoinTags(s string) string { return strings.Join(SplitTags(s), ", ") }
