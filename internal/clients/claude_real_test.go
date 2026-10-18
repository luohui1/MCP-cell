package clients

import (
	"os"
	"path/filepath"
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
