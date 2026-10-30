package server

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-time/internal/store"
)

// Pocket Core app links (docs/links.md in pocket-homelab-core), Time as the
// target: another Pocket app the person allowed in Core may act here for
// them with a pcx_ token, only within these scopes. Every route not listed
// in linkRoutes stays closed to it (403).
//
// This is what makes Time the one hub for timers: Pocket Guitar, Projects,
// Learn… start and stop the person's ONE running timer here (time:write),
// and show it when it was started somewhere else (time:read).

var linkOffers = []core.Offer{
	{Scope: "time:read", Name: "See your timer and tracked time", Description: "See the timer that is running (wherever it was started) and the time you tracked, by day and project — for your journal, calendar, dashboards and other apps' timer buttons. Never your notes."},
	{Scope: "time:write", Name: "Start and stop your timer", Description: "Start and stop your one running timer from inside another app (practising a song, working on an issue), and log time done there. It may change only the time that app logged itself."},
}

type linkRoute struct {
	methods string
	path    *regexp.Regexp
	scope   string // "" = any linked app
}

var linkRoutes = []linkRoute{
	{"GET", regexp.MustCompile(`^/api/v1/auth/me$`), ""},
	{"GET", regexp.MustCompile(`^/api/v1/(timer|dashboard|entries|projects|report)$`), "time:read time:write"},
	{"GET", regexp.MustCompile(`^/api/v1/entries/\d+$`), "time:read time:write"},
	{"GET", regexp.MustCompile(`^/api/v1/(day|summary)/\d{4}-\d{2}-\d{2}$`), "time:read"},
	{"POST", regexp.MustCompile(`^/api/v1/timer/(start|stop)$`), "time:write"},
	{"POST", regexp.MustCompile(`^/api/v1/entries$`), "time:write"},
	{"PUT DELETE", regexp.MustCompile(`^/api/v1/entries/\d+$`), "time:write"},
}

var errLinkScope = errors.New("outside the link's scope")

// linkAllows reports whether a link token with these scopes may make this request.
func linkAllows(scopes []string, r *http.Request) bool {
	for _, lr := range linkRoutes {
		if !strings.Contains(" "+lr.methods+" ", " "+r.Method+" ") || !lr.path.MatchString(r.URL.Path) {
			continue
		}
		if lr.scope == "" {
			return true
		}
		for _, sc := range scopes {
			for _, need := range strings.Fields(lr.scope) {
				if sc == need {
					return true
				}
			}
		}
	}
	return false
}

// linkUser validates a pcx_ token, checks the route against its scopes and
// maps it to the person it acts for. The second value is the acting app's slug.
func (s *Server) linkUser(ctx context.Context, tok string, r *http.Request) (*store.User, string, error) {
	in, err := s.core.Introspect(ctx, tok)
	if err != nil {
		return nil, "", err
	}
	if !in.Active || !in.Delegated() || in.Audience != "time" || in.User == nil || in.User.Email == "" || in.App == nil || in.App.Slug == "" {
		return nil, "", http.ErrNoCookie
	}
	if !linkAllows(strings.Fields(in.Scope), r) {
		return nil, "", errLinkScope
	}
	u, err := s.coreIdentityUser(ctx, in)
	return u, in.App.Slug, err
}

const linkAppKey ctxKey = 2

// linkApp is the acting app's slug when the request came in on a link token.
func linkApp(ctx context.Context) string {
	a, _ := ctx.Value(linkAppKey).(string)
	return a
}
