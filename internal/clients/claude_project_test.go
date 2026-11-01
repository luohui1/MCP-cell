package clients

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

// claudeProjects is a ~/.claude.json with three projects: one with a
// local-scope server, one with only a .mcp.json (three servers: approved,
// not approved, approved but with a ${VAR}), one with neither.
func claudeProjects(t *testing.T) (c *ClaudeCode, path, withLocal, withShared string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	path = filepath.Join(dir, ".claude.json")
	withLocal, withShared, plain, empty := t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()
	write(t, filepath.Join(empty, ".mcp.json"), `{"other": "settings, no servers"}`)
	write(t, filepath.Join(withShared, ".mcp.json"), `{"mcpServers": {
		"approved": {"command": "npx", "args": ["-y", "@upstash/context7-mcp"]},
		"pending": {"command": "npx", "args": ["-y", "pending-mcp"]},
		"secret": {"command": "npx", "args": ["-y", "x-mcp"], "env": {"TOKEN": "${X_TOKEN}"}},
		"newer": {"command": "npx", "args": ["-y", "newer-mcp"]}}}`)
	write(t, filepath.Join(withShared, ".claude", "settings.local.json"), `{"enabledMcpjsonServers": ["newer"]}`)
	key := func(d string) string { return `"` + filepath.ToSlash(d) + `"` }
	write(t, path, `{"mcpServers": {"user": {"command": "npx", "args": ["-y", "@upstash/context7-mcp"]}}, "projects": {
		`+key(withLocal)+`: {"mcpServers": {"ctx": {"type": "stdio", "command": "npx", "args": ["-y", "@upstash/context7-mcp"], "env": {"K": "v"}}}, "disabledMcpServers": []},
		`+key(withShared)+`: {"mcpServers": {}, "enabledMcpjsonServers": ["approved", "secret"], "disabledMcpjsonServers": []},
		`+key(plain)+`: {"mcpServers": {}},
		`+key(empty)+`: {"mcpServers": {}},
		`+backslashed(withLocal)+`: {"mcpServers": {}}}}`)
	c = NewClaudeCode(t.TempDir(), t.TempDir(), fakeClaude(t, path, nil))
	inTemp(t, c.Path())
	return c, path, withLocal, withShared
}

// backslashed is dir as older Claude Code versions keyed it.
func backslashed(dir string) string {
	return `"` + strings.ReplaceAll(filepath.ToSlash(dir), "/", `\\`) + `"`
}

func byName(entries []Entry) map[string]Entry {
	out := map[string]Entry{}
	for _, e := range entries {
		out[e.Name] = e
	}
	return out
}

// Only projects with servers of their own become clients; the user's list
// stays the user's.
func TestClaudeProjectsListed(t *testing.T) {
	c, _, withLocal, withShared := claudeProjects(t)
	ps := c.Projects()
	if len(ps) != 2 {
		t.Fatalf("%d projects, want the two with servers (once each, however keyed)", len(ps))
	}
	ids := map[string]Client{}
	for _, p := range ps {
		ids[p.ID()] = p
	}
	local, shared := ids["claude-code@"+filepath.ToSlash(withLocal)], ids["claude-code@"+filepath.ToSlash(withShared)]
	if local == nil || shared == nil {
		t.Fatalf("project ids: %v", ids)
	}
	if !strings.HasPrefix(local.Name(), "Claude Code · ") || local.Path() != c.Path() {
		t.Fatalf("name %q path %q", local.Name(), local.Path())
	}
	user, _ := c.List()
	if len(user) != 1 || user[0].Name != "user" {
		t.Fatalf("user scope lists %d entries", len(user))
	}

	got, _ := local.List()
	if e := byName(got)["ctx"]; len(got) != 1 || !e.Enabled || e.Shared || e.Hold != "" || e.Env["K"] != "v" {
		t.Fatalf("local scope: %+v", got)
	}
	got, _ = shared.List()
	es := byName(got)
	if len(es) != 4 || !es["approved"].Shared || !es["approved"].Enabled || es["approved"].Hold != "" {
		t.Fatalf("approved .mcp.json server: %+v", es["approved"])
	}
	if es["pending"].Enabled || es["pending"].Hold == "" {
		t.Fatalf("a server the user has not approved must be held: %+v", es["pending"])
	}
	if !es["secret"].Enabled || !strings.Contains(es["secret"].Hold, "${") {
		t.Fatalf("a server with ${VAR} must be held: %+v", es["secret"])
	}
	if !es["newer"].Enabled || es["newer"].Hold != "" {
		t.Fatalf("approved in .claude/settings.local.json: %+v", es["newer"])
	}
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range Overview([]Client{shared}, store, nil, base)[0].Entries {
		if Takeable(v) != (v.Name == "approved" || v.Name == "newer") {
			t.Fatalf("%s takeable = %v", v.Name, Takeable(v))
		}
	}
	all := Expand([]Client{c})
	if len(all) != 3 || all[0] != Client(c) {
		t.Fatalf("Expand: %d clients", len(all))
	}
}

// A local-scope entry is changed in place, through the CLI run in the
// project, and put back.
func TestClaudeProjectLocalAttachRestore(t *testing.T) {
	c, path, withLocal, _ := claudeProjects(t)
	p := c.Project(withLocal)
	before := read(t, path)
	rec, err := p.Attach("ctx", Target{URL: base + "/s/ctx", Token: tok, Wrapper: wrapper})
	if err != nil {
		t.Fatal(err)
	}
	if rec.Client != p.ID() || rec.Override || rec.Added {
		t.Fatalf("record %+v", rec)
	}
	got, _ := p.List()
	if e := got[0]; !strings.Contains(e.Command, "mcpcell-run") || e.Env["K"] != "v" {
		t.Fatalf("after attach: %+v", e)
	}
	if u, _ := c.List(); u[0].Command != "npx" {
		t.Fatalf("the user scope changed: %+v", u[0])
	}
	if err := p.Restore(rec); err != nil {
		t.Fatal(err)
	}
	got, _ = p.List()
	if e := got[0]; e.Command != "npx" || len(e.Args) != 2 || e.Env["K"] != "v" {
		t.Fatalf("after restore: %+v", e)
	}
	if !strings.Contains(before, "ctx") {
		t.Fatal("fixture lost")
	}
}

// A .mcp.json server is taken over by a local entry of the same name; the
// shared file is never written, and restoring removes the local entry.
func TestClaudeProjectSharedOverride(t *testing.T) {
	c, _, _, withShared := claudeProjects(t)
	p := c.Project(withShared)
	mcpJSON := filepath.Join(withShared, ".mcp.json")
	shared := read(t, mcpJSON)
	rec, err := p.Attach("approved", Target{URL: base + "/s/approved", Token: tok, Wrapper: wrapper})
	if err != nil {
		t.Fatal(err)
	}
	if !rec.Override || rec.Added || !strings.Contains(rec.Original, "context7") {
		t.Fatalf("record %+v", rec)
	}
	es := byName(must(p.List()))
	if e := es["approved"]; e.Shared || !strings.Contains(e.Command, "mcpcell-run") {
		t.Fatalf("the local override should be what is listed: %+v", e)
	}
	if read(t, mcpJSON) != shared {
		t.Fatal(".mcp.json was written")
	}
	if err := p.Restore(rec); err != nil {
		t.Fatal(err)
	}
	if e := byName(must(p.List()))["approved"]; !e.Shared || e.Command != "npx" {
		t.Fatalf("after restore the shared entry should apply again: %+v", e)
	}
	if read(t, mcpJSON) != shared {
		t.Fatal(".mcp.json was written")
	}

	for _, name := range []string{"pending", "secret"} {
		if _, err := p.Attach(name, Target{URL: base + "/s/x", Token: tok}); err == nil {
			t.Fatalf("%s was taken over", name)
		}
	}
	if _, err := p.Add("new", Target{URL: base + "/s/new", Token: tok}); !errors.Is(err, ErrProjectAdd) {
		t.Fatalf("add to a project: %v", err)
	}
}

// Records name the project client; it is found again by id, even after the
// project's servers are gone.
func TestFindClaudeProject(t *testing.T) {
	c, _, withLocal, _ := claudeProjects(t)
	id := "claude-code@" + filepath.ToSlash(withLocal)
	found := FindClient([]Client{c}, id)
	if found == nil || found.ID() != id {
		t.Fatalf("found %v", found)
	}
	if FindClient([]Client{c}, "claude-code@"+filepath.ToSlash(t.TempDir())) == nil {
		t.Fatal("a project without servers is not found by id")
	}
	if got := clientSlug(id); !strings.HasPrefix(got, "claude-") || strings.ContainsAny(got, "@:/\\") {
		t.Fatalf("slug %q", got)
	}
}

func must(entries []Entry, err error) []Entry {
	if err != nil {
		panic(err)
	}
	return entries
}
