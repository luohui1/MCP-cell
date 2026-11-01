package clients

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Runs the real `claude mcp` CLI against a throwaway CLAUDE_CONFIG_DIR.
// Opt in with MCPCELL_REAL_CLAUDE=1; it never touches ~/.claude.json.
func TestRealClaudeCLI(t *testing.T) {
	if os.Getenv("MCPCELL_REAL_CLAUDE") != "1" {
		t.Skip("set MCPCELL_REAL_CLAUDE=1 to run against the installed claude CLI")
	}
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	path := filepath.Join(dir, ".claude.json")
	write(t, path, `{"mcpServers":{"demo":{"type":"stdio","command":"cmd","args":["/c","npx","-y","@upstash/context7-mcp"],"env":{"K":"v"}}}}`)
	c := NewClaudeCode(dir, t.TempDir(), nil)
	inTemp(t, c.Path())
	rec, err := c.Attach("demo", Target{URL: base + "/s/demo", Token: tok})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Restore(rec); err != nil {
		t.Fatal(err)
	}
	entries, _ := c.List()
	if len(entries) != 1 || entries[0].Command != "cmd" || len(entries[0].Args) != 4 || entries[0].Env["K"] != "v" {
		t.Fatalf("restore through the real CLI lost data: %d entries", len(entries))
	}
}

// A project's local-scope entry and an approved .mcp.json entry, taken over
// and restored through the real CLI run in the project.
func TestRealClaudeProject(t *testing.T) {
	if os.Getenv("MCPCELL_REAL_CLAUDE") != "1" {
		t.Skip("set MCPCELL_REAL_CLAUDE=1 to run against the installed claude CLI")
	}
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	project := t.TempDir()
	write(t, filepath.Join(project, ".mcp.json"), `{"mcpServers":{"shared":{"command":"npx","args":["-y","@upstash/context7-mcp"]}}}`)
	mcpJSON := read(t, filepath.Join(project, ".mcp.json"))
	if _, err := cli(project, "mcp", "add-json", "-s", "local", "mine", `{"type":"stdio","command":"npx","args":["-y","@upstash/context7-mcp"],"env":{"K":"v"}}`); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, ".claude.json")
	// Approve the shared server, as answering Claude Code's prompt would.
	var doc map[string]any
	if err := json.Unmarshal([]byte(read(t, path)), &doc); err != nil {
		t.Fatal(err)
	}
	doc["projects"].(map[string]any)[filepath.ToSlash(project)].(map[string]any)["enabledMcpjsonServers"] = []string{"shared"}
	b, _ := json.Marshal(doc)
	write(t, path, string(b))

	c := NewClaudeCode(dir, t.TempDir(), nil)
	inTemp(t, c.Path())
	p := c.Project(project)
	for _, name := range []string{"mine", "shared"} {
		rec, err := p.Attach(name, Target{Wrapper: wrapper})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if e := byName(must(p.List()))[name]; !strings.Contains(e.Command, "mcpcell-run") {
			t.Fatalf("%s after attach: %+v", name, e)
		}
		if err := p.Restore(rec); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if e := byName(must(p.List()))[name]; e.Command != "npx" || len(e.Args) != 2 {
			t.Fatalf("%s after restore: %+v", name, e)
		}
	}
	if read(t, filepath.Join(project, ".mcp.json")) != mcpJSON {
		t.Fatal(".mcp.json was written")
	}
}
