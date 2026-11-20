package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-time/internal/config"
)

// moveLinks starts Time behind a fake Core whose pcx_<who>_<export|import|read>
// tokens are Pocket Move links acting for alice@ or bob@.
func moveLinks(t *testing.T) *app {
	t.Helper()
	coreSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/api/v1/auth/introspect" {
			io.WriteString(w, `{}`)
			return
		}
		var in struct {
			Token string `json:"token"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		parts := strings.SplitN(strings.TrimPrefix(in.Token, "pcx_"), "_", 2)
		if len(parts) != 2 {
			io.WriteString(w, `{"active":false}`)
			return
		}
		scope := map[string]string{"export": core.ScopeMoveExport, "import": core.ScopeMoveImport, "read": "time:read"}[parts[1]]
		email := parts[0] + "@example.com"
		fmt.Fprintf(w, `{"active":true,"kind":"delegated","scope":%q,"audience":"time","user":{"id":1,"sub":%q,"email":%q,"name":%q,"role":"member"},"app":{"id":7,"slug":"move","name":"Pocket Move"}}`, scope, email, email, parts[0])
	}))
	t.Cleanup(coreSrv.Close)
	return newApp(t, func(c *config.Config) { c.CoreURL = coreSrv.URL })
}

func (a *app) raw(tok, method, path, ctype string, body []byte) (int, []byte) {
	a.t.Helper()
	req, _ := http.NewRequest(method, a.url+path, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+tok)
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		a.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, b
}

func (a *app) moveImport(tok, path, ctype string, data []byte) map[string]any {
	a.t.Helper()
	code, out := a.raw(tok, "POST", path, ctype, data)
	if code != 200 {
		a.t.Fatalf("POST %s → %d %s", path, code, out)
	}
	var m map[string]any
	_ = json.Unmarshal(out, &m)
	return m
}

func TestMoveLinkScopes(t *testing.T) {
	a := moveLinks(t)
	exp, imp, read := "pcx_bob_export", "pcx_bob_import", "pcx_bob_read"
	for _, c := range []struct {
		tok, method, path string
		want              int
	}{
		{exp, "GET", core.PortablePath, 200},
		{exp, "GET", "/api/v1/export.json", 200},
		{exp, "GET", "/api/v1/export.csv", 200},
		{exp, "POST", "/api/v1/import", 403},
		{exp, "POST", "/api/v1/import/csv", 403},
		{exp, "GET", "/api/v1/settings", 403},
		{exp, "GET", "/api/v1/entries", 403},
		{exp, "GET", "/api/v1/backup", 403},
		{imp, "GET", core.PortablePath, 200},
		{imp, "GET", "/api/v1/export.json", 403},
		{imp, "PUT", "/api/v1/settings", 403},
		{imp, "POST", "/api/v1/entries", 403},
		{imp, "POST", "/api/v1/timer/start", 403},
		{read, "GET", core.PortablePath, 403},
		{read, "GET", "/api/v1/export.json", 403},
	} {
		if code, body := a.raw(c.tok, c.method, c.path, "application/json", []byte("{}")); code != c.want {
			t.Errorf("%s %s %s → %d, want %d (%s)", c.tok, c.method, c.path, code, c.want, body)
		}
	}
	_, body := a.raw(exp, "GET", core.PortablePath, "", nil)
	var p core.Portable
	if err := json.Unmarshal(body, &p); err != nil || len(p.Exports) != 2 || len(p.Imports) != 2 || p.Exports[0].Kind != core.KindPocket {
		t.Fatalf("portable: %s", body)
	}
}

// Move brings Alice's time to Bob: each import adds once, her own file back
// adds nothing, and nothing of one person shows up for the other.
func TestMoveImportsAddOnce(t *testing.T) {
	a := moveLinks(t)
	p := a.ok(a.alice, "POST", "/api/v1/projects", map[string]any{"name": "Book"})
	a.ok(a.alice, "POST", "/api/v1/entries", map[string]any{"title": "Chapter 1", "project_id": num(p, "id"), "started_at": time.Now().Add(-3 * time.Hour).Format(time.RFC3339), "duration_s": 1200})

	code, export := a.raw("pcx_alice_export", "GET", "/api/v1/export.json", "", nil)
	if code != 200 || !strings.Contains(string(export), "Chapter 1") {
		t.Fatalf("alice's export: %d %s", code, export)
	}
	if _, bob := a.raw("pcx_bob_export", "GET", "/api/v1/export.json", "", nil); strings.Contains(string(bob), "Chapter 1") {
		t.Fatalf("bob's export holds alice's time: %s", bob)
	}
	if own := a.moveImport("pcx_alice_import", "/api/v1/import", "application/json", export); num(own, "entries")+num(own, "projects") != 0 {
		t.Fatalf("alice's own file back: %v", own)
	}

	first := a.moveImport("pcx_bob_import", "/api/v1/import", "application/json", export)
	second := a.moveImport("pcx_bob_import", "/api/v1/import", "application/json", export)
	if num(first, "entries") != 1 || num(first, "projects") != 1 || num(second, "entries")+num(second, "projects") != 0 || num(second, "already") != 1 {
		t.Fatalf("imports: %v then %v", first, second)
	}

	csv := []byte("Project,Description,Start date,Start time,End date,End time\nGarden,Digging,2026-10-01,09:00:00,2026-10-01,10:30:00\n")
	c1 := a.moveImport("pcx_bob_import", "/api/v1/import/csv", "text/csv", csv)
	c2 := a.moveImport("pcx_bob_import", "/api/v1/import/csv", "text/csv", csv)
	if num(c1, "entries") != 1 || num(c2, "entries") != 0 || num(c2, "already") != 1 {
		t.Fatalf("csv imports: %v then %v", c1, c2)
	}

	if l := a.ok(a.bob, "GET", "/api/v1/entries?from=2026-01-01", nil); num(l, "total") != 2 {
		t.Fatalf("bob's entries: %v", l["total"])
	}
	if l := a.ok(a.alice, "GET", "/api/v1/entries?from=2026-01-01", nil); num(l, "total") != 1 {
		t.Fatalf("alice's entries changed: %v", l["total"])
	}
}
