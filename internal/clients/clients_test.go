package clients

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"

	"github.com/luohui1/MCP-cell/internal/registry"
)

// The adapters honor CODEX_HOME and CLAUDE_CONFIG_DIR. Clear them so no test
// can ever reach the real client configs of the machine running it.
func TestMain(m *testing.M) {
	os.Unsetenv("CODEX_HOME")
	os.Unsetenv("CLAUDE_CONFIG_DIR")
	os.Exit(m.Run())
}

// inTemp fails the test unless path is under the test's temp directories.
func inTemp(t *testing.T, path string) {
	t.Helper()
	if !strings.HasPrefix(path, os.TempDir()) && !strings.Contains(path, filepath.Join("Temp", "")) {
		t.Fatalf("refusing to touch %s outside the temp dir", path)
	}
}

const base = "http://127.0.0.1:47621"
const tok = "abc123"

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestValueSpan(t *testing.T) {
	src := []byte(`{
  // comment with "quotes" and {braces}
  "a": {"x": "}", "y": [1, {"z": 2}]},
  "mcpServers": {
    "one": {"command": "npx", "args": ["-y", "p"]}, /* block */
    "two": {"url": "http://x"},
  },
}`)
	start, end, err := valueSpan(src, "mcpServers", "two")
	if err != nil || string(src[start:end]) != `{"url": "http://x"}` {
		t.Fatalf("span %q %v", src[start:end], err)
	}
	if _, _, err := valueSpan(src, "mcpServers", "three"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing key: %v", err)
	}
	if !sameJSON([]byte(`{"a": 1, "b": [2]}`), []byte("{\"b\":[2],\n // c\n \"a\":1,}")) {
		t.Fatal("sameJSON should ignore order, comments and trailing commas")
	}
}

const cursorFixture = `{
  // my MCP servers
  "mcpServers": {
    "context7": {
      "command": "npx",
      "args": ["-y", "@upstash/context7-mcp"]
    },
    "browser": { "url": "http://127.0.0.1:9010/mcp" }
  }
}
`

func TestCursorAttachRestoreIsByteExact(t *testing.T) {
	for _, crlf := range []bool{false, true} {
		home, backups := t.TempDir(), t.TempDir()
		fixture := cursorFixture
		if crlf {
			fixture = strings.ReplaceAll(fixture, "\n", "\r\n")
		}
		path := filepath.Join(home, ".cursor", "mcp.json")
		write(t, path, fixture)
		c := NewCursor(home, backups)
		inTemp(t, c.Path())
		rec, err := c.Attach("context7", base+"/s/context7", tok)
		if err != nil {
			t.Fatal(err)
		}
		after := read(t, path)
		if !strings.Contains(after, "// my MCP servers") || !strings.Contains(after, `"browser": { "url": "http://127.0.0.1:9010/mcp" }`) {
			t.Fatalf("unrelated content changed:\n%s", after)
		}
		if crlf && strings.Count(after, "\r\n") != strings.Count(after, "\n") {
			t.Fatal("line endings mixed after attach")
		}
		entries, _ := c.List()
		for _, e := range entries {
			if e.Name == "context7" && (e.URL != base+"/s/context7" || e.Command != "") {
				t.Fatalf("entry after attach: url %q command %q", e.URL, e.Command)
			}
		}
		if _, err := os.Stat(rec.Backup); err != nil {
			t.Fatal("no backup kept")
		}
		if err := c.Restore(rec); err != nil {
			t.Fatal(err)
		}
		if got := read(t, path); got != fixture {
			t.Fatalf("restore not byte-exact (crlf=%v):\n%q\nwant\n%q", crlf, got, fixture)
		}
	}
}

func TestRestoreRefusesEditedEntry(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".cursor", "mcp.json")
	write(t, path, cursorFixture)
	c := NewCursor(home, "")
	rec, _ := c.Attach("context7", base+"/s/context7", tok)
	edited := strings.Replace(read(t, path), "47621", "9999", 1)
	write(t, path, edited)
	if err := c.Restore(rec); !errors.Is(err, ErrConflict) {
		t.Fatalf("restore over a user edit: %v", err)
	}
	if read(t, path) != edited {
		t.Fatal("file changed despite conflict")
	}
}

const codexFixture = `model = "gpt-5"

# MCP servers
[mcp_servers.fastctx]
command = "fastctx"
args = ["serve"]

[mcp_servers.context7]
enabled = true
command = "cmd"
args = [
  "/c",
  "npx",
  "-y", # yes
  "@upstash/context7-mcp",
]
startup_timeout_sec = 30

[mcp_servers.context7.env]
API_KEY = "k"

# notes about the next table
[projects."D:\\work"]
trust_level = "trusted"
`

func TestCodexAttachKeepsPolicyAndRestoresExactly(t *testing.T) {
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
		entries, err := c.List()
		if err != nil || len(entries) != 2 {
			t.Fatalf("list: %d entries, %v", len(entries), err)
		}
		rec, err := c.Attach("context7", base+"/s/context7", tok)
		if err != nil {
			t.Fatal(err)
		}
		after := read(t, path)
		var doc struct {
			MCPServers map[string]map[string]any `toml:"mcp_servers"`
			Projects   map[string]map[string]any `toml:"projects"`
		}
		if _, err := toml.Decode(after, &doc); err != nil {
			t.Fatalf("result does not parse: %v\n%s", err, after)
		}
		c7 := doc.MCPServers["context7"]
		if c7["url"] != base+"/s/context7" || c7["command"] != nil || c7["env"] != nil || c7["args"] != nil {
			t.Fatalf("context7 after attach: %v", c7)
		}
		if c7["enabled"] != true || c7["startup_timeout_sec"] != int64(30) {
			t.Fatalf("client policy keys were dropped: %v", c7)
		}
		if h, _ := c7["http_headers"].(map[string]any); h["Authorization"] != "Bearer "+tok {
			t.Fatalf("headers: %v", c7["http_headers"])
		}
		if doc.MCPServers["fastctx"]["command"] != "fastctx" || doc.Projects[`D:\work`]["trust_level"] != "trusted" {
			t.Fatal("other tables changed")
		}
		if !strings.Contains(after, "# notes about the next table") || !strings.Contains(after, "# MCP servers") {
			t.Fatal("comments outside the entry were lost")
		}
		if err := c.Restore(rec); err != nil {
			t.Fatal(err)
		}
		if got := read(t, path); got != fixture {
			t.Fatalf("restore not byte-exact (crlf=%v):\n%s", crlf, got)
		}
	}
}

func TestCodexInlineTableRefused(t *testing.T) {
	home := t.TempDir()
	write(t, filepath.Join(home, ".codex", "config.toml"), "[mcp_servers]\nctx = { command = \"npx\" }\n")
	c := NewCodex(home, "")
	inTemp(t, c.Path())
	if _, err := c.Attach("ctx", base+"/s/ctx", tok); err == nil || !strings.Contains(err.Error(), "内联") {
		t.Fatalf("inline table: %v", err)
	}
}

// fakeClaude emulates `claude mcp remove/add-json -s user` on a config file.
func fakeClaude(t *testing.T, path string, failAdd *bool) Runner {
	return func(args ...string) ([]byte, error) {
		var doc map[string]any
		_ = json.Unmarshal([]byte(read(t, path)), &doc)
		servers, _ := doc["mcpServers"].(map[string]any)
		switch {
		case args[1] == "remove":
			delete(servers, args[4])
		case args[1] == "add-json":
			if failAdd != nil && *failAdd {
				*failAdd = false
				return nil, errors.New("boom")
			}
			var v map[string]any
			_ = json.Unmarshal([]byte(args[5]), &v)
			if v["command"] != nil && v["type"] == nil {
				v["type"] = "stdio" // the real CLI adds this
			}
			servers[args[4]] = v
		}
		b, _ := json.MarshalIndent(doc, "", "  ")
		write(t, path, string(b))
		return nil, nil
	}
}

func TestClaudeCodeViaCLI(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	path := filepath.Join(dir, ".claude.json")
	write(t, path, `{"numStartups": 5, "mcpServers": {"context7": {"command": "cmd", "args": ["/c", "npx", "-y", "@upstash/context7-mcp"], "env": {"K": "v"}}}}`)
	failAdd := false
	c := NewClaudeCode(t.TempDir(), t.TempDir(), fakeClaude(t, path, &failAdd))
	inTemp(t, c.Path())
	rec, err := c.Attach("context7", base+"/s/context7", tok)
	if err != nil {
		t.Fatal(err)
	}
	entries, _ := c.List()
	if entries[0].URL != base+"/s/context7" {
		t.Fatalf("after attach: url %q", entries[0].URL)
	}
	if err := c.Restore(rec); err != nil {
		t.Fatal(err)
	}
	entries, _ = c.List()
	if e := entries[0]; e.Command != "cmd" || len(e.Args) != 4 || e.Env["K"] != "v" {
		t.Fatalf("after restore: command %q args %d env keys %v", e.Command, len(e.Args), e.EnvKeys)
	}
	// A failed add after the remove puts the original back.
	failAdd = true
	if _, err := c.Attach("context7", base+"/s/context7", tok); err == nil {
		t.Fatal("expected failure")
	}
	entries, _ = c.List()
	if len(entries) != 1 || entries[0].Command != "cmd" {
		t.Fatalf("original not put back after failed add: %d entries", len(entries))
	}
}

func TestClassify(t *testing.T) {
	cases := []struct {
		e    Entry
		want Class
	}{
		{Entry{Kind: "stdio", Command: "cmd", Args: []string{"/c", "npx", "-y", "@upstash/context7-mcp"}}, ClassShared},
		{Entry{Kind: "stdio", Command: "npx", Args: []string{"chrome-devtools-mcp@latest", "--autoConnect"}}, ClassStateful},
		{Entry{Kind: "stdio", Command: "uvx", Args: []string{"mcp-server-fetch==1.0"}}, ClassShared},
		{Entry{Kind: "stdio", Command: `C:\x\mcp-server-windows-x64.exe`, Args: []string{"--app", "a"}}, ClassStateful},
		{Entry{Kind: "stdio", Client: "codex", Name: "node_repl", Command: "node"}, ClassInternal},
		{Entry{Kind: "stdio", Command: "npx", Args: []string{"-y", "@upstash/context7-mcp"}, EnvVars: []string{"PATH"}}, ClassUnknown},
		{Entry{Kind: "stdio", Command: "python", Args: []string{"my_server.py"}}, ClassUnknown},
		{Entry{Kind: "http", URL: "https://example.com/mcp"}, ClassRemote},
		{Entry{Kind: "http", URL: base + "/s/context7"}, ClassManaged},
	}
	for _, c := range cases {
		if got, why := Classify(c.e, base); got != c.want {
			t.Errorf("%v %v: got %s (%s), want %s", c.e.Command, c.e.Args, got, why, c.want)
		}
	}
}

type fakeClient struct {
	id      string
	entries []Entry
}

func (f fakeClient) ID() string                                    { return f.id }
func (f fakeClient) Name() string                                  { return f.id }
func (f fakeClient) Path() string                                  { return "" }
func (f fakeClient) List() ([]Entry, error)                        { return f.entries, nil }
func (f fakeClient) Attach(string, string, string) (Record, error) { return Record{}, nil }
func (f fakeClient) Restore(Record) error                          { return nil }

func TestOverviewMapsSameServerAcrossClients(t *testing.T) {
	c7 := func(client, cmd string, args ...string) Entry {
		return Entry{Client: client, Name: "context7", Kind: "stdio", Command: cmd, Args: args}
	}
	lark := func(client, secret string) Entry {
		e := Entry{Client: client, Name: "feishu", Kind: "stdio", Command: "npx", Args: []string{"-y", "@larksuiteoapi/lark-mcp"}}
		e.setEnv(map[string]string{"APP_SECRET": secret})
		return e
	}
	cs := []Client{
		fakeClient{"claude-code", []Entry{c7("claude-code", "cmd", "/c", "npx", "-y", "@upstash/context7-mcp"), lark("claude-code", "a")}},
		fakeClient{"codex", []Entry{c7("codex", "npx", "@upstash/context7-mcp@latest"), lark("codex", "b")}},
	}
	store, _ := OpenStore(t.TempDir())
	existing := map[string]*registry.Service{"ctx": {Command: "npx", Args: []string{"-y", "@upstash/context7-mcp"}}}
	view := Overview(cs, store, existing, base)
	got := map[string]string{}
	for _, cv := range view {
		for _, e := range cv.Entries {
			got[cv.ID+"/"+e.Name] = e.Service
		}
	}
	if got["claude-code/context7"] != "ctx" || got["codex/context7"] != "ctx" {
		t.Fatalf("same context7 should reuse the existing service: %v", got)
	}
	empty := Entry{Command: "npx", Args: []string{"-y", "@upstash/context7-mcp"}}
	empty.setEnv(map[string]string{})
	if EntryKey(empty) != DefinitionKey(existing["ctx"]) {
		t.Fatal("an empty env table must match a definition without env")
	}
	if got["claude-code/feishu"] == got["codex/feishu"] {
		t.Fatalf("different credentials must not share a service: %v", got)
	}
}
