package clients

import (
	"path/filepath"
	"strings"
	"testing"
)

// Every client's file is under the home it was given, so tests and trial
// daemons with another home never reach the machine's real configs.
func TestAllStaysInHome(t *testing.T) {
	home := t.TempDir()
	cs := All(home, t.TempDir())
	if len(cs) < 10 {
		t.Fatalf("only %d clients", len(cs))
	}
	seen := map[string]bool{}
	for _, c := range cs {
		if !strings.HasPrefix(c.Path(), home) {
			t.Fatalf("%s reads %s, outside home", c.ID(), c.Path())
		}
		if seen[c.ID()] {
			t.Fatalf("%s twice", c.ID())
		}
		seen[c.ID()] = true
		// A missing file is an agent that is not installed, not one without servers.
		if entries, err := c.List(); entries != nil || err != nil {
			t.Fatalf("%s without a file: %v, %v", c.ID(), entries, err)
		}
	}
}

func jsonClient(t *testing.T, id, home string) *JSONClient {
	t.Helper()
	for _, a := range jsonAgents {
		if a.id == id {
			c := newJSONClient(a, home, t.TempDir())
			inTemp(t, c.Path())
			return c
		}
	}
	t.Fatalf("no agent %s", id)
	return nil
}

func entriesByName(t *testing.T, c Client) map[string]Entry {
	t.Helper()
	list, err := c.List()
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]Entry{}
	for _, e := range list {
		out[e.Name] = e
	}
	return out
}

const vscodeFixture = `{
	// servers for Copilot
	"servers": {
		"ctx": {
			"type": "stdio",
			"command": "npx",
			"args": ["-y", "@upstash/context7-mcp"],
		},
		"remote": { "type": "http", "url": "https://example.com/mcp" },
		"keyed": { "command": "npx", "args": ["-y", "tavily-mcp"], "env": { "TAVILY_API_KEY": "${input:tavily}" } },
	},
	"inputs": [],
}
`

// VS Code: servers under "servers", comments and trailing commas, variables
// it fills in itself.
func TestVSCodeServers(t *testing.T) {
	home := t.TempDir()
	c := jsonClient(t, "vscode", home)
	write(t, c.Path(), vscodeFixture)
	es := entriesByName(t, c)
	if es["ctx"].Kind != "stdio" || es["ctx"].Hold != "" || es["remote"].Kind != "http" || es["keyed"].Hold == "" {
		t.Fatalf("entries: %+v", es)
	}
	view := func(e Entry) EntryView {
		v := EntryView{Entry: e}
		v.Class, v.Reason = Classify(e, base)
		return v
	}
	if !Takeable(view(es["ctx"])) || Takeable(view(es["keyed"])) {
		t.Fatal("only the entry without variables can be taken over")
	}
	rec, err := c.Attach("ctx", Target{Wrapper: wrapper})
	if err != nil {
		t.Fatal(err)
	}
	after := read(t, c.Path())
	inner, ok := Unwrap(entriesByName(t, c)["ctx"])
	if !ok || inner.Command != "npx" || !strings.Contains(after, "// servers for Copilot") || !strings.Contains(after, `"type": "stdio"`) {
		t.Fatalf("after wrapping:\n%s", after)
	}
	if err := c.Restore(rec); err != nil {
		t.Fatal(err)
	}
	if got := read(t, c.Path()); got != vscodeFixture {
		t.Fatalf("restore not byte-exact:\n%s", got)
	}
}

const opencodeFixture = `{
  "$schema": "https://opencode.ai/config.json",
  "mcp": {
    "ctx": {
      "type": "local",
      "command": ["npx", "-y", "@upstash/context7-mcp"],
      "environment": { "K": "v" },
      "enabled": true
    },
    "off": { "type": "local", "command": ["uvx", "mcp-server-time"], "enabled": false }
  }
}
`

// OpenCode: the command and its arguments in one array, "environment",
// "enabled".
func TestOpenCodeCommandArray(t *testing.T) {
	home := t.TempDir()
	c := jsonClient(t, "opencode", home)
	write(t, c.Path(), opencodeFixture)
	es := entriesByName(t, c)
	if e := es["ctx"]; e.Command != "npx" || len(e.Args) != 2 || e.Env["K"] != "v" || !e.Enabled || es["off"].Enabled {
		t.Fatalf("entries: %+v", es)
	}
	rec, err := c.Attach("ctx", Target{Wrapper: wrapper})
	if err != nil {
		t.Fatal(err)
	}
	e := entriesByName(t, c)["ctx"]
	inner, ok := Unwrap(e)
	if !ok || inner.Command != "npx" || len(inner.Args) != 2 || e.Env["K"] != "v" {
		t.Fatalf("after wrapping: %+v\n%s", e, read(t, c.Path()))
	}
	if class, _ := Classify(e, base); class != ClassManaged {
		t.Fatalf("wrapped entry classed %s", class)
	}
	if err := c.Restore(rec); err != nil {
		t.Fatal(err)
	}
	if got := read(t, c.Path()); got != opencodeFixture {
		t.Fatalf("restore not byte-exact:\n%s", got)
	}
}

// Gemini CLI keeps servers among its other settings; Windsurf and Gemini
// name remote URLs their own way.
func TestSettingsFileAndRemoteNames(t *testing.T) {
	home := t.TempDir()
	g := jsonClient(t, "gemini", home)
	const settings = `{
  "tools": { "enableHooks": true },
  "mcpServers": {
    "ctx": { "command": "npx", "args": ["-y", "@upstash/context7-mcp"], "trust": true },
    "web": { "httpUrl": "https://example.com/mcp" }
  }
}
`
	write(t, g.Path(), settings)
	if es := entriesByName(t, g); es["web"].Kind != "http" || es["ctx"].Kind != "stdio" {
		t.Fatalf("gemini: %+v", es)
	}
	rec, err := g.Attach("ctx", Target{Wrapper: wrapper})
	if err != nil {
		t.Fatal(err)
	}
	if after := read(t, g.Path()); !strings.Contains(after, `"enableHooks": true`) || !strings.Contains(after, `"trust": true`) {
		t.Fatalf("other settings changed:\n%s", after)
	}
	if err := g.Restore(rec); err != nil || read(t, g.Path()) != settings {
		t.Fatalf("restore: %v\n%s", err, read(t, g.Path()))
	}

	w := jsonClient(t, "windsurf", home)
	write(t, w.Path(), `{"mcpServers": {"web": {"serverUrl": "https://example.com/mcp"}}}`)
	if es := entriesByName(t, w); es["web"].Kind != "http" {
		t.Fatalf("windsurf: %+v", es)
	}
}

// Claude Desktop's file holds its preferences; a server added to it gets
// an mcpServers object, and removing it leaves the preferences alone.
func TestAddToFileWithoutServers(t *testing.T) {
	home := t.TempDir()
	c := jsonClient(t, "claude-desktop", home)
	if want := filepath.Join(appDir(home), "Claude", "claude_desktop_config.json"); c.Path() != want {
		t.Fatalf("path %s, want %s", c.Path(), want)
	}
	write(t, c.Path(), "{\n  \"preferences\": {\n    \"sidebarMode\": \"chat\"\n  }\n}\n")
	if es, err := c.List(); es == nil || len(es) != 0 || err != nil {
		t.Fatalf("installed without servers: %v %v", es, err)
	}
	server := Entry{Kind: "stdio", Command: "npx", Args: []string{"-y", "@upstash/context7-mcp"}}
	rec, err := c.Add("ctx", Target{Wrapper: wrapper, Server: server})
	if err != nil {
		t.Fatal(err)
	}
	if inner, ok := Unwrap(entriesByName(t, c)["ctx"]); !ok || inner.Command != "npx" {
		t.Fatalf("added:\n%s", read(t, c.Path()))
	}
	if err := c.Restore(rec); err != nil {
		t.Fatal(err)
	}
	if got := read(t, c.Path()); !strings.Contains(got, `"sidebarMode": "chat"`) || strings.Contains(got, "ctx") {
		t.Fatalf("after removing:\n%s", got)
	}
}
