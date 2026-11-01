package httpd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/luohui1/MCP-cell/internal/clients"
)

// fakeClaudeCLI emulates `claude mcp add-json/remove -s user|local` on path;
// the local scope is the project the CLI runs in.
func fakeClaudeCLI(t *testing.T, path string) clients.Runner {
	return func(dir string, args ...string) ([]byte, error) {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var doc map[string]any
		if err := json.Unmarshal(b, &doc); err != nil {
			t.Fatal(err)
		}
		holder := doc
		if args[3] == "local" {
			projects, _ := doc["projects"].(map[string]any)
			p, _ := projects[filepath.ToSlash(dir)].(map[string]any)
			if p == nil {
				p = map[string]any{}
				projects[filepath.ToSlash(dir)] = p
			}
			holder = p
		}
		servers, _ := holder["mcpServers"].(map[string]any)
		if servers == nil {
			servers = map[string]any{}
			holder["mcpServers"] = servers
		}
		switch args[1] {
		case "remove":
			delete(servers, args[4])
		case "add-json":
			var v any
			_ = json.Unmarshal([]byte(args[5]), &v)
			servers[args[4]] = v
		}
		out, _ := json.MarshalIndent(doc, "", "  ")
		return nil, os.WriteFile(path, out, 0o600)
	}
}

// claudeDoc writes a ~/.claude.json whose projects have the given servers
// (project directory -> server names), each a fake server.
func claudeDoc(t *testing.T, path string, projects map[string][]string, tick time.Time) {
	t.Helper()
	ps := map[string]any{}
	for dir, names := range projects {
		servers := map[string]any{}
		for _, n := range names {
			servers[n] = map[string]any{"command": "fake", "args": []string{n}}
		}
		ps[filepath.ToSlash(dir)] = map[string]any{"mcpServers": servers}
	}
	b, _ := json.Marshal(map[string]any{"projects": ps})
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	_ = os.Chtimes(path, tick, tick) // a new modification time, whatever the clock's resolution
}

// Automatic takeover follows Claude Code projects: servers of the projects
// there at the first look are left for 一键托管; a project that shows up
// later (`claude mcp add` in a new project) is all new, and so is a server
// added to a known project.
func TestAutoTakeoverClaudeProjects(t *testing.T) {
	h, home := startWithHome(t)
	cfgDir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", cfgDir)
	path := filepath.Join(cfgDir, ".claude.json")
	known, later := t.TempDir(), t.TempDir()
	tick := time.Now().Add(-time.Hour)
	claudeDoc(t, path, map[string][]string{known: {"one"}}, tick)
	store, err := clients.OpenStore(home)
	if err != nil {
		t.Fatal(err)
	}
	h.srv.opts.Clients = []clients.Client{clients.NewClaudeCode(t.TempDir(), filepath.Join(home, "backups"), fakeClaudeCLI(t, path))}
	h.srv.opts.Takeovers = store
	id := func(dir string) string { return "claude-code@" + filepath.ToSlash(dir) }
	taken := func(dir, name string) bool {
		rec, ok := store.Get(id(dir), name)
		return ok && rec.Auto
	}

	h.srv.autoTakeover()
	if _, ok := store.Get(id(known), "one"); ok {
		t.Fatal("a project server there on the first look was taken over")
	}

	claudeDoc(t, path, map[string][]string{known: {"one", "two"}, later: {"three"}}, tick.Add(time.Second))
	h.srv.autoTakeover()
	if !taken(later, "three") {
		t.Fatalf("the server of a project that appeared later was not taken over: %+v", store.All())
	}
	if !taken(known, "two") {
		t.Fatal("a server added to a known project was not taken over")
	}
	if _, ok := store.Get(id(known), "one"); ok {
		t.Fatal("the server from the first look was taken over after all")
	}

	// The project's entry now runs through MCPcell, and restores.
	var doc struct {
		Projects map[string]struct {
			MCPServers map[string]struct {
				URL string `json:"url"`
			} `json:"mcpServers"`
		} `json:"projects"`
	}
	b, _ := os.ReadFile(path)
	_ = json.Unmarshal(b, &doc)
	if doc.Projects[filepath.ToSlash(later)].MCPServers["three"].URL == "" {
		t.Fatalf("the later project's entry was not pointed at MCPcell: %s", b)
	}
	rec, _ := store.Get(id(later), "three")
	c := clients.FindClient(h.srv.opts.Clients, id(later))
	if err := c.Restore(rec); err != nil {
		t.Fatal(err)
	}
}
