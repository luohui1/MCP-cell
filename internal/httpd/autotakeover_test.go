package httpd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/luohui1/MCP-cell/internal/clients"
)

// autoAgent is a Cursor configuration in a temporary home, watched by the
// server's automatic takeover.
type autoAgent struct {
	h     *harness
	file  string
	store *clients.Store
	tick  time.Time
}

func newAutoAgent(t *testing.T, entries string) *autoAgent {
	t.Helper()
	h, home := startWithHome(t)
	userHome := t.TempDir()
	a := &autoAgent{h: h, file: filepath.Join(userHome, ".cursor", "mcp.json"), tick: time.Now().Add(-time.Hour)}
	_ = os.MkdirAll(filepath.Dir(a.file), 0o700)
	a.write(t, entries)
	store, err := clients.OpenStore(home)
	if err != nil {
		t.Fatal(err)
	}
	a.store = store
	h.srv.opts.Clients = []clients.Client{clients.NewCursor(userHome, filepath.Join(home, "backups"))}
	h.srv.opts.Takeovers = store
	return a
}

// write replaces the agent's entries, as the user editing the file would.
func (a *autoAgent) write(t *testing.T, entries string) {
	t.Helper()
	if err := os.WriteFile(a.file, []byte(`{"mcpServers": {`+entries+`}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	a.tick = a.tick.Add(time.Second) // a new modification time, whatever the clock's resolution
	_ = os.Chtimes(a.file, a.tick, a.tick)
}

func (a *autoAgent) taken(name string) bool {
	_, ok := a.store.Get("cursor", name)
	return ok
}

func (a *autoAgent) content(t *testing.T) string {
	b, err := os.ReadFile(a.file)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

const (
	oldEntry    = `"old": { "command": "fake", "args": ["old"] }`
	freshEntry  = `"fresh": { "command": "fake", "args": ["fresh"] }`
	brokenEntry = `"broken": { "command": "missing-server" }`
)

// Entries already there on the first look are not new; one added later is
// taken over, one that does not start is left as it is and not tried again.
func TestAutoTakeoverNewEntries(t *testing.T) {
	a := newAutoAgent(t, oldEntry)
	a.h.srv.autoTakeover()
	if a.taken("old") {
		t.Fatal("an entry that was there on the first look was taken over")
	}
	a.write(t, oldEntry+","+freshEntry+","+brokenEntry)
	a.h.srv.autoTakeover()
	rec, ok := a.store.Get("cursor", "fresh")
	if !ok || !rec.Auto {
		t.Fatalf("new entry not taken over: %+v %v", rec, ok)
	}
	if a.taken("old") || a.taken("broken") {
		t.Fatal("taken over: old or broken")
	}
	if !strings.Contains(a.content(t), "missing-server") {
		t.Fatal("the entry that does not start was rewritten")
	}
	ev := a.store.Events()
	if len(ev) != 2 {
		t.Fatalf("events: %+v", ev)
	}
	for _, e := range ev {
		if e.Name == "fresh" && (!e.OK || e.Service == "") || e.Name == "broken" && (e.OK || e.Error == "") {
			t.Fatalf("event: %+v", e)
		}
	}
	// Nothing changed: the file is not even read. Touched: broken is not tried again.
	a.h.srv.autoTakeover()
	a.write(t, strings.TrimSuffix(a.content(t)[len(`{"mcpServers": {`):], "}}"))
	a.h.srv.autoTakeover()
	if n := len(a.store.Events()); n != 2 {
		t.Fatalf("%d events, want no new attempt", n)
	}
	code, body := a.h.apiCall(t, "GET", "/api/auto", "")
	if code != 200 || !strings.Contains(body, `"enabled":true`) || !strings.Contains(body, `"name":"fresh"`) {
		t.Fatalf("GET /api/auto = %d %s", code, body)
	}
}

// An entry the user restored is never taken over by itself again, even
// when its settings change later; taking it over by hand is still possible.
func TestAutoTakeoverRespectsRestore(t *testing.T) {
	a := newAutoAgent(t, oldEntry)
	a.h.srv.autoTakeover()
	a.write(t, oldEntry+","+freshEntry)
	a.h.srv.autoTakeover()
	if !a.taken("fresh") {
		t.Fatal("not taken over")
	}
	if code, body := a.h.apiCall(t, "POST", "/api/clients/cursor/restore", `{"names":["fresh"]}`); code != 200 || !strings.Contains(body, `"ok":true`) {
		t.Fatalf("restore = %d %s", code, body)
	}
	if e, no := a.store.Excluded("cursor", "fresh"); !no || e.Reason != "restored" {
		t.Fatalf("not excluded after restore: %+v", e)
	}
	a.write(t, oldEntry+`,"fresh": { "command": "fake", "args": ["fresh", "--v2"] }`)
	a.h.srv.autoTakeover()
	if a.taken("fresh") {
		t.Fatal("a restored entry was taken over again")
	}
	if code, body := a.h.apiCall(t, "POST", "/api/clients/cursor/attach", `{"names":["fresh"]}`); code != 200 || !strings.Contains(body, `"ok":true`) {
		t.Fatalf("attach by hand = %d %s", code, body)
	}
	if _, no := a.store.Excluded("cursor", "fresh"); no {
		t.Fatal("still excluded after a takeover by hand")
	}

	// Turned off: new entries stay as they are, and are not swept up later.
	if code, _ := a.h.apiCall(t, "POST", "/api/auto", `{"enabled":false}`); code != 200 {
		t.Fatal("cannot turn it off")
	}
	a.write(t, strings.TrimSuffix(a.content(t)[len(`{"mcpServers": {`):], "}}")+`,"later": { "command": "fake", "args": ["later"] }`)
	a.h.srv.autoTakeover()
	a.h.apiCall(t, "POST", "/api/auto", `{"enabled":true}`)
	a.write(t, strings.TrimSuffix(a.content(t)[len(`{"mcpServers": {`):], "}}"))
	a.h.srv.autoTakeover()
	if a.taken("later") {
		t.Fatal("added while it was off, taken over once it was on again")
	}
}

// An agent that puts the original entry back (Codex rewriting config.toml)
// is followed once; the second time MCPcell stops and says so.
func TestAutoTakeoverFollowsAgentOnce(t *testing.T) {
	a := newAutoAgent(t, oldEntry)
	a.h.srv.autoTakeover()
	a.write(t, oldEntry+","+freshEntry)
	a.h.srv.autoTakeover()
	if !a.taken("fresh") {
		t.Fatal("not taken over")
	}
	a.write(t, oldEntry+","+freshEntry) // the agent's rewrite
	a.h.srv.autoTakeover()
	rec, ok := a.store.Get("cursor", "fresh")
	if !ok || rec.Reattached != 1 || strings.Contains(a.content(t), `"args": ["fresh"]`) && !strings.Contains(a.content(t), "/s/") {
		t.Fatalf("not taken over again after the agent put it back: %+v %v\n%s", rec, ok, a.content(t))
	}
	a.write(t, oldEntry+","+freshEntry) // and again
	a.h.srv.autoTakeover()
	if e, no := a.store.Excluded("cursor", "fresh"); !no || e.Reason != "lost" {
		t.Fatalf("still fighting the agent: %+v", e)
	}
	if strings.Contains(a.content(t), "/s/") {
		t.Fatal("rewrote the entry a third time")
	}
	ev := a.store.Events()
	if last := ev[len(ev)-1]; last.OK || !strings.Contains(last.Error, "停止自动托管") {
		t.Fatalf("last event: %+v", last)
	}
	a.write(t, oldEntry+","+freshEntry)
	a.h.srv.autoTakeover()
	if n := len(a.store.Events()); n != len(ev) {
		t.Fatal("tried again after stopping")
	}
}

// The page iterates the lists: they are [] rather than null when empty.
func TestAutoStatusEmptyLists(t *testing.T) {
	a := newAutoAgent(t, oldEntry)
	if code, body := a.h.apiCall(t, "GET", "/api/auto", ""); code != 200 || !strings.Contains(body, `"events":[]`) || !strings.Contains(body, `"excluded":[]`) {
		t.Fatalf("GET /api/auto = %d %s", code, body)
	}
}
