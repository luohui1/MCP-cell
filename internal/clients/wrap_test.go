package clients

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"

	"github.com/luohui1/MCP-cell/internal/registry"
)

const wrapper = `C:\Users\me\.mcpcell\bin\mcpcell-run.exe`

func TestUnwrap(t *testing.T) {
	cases := []struct {
		e    Entry
		cmd  string
		args int
		ok   bool
	}{
		{Entry{Kind: "stdio", Command: wrapper, Args: []string{"--", "npx", "-y", "x"}}, "npx", 2, true},
		{Entry{Kind: "stdio", Command: "mcpcell-run", Args: []string{"--shared", "--", "node", "s.js"}}, "node", 1, true},
		{Entry{Kind: "stdio", Command: "/usr/bin/mcpcell-run", Args: []string{"node", "s.js"}}, "node", 1, true},
		{Entry{Kind: "stdio", Command: "mcpcell-run", Args: []string{"--"}}, "", 0, false},
		{Entry{Kind: "stdio", Command: "npx", Args: []string{"-y", "x"}}, "", 0, false},
	}
	for _, c := range cases {
		inner, ok := Unwrap(c.e)
		if ok != c.ok || ok && (inner.Command != c.cmd || len(inner.Args) != c.args) {
			t.Errorf("Unwrap(%v %v) = %q %v, %v", c.e.Command, c.e.Args, inner.Command, inner.Args, ok)
		}
	}
	wrapped := Entry{Kind: "stdio", Command: wrapper, Args: []string{"--", "cmd", "/c", "npx", "-y", "@upstash/context7-mcp"}}
	native := Entry{Kind: "stdio", Command: "npx", Args: []string{"-y", "@upstash/context7-mcp"}}
	if EntryKey(wrapped) != EntryKey(native) || LaunchPackage(wrapped) != "@upstash/context7-mcp" {
		t.Fatal("a wrapped entry must match the same server configured natively")
	}
	if class, _ := Classify(wrapped, base); class != ClassManaged {
		t.Fatalf("wrapped entry classified %s", class)
	}
}

func TestCodexWrapKeepsEverythingButTheCommand(t *testing.T) {
	for _, crlf := range []bool{false, true} {
		home := t.TempDir()
		fixture := codexFixture
		if crlf {
			fixture = strings.ReplaceAll(fixture, "\n", "\r\n")
		}
		path := filepath.Join(home, ".codex", "config.toml")
		write(t, path, fixture)
		c := NewCodex(home, t.TempDir())
		inTemp(t, c.Path())
		rec, err := c.Attach("context7", Target{Wrapper: wrapper})
		if err != nil {
			t.Fatal(err)
		}
		after := read(t, path)
		var doc struct {
			MCPServers map[string]map[string]any `toml:"mcp_servers"`
		}
		if _, err := toml.Decode(after, &doc); err != nil {
			t.Fatalf("result does not parse: %v\n%s", err, after)
		}
		c7 := doc.MCPServers["context7"]
		args, _ := c7["args"].([]any)
		if c7["command"] != wrapper || len(args) != 6 || args[0] != "--" || args[1] != "cmd" || args[5] != "@upstash/context7-mcp" {
			t.Fatalf("context7 after wrap: %v", c7)
		}
		if env, _ := c7["env"].(map[string]any); c7["enabled"] != true || c7["startup_timeout_sec"] != int64(30) || env["API_KEY"] != "k" {
			t.Fatalf("env or client policy changed: %v", c7)
		}
		if strings.Count(after, "\n") != strings.Count(fixture, "\n")-5 { // the 6-line args array became one line
			t.Fatalf("unexpected layout change:\n%s", after)
		}
		entries, _ := c.List()
		for _, e := range entries {
			if e.Name == "context7" {
				if inner, ok := Unwrap(e); !ok || inner.Command != "cmd" {
					t.Fatalf("listed entry does not unwrap: %v %v", e.Command, e.Args)
				}
			}
		}
		if err := c.Restore(rec); err != nil {
			t.Fatal(err)
		}
		if got := read(t, path); got != fixture {
			t.Fatalf("restore not byte-exact:\n%q\nwant\n%q", got, fixture)
		}
	}
}

func TestCursorWrapKeepsKeyOrderAndRestoresExactly(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".cursor", "mcp.json")
	write(t, path, cursorFixture)
	c := NewCursor(home, t.TempDir())
	inTemp(t, c.Path())
	rec, err := c.Attach("context7", Target{Wrapper: wrapper})
	if err != nil {
		t.Fatal(err)
	}
	after := read(t, path)
	entries, _ := c.List()
	for _, e := range entries {
		if e.Name != "context7" {
			continue
		}
		inner, ok := Unwrap(e)
		if !ok || e.Command != wrapper || inner.Command != "npx" || len(inner.Args) != 2 {
			t.Fatalf("context7 after wrap: %v %v env %v", e.Command, e.Args, e.EnvKeys)
		}
	}
	if strings.Index(after, `"command"`) > strings.Index(after, `"args"`) || !strings.Contains(after, "// my MCP servers") {
		t.Fatalf("member order changed:\n%s", after)
	}
	if err := c.Restore(rec); err != nil {
		t.Fatal(err)
	}
	if got := read(t, path); got != cursorFixture {
		t.Fatalf("restore not byte-exact:\n%s", got)
	}
}

func TestWrappedAdd(t *testing.T) {
	server := Entry{Kind: "stdio", Command: "npx", Args: []string{"-y", "@upstash/context7-mcp"}, Env: map[string]string{"K": "v"}}
	home := t.TempDir()
	codexPath := filepath.Join(home, ".codex", "config.toml")
	write(t, codexPath, codexFixture)
	cx := NewCodex(home, t.TempDir())
	inTemp(t, cx.Path())
	rec, err := cx.Add("ctx", Target{Wrapper: wrapper, Server: server})
	if err != nil {
		t.Fatal(err)
	}
	entries, _ := cx.List()
	found := false
	for _, e := range entries {
		if inner, ok := Unwrap(e); e.Name == "ctx" && ok && inner.Command == "npx" && e.Env["K"] == "v" {
			found = true
		}
	}
	if !found {
		t.Fatalf("added entry not found:\n%s", read(t, codexPath))
	}
	if err := cx.Restore(rec); err != nil || read(t, codexPath) != codexFixture {
		t.Fatalf("removing the added entry: %v\n%s", err, read(t, codexPath))
	}

	cursorPath := filepath.Join(home, ".cursor", "mcp.json")
	write(t, cursorPath, cursorFixture)
	cu := NewCursor(home, t.TempDir())
	inTemp(t, cu.Path())
	rec, err = cu.Add("ctx", Target{Wrapper: wrapper, Server: server})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(read(t, cursorPath), `"args": [`) {
		t.Fatalf("added entry:\n%s", read(t, cursorPath))
	}
	if err := cu.Restore(rec); err != nil || read(t, cursorPath) != cursorFixture {
		t.Fatalf("removing the added entry: %v", err)
	}
}

func TestClaudeCodeWrapViaCLI(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	path := filepath.Join(dir, ".claude.json")
	write(t, path, `{"mcpServers": {"context7": {"type": "stdio", "command": "cmd", "args": ["/c", "npx", "-y", "@upstash/context7-mcp"], "env": {"K": "v"}}}}`)
	c := NewClaudeCode(t.TempDir(), t.TempDir(), fakeClaude(t, path, nil))
	inTemp(t, c.Path())
	rec, err := c.Attach("context7", Target{Wrapper: wrapper})
	if err != nil {
		t.Fatal(err)
	}
	entries, _ := c.List()
	inner, ok := Unwrap(entries[0])
	if !ok || inner.Command != "cmd" || len(inner.Args) != 4 || entries[0].Env["K"] != "v" {
		t.Fatalf("after wrap: %v %v", entries[0].Command, entries[0].Args)
	}
	if !strings.Contains(rec.Written, `"type":"stdio","command":`) {
		t.Fatalf("member order not kept: %s", rec.Written)
	}
	if err := c.Restore(rec); err != nil {
		t.Fatal(err)
	}
	entries, _ = c.List()
	if e := entries[0]; e.Command != "cmd" || len(e.Args) != 4 {
		t.Fatalf("after restore: %v %v", e.Command, e.Args)
	}
}

func TestDriftFromTheTakenOverService(t *testing.T) {
	def := &registry.Service{Command: "python", Args: []string{"img.py"}, Env: map[string]string{"KEY": "old"}}
	wrapped := func(args []string, env map[string]string) Entry {
		return Entry{Kind: "stdio", Command: wrapper, Args: append([]string{"--"}, args...), Env: env}
	}
	if d := drift(wrapped([]string{"python", "img.py"}, map[string]string{"KEY": "old"}), def); d != "" {
		t.Fatalf("unchanged entry drifts: %q", d)
	}
	if d := drift(wrapped([]string{"python", "img.py"}, map[string]string{"KEY": "new"}), def); !strings.Contains(d, "环境变量") {
		t.Fatalf("new key: %q", d)
	}
	if d := drift(wrapped([]string{"python", "img2.py"}, map[string]string{"KEY": "old"}), def); !strings.Contains(d, "参数") {
		t.Fatalf("new script: %q", d)
	}
	if d := drift(wrapped([]string{"python", "img.py"}, nil), nil); d != "" {
		t.Fatalf("no service: %q", d)
	}
}
