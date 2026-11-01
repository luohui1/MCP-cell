package httpd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/luohui1/MCP-cell/internal/clients"
)

// One app's MCP is added to the other apps; once the user takes it back
// from an app, sync leaves that app alone.
func TestSyncApps(t *testing.T) {
	h, home := startWithHome(t)
	userHome := t.TempDir()
	codexHome := filepath.Join(userHome, ".codex")
	t.Setenv("CODEX_HOME", codexHome) // never the real one
	cursorFile := filepath.Join(userHome, ".cursor", "mcp.json")
	codexFile := filepath.Join(codexHome, "config.toml")
	_ = os.MkdirAll(filepath.Dir(cursorFile), 0o700)
	_ = os.MkdirAll(codexHome, 0o700)
	_ = os.WriteFile(cursorFile, []byte(`{"mcpServers": {"ctx": {"command": "fake", "args": ["ctx"]}, "off": {"command": "fake", "args": ["off"], "disabled": true}}}`), 0o600)
	const codexOriginal = "model = \"x\"\n"
	_ = os.WriteFile(codexFile, []byte(codexOriginal), 0o600)
	store, _ := clients.OpenStore(home)
	h.srv.opts.Clients = []clients.Client{clients.NewCursor(userHome, ""), clients.NewCodex(userHome, "")}
	h.srv.opts.Takeovers = store
	// Wrapped, a taken-over entry keeps its other members, "disabled" too.
	h.srv.opts.Wrapper = filepath.Join(t.TempDir(), "mcpcell-run.exe")

	sync := func() []syncResult {
		t.Helper()
		code, body := h.apiCall(t, "POST", "/api/sync", "{}")
		var out struct{ Results []syncResult }
		if code != 200 || json.Unmarshal([]byte(body), &out) != nil {
			t.Fatalf("POST /api/sync = %d %s", code, body)
		}
		return out.Results
	}
	if r := sync(); len(r) != 0 {
		t.Fatalf("nothing runs through MCPcell yet, still synced: %+v", r)
	}
	// off is turned off in Cursor, the only app that has it: not spread.
	if _, body := h.apiCall(t, "POST", "/api/clients/cursor/attach", `{"names":["ctx","off"]}`); strings.Count(body, `"ok":true`) != 2 {
		t.Fatalf("attach: %s", body)
	}
	// The page sees beforehand where it would go.
	syncTo := func() []string {
		t.Helper()
		_, body := h.apiCall(t, "GET", "/api/mcp", "")
		var v struct {
			Rows []struct {
				Service string
				SyncTo  []string
			}
		}
		_ = json.Unmarshal([]byte(body), &v)
		for _, r := range v.Rows {
			if r.Service == "ctx" {
				return r.SyncTo
			}
		}
		return nil
	}
	if got := syncTo(); len(got) != 1 || got[0] != "codex" {
		t.Fatalf("syncTo: %v", got)
	}
	if r := sync(); len(r) != 1 || r[0] != (syncResult{Client: "codex", Service: "ctx", OK: true}) {
		t.Fatalf("sync: %+v", r)
	}
	if got := syncTo(); len(got) != 0 {
		t.Fatalf("syncTo after the sync: %v", got)
	}
	if b, _ := os.ReadFile(codexFile); !strings.Contains(string(b), "[mcp_servers.ctx]") {
		t.Fatalf("not added to codex:\n%s", b)
	}
	if r := sync(); len(r) != 0 {
		t.Fatalf("synced twice: %+v", r)
	}

	// Taken back from Codex: the file is as it was, and it is not added again.
	if _, body := h.apiCall(t, "POST", "/api/clients/codex/restore", `{"names":["ctx"]}`); !strings.Contains(body, `"ok":true`) {
		t.Fatalf("restore: %s", body)
	}
	if b, _ := os.ReadFile(codexFile); string(b) != codexOriginal {
		t.Fatalf("codex after restore:\n%s", b)
	}
	if r := sync(); len(r) != 0 {
		t.Fatalf("added again after the user took it back: %+v", r)
	}
}

// 全部还原 does not keep the sync out: the next 一键托管 brings every app
// the same set again.
func TestRestoreAllLetsSyncAddAgain(t *testing.T) {
	h, home := startWithHome(t)
	userHome := t.TempDir()
	codexHome := filepath.Join(userHome, ".codex")
	t.Setenv("CODEX_HOME", codexHome) // never the real one
	cursorFile := filepath.Join(userHome, ".cursor", "mcp.json")
	_ = os.MkdirAll(filepath.Dir(cursorFile), 0o700)
	_ = os.MkdirAll(codexHome, 0o700)
	_ = os.WriteFile(cursorFile, []byte(`{"mcpServers": {"ctx": {"command": "fake", "args": ["ctx"]}}}`), 0o600)
	_ = os.WriteFile(filepath.Join(codexHome, "config.toml"), []byte("model = \"x\"\n"), 0o600)
	store, _ := clients.OpenStore(home)
	h.srv.opts.Clients = []clients.Client{clients.NewCursor(userHome, ""), clients.NewCodex(userHome, "")}
	h.srv.opts.Takeovers = store
	h.srv.opts.Wrapper = filepath.Join(t.TempDir(), "mcpcell-run.exe")

	synced := func() int {
		t.Helper()
		_, body := h.apiCall(t, "POST", "/api/sync", "{}")
		return strings.Count(body, `"ok":true`)
	}
	h.apiCall(t, "POST", "/api/clients/cursor/attach", `{"names":["ctx"]}`)
	if n := synced(); n != 1 {
		t.Fatalf("first sync added %d", n)
	}
	for _, c := range []string{"cursor", "codex"} {
		if _, body := h.apiCall(t, "POST", "/api/clients/"+c+"/restore?all=1", `{"names":["ctx"]}`); !strings.Contains(body, `"ok":true`) {
			t.Fatalf("restore %s: %s", c, body)
		}
	}
	h.apiCall(t, "POST", "/api/clients/cursor/attach", `{"names":["ctx"]}`)
	if n := synced(); n != 1 {
		t.Fatalf("after 全部还原 and 一键托管, sync added %d", n)
	}
}

// What automatic takeover takes over in one app reaches the others.
func TestAutoTakeoverSyncs(t *testing.T) {
	a := newAutoAgent(t, oldEntry)
	userHome := filepath.Dir(filepath.Dir(a.file))
	codexHome := filepath.Join(userHome, ".codex")
	t.Setenv("CODEX_HOME", codexHome) // never the real one
	_ = os.MkdirAll(codexHome, 0o700)
	_ = os.WriteFile(filepath.Join(codexHome, "config.toml"), []byte("model = \"x\"\n"), 0o600)
	a.h.srv.opts.Clients = append(a.h.srv.opts.Clients, clients.NewCodex(userHome, ""))

	a.h.srv.autoTakeover() // the first look
	a.write(t, oldEntry+","+freshEntry)
	a.h.srv.autoTakeover()
	if !a.taken("fresh") {
		t.Fatal("not taken over")
	}
	if rec, ok := a.store.Get("codex", "fresh"); !ok || !rec.Added {
		t.Fatalf("not added to codex: %+v", a.store.All())
	}
	if _, ok := a.store.Get("codex", "old"); ok {
		t.Fatal("an entry not run through MCPcell was synced")
	}
	a.h.srv.autoTakeover() // codex changed: nothing new to take over or add
	if n := len(a.store.All()); n != 2 {
		t.Fatalf("%d records after another look", n)
	}
}
